package pulse

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"
)

// Entries expire before Pulse's 24-hour idempotency record can disappear.
const outboxMaxAge = 23 * time.Hour

type queuedBatch struct {
	Created  time.Time         `json:"created"`
	Parts    []json.RawMessage `json:"parts"`
	Rejected []json.RawMessage `json:"rejected"`
}

func (c Client) deliverQueued(ctx context.Context, parts []Batch, log *slog.Logger) error {
	if c.SpoolMaxBytes <= 0 {
		return errors.New("spool_max_bytes must be positive when spool_dir is configured")
	}
	if err := os.MkdirAll(c.SpoolDir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(c.SpoolDir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("spool directory must be private (0700) and not a symlink")
	}
	lock, err := os.OpenFile(filepath.Join(c.SpoolDir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("spool directory is in use by another sender")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	pending, err := filepath.Glob(filepath.Join(c.SpoolDir, ".pending-*"))
	if err != nil {
		return err
	}
	for _, path := range pending {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	binding := fmt.Sprintf("%x", sha256.Sum256([]byte(c.URL+"\x00"+c.Token)))
	bindingPath := filepath.Join(c.SpoolDir, ".binding")
	existing, err := os.ReadFile(bindingPath)
	if errors.Is(err, os.ErrNotExist) {
		err = atomicWrite(bindingPath, []byte(binding))
	} else if err == nil && string(existing) != binding {
		return errors.New("spool endpoint or source token changed; use a separate spool directory")
	}
	if err != nil {
		return err
	}
	paths, err := filepath.Glob(filepath.Join(c.SpoolDir, "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	var notices []error
	var used int64
	entries := map[string]queuedBatch{}
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("spool entry must be a private regular file")
		}
		if info.Size() > c.SpoolMaxBytes {
			return errors.New("spool entry exceeds configured byte budget")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var entry queuedBatch
		if err := json.Unmarshal(data, &entry); err != nil {
			return errors.New("invalid spool entry; manual recovery required")
		}
		if entry.Created.IsZero() || entry.Created.After(time.Now()) {
			return errors.New("invalid spool creation timestamp")
		}
		if time.Since(entry.Created) >= outboxMaxAge {
			if err := os.Remove(path); err != nil {
				return err
			}
			notices = append(notices, errors.New("expired spool entry discarded after 23 hours; data may be missing"))
			continue
		}
		used += info.Size()
		if used > c.SpoolMaxBytes {
			return errors.New("existing spool exceeds configured byte budget")
		}
		entries[path] = entry
	}
	// Attempt existing entries first so recovered senders can reclaim capacity.
	deliveryFailed := false
	for _, path := range paths {
		entry, ok := entries[path]
		if !ok {
			continue
		}
		if len(entry.Rejected) > 0 && len(entry.Parts) == 0 {
			notices = append(notices, errors.New("spool contains rejected data; inspect configuration and remove the rejected entry"))
			continue
		}
		if deliveryFailed {
			continue
		}
		size, _ := os.Stat(path)
		sendErr := c.drainEntry(ctx, path, &entry, log)
		if sendErr != nil {
			notices = append(notices, sendErr)
			if len(entry.Rejected) > 0 && len(entry.Parts) == 0 {
				entries[path] = entry
			} else {
				deliveryFailed = true
			}
		} else {
			delete(entries, path)
		}
		// Rewrites shrink entries after partial success.
		if after, err := os.Stat(path); err == nil {
			used += after.Size() - size.Size()
		} else if errors.Is(err, os.ErrNotExist) {
			used -= size.Size()
		} else {
			return err
		}
	}
	entry := queuedBatch{Created: time.Now().UTC()}
	for _, part := range parts {
		body, err := json.Marshal(part)
		if err != nil {
			return err
		}
		entry.Parts = append(entry.Parts, body)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if used+int64(len(data)) > c.SpoolMaxBytes {
		return errors.Join(append(notices, errors.New("spool byte budget exhausted; new collection was not queued"))...)
	}
	path := filepath.Join(c.SpoolDir, fmt.Sprintf("%020d-%s.json", entry.Created.UnixNano(), idempotencyKey(data)))
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	if !deliveryFailed {
		if err := c.drainEntry(ctx, path, &entry, log); err != nil {
			notices = append(notices, err)
		}
	}
	return errors.Join(notices...)
}

func (c Client) drainEntry(ctx context.Context, path string, entry *queuedBatch, log *slog.Logger) error {
	for len(entry.Parts) > 0 {
		// Include the retry loop in the entry's replay deadline.
		sendCtx, cancel := context.WithDeadline(ctx, entry.Created.Add(outboxMaxAge))
		err := c.postBody(sendCtx, entry.Parts[0], log)
		cancel()
		if err != nil {
			var status httpStatusError
			if errors.As(err, &status) && status.code >= 400 && status.code < 500 && status.code != 401 && status.code != 403 && !isRetryable(err) {
				entry.Rejected = append(entry.Rejected, entry.Parts[0])
				entry.Parts = entry.Parts[1:]
				data, _ := json.Marshal(entry)
				if writeErr := atomicWrite(path, data); writeErr != nil {
					return errors.Join(err, writeErr)
				}
				continue
			}
			return fmt.Errorf("delivery failed; batch retained in spool: %w", err)
		}
		entry.Parts = entry.Parts[1:]
		if len(entry.Parts) > 0 {
			data, _ := json.Marshal(entry)
			if err := atomicWrite(path, data); err != nil {
				return err
			}
		}
	}
	if len(entry.Rejected) > 0 {
		data, _ := json.Marshal(entry)
		if err := atomicWrite(path, data); err != nil {
			return err
		}
		return errors.New("permanently rejected spool parts retained for inspection")
	}
	if err := os.Remove(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	return syncDirectory(dir)
}
func syncDirectory(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
