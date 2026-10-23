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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Parts expire before Pulse's 24-hour idempotency record can disappear.
const SpoolMaxAge = 23 * time.Hour

const (
	partSuffix         = ".part"
	quarantineDir      = "quarantine"
	maxQuarantineFiles = 10
	maxPartBytes       = maxBytesPerIngestPayload + 1024
)

// Spool is a bounded FIFO of encoded ingest parts. With Dir set, parts survive
// restarts; without it they live in memory. One process owns a directory.
type Spool struct {
	Dir      string
	MaxBytes int64
	// Binding identifies endpoint and credential; a directory never mixes them.
	Binding string
	Stats   *Stats
	Logger  *slog.Logger
	// Now supplies the clock for delivery and maintenance; nil uses time.Now.
	Now func() time.Time

	mu   sync.Mutex
	lock *os.File
	// splits counts rejected parts split within the current hour.
	splits      int
	splitWindow time.Time
	parts       []spoolPart
	bytes       int64
	seq         uint64
}

type spoolPart struct {
	name    string
	created time.Time
	size    int64
	body    []byte // memory spool only
}

// SpoolState summarizes the queue for self-monitoring.
type SpoolState struct {
	Parts  int
	Bytes  int64
	Oldest time.Time
}

func SpoolBinding(url, token string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(url+"\x00"+token)))
}

func (s *Spool) log() *slog.Logger {
	if s.Logger == nil {
		return slog.Default()
	}
	return s.Logger
}

func (s *Spool) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Open validates and locks the directory and indexes queued parts.
func (s *Spool) Open(now time.Time) error {
	if s.MaxBytes <= 0 {
		return errors.New("spool_max_bytes must be positive")
	}
	if s.Dir == "" {
		return nil
	}
	if err := os.MkdirAll(s.Dir, 0700); err != nil {
		return err
	}
	// systemd's DynamicUser= state directories are symlinks into /var/lib/private.
	resolved, err := filepath.EvalSymlinks(s.Dir)
	if err != nil {
		return err
	}
	s.Dir = resolved
	if err := requirePrivateDir(s.Dir); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(s.Dir, ".lock"), os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return errors.New("spool directory is in use by another ingestor process")
	}
	s.lock = lock
	if err := s.load(now); err != nil {
		s.Close()
		return err
	}
	return nil
}

func (s *Spool) load(now time.Time) error {
	legacy, err := filepath.Glob(filepath.Join(s.Dir, "*.json"))
	if err != nil {
		return err
	}
	if len(legacy) > 0 {
		return fmt.Errorf("spool directory %s contains legacy outbox JSON files; deliver or delete them before starting", s.Dir)
	}
	pending, err := filepath.Glob(filepath.Join(s.Dir, ".pending-*"))
	if err != nil {
		return err
	}
	for _, path := range pending {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	bindingPath := filepath.Join(s.Dir, ".binding")
	existing, err := os.ReadFile(bindingPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := atomicWrite(bindingPath, []byte(s.Binding)); err != nil {
			return err
		}
	case err != nil:
		return err
	case string(existing) != s.Binding:
		return errors.New("spool endpoint or source token changed; use a separate spool directory or drain and remove the old one")
	}
	paths, err := filepath.Glob(filepath.Join(s.Dir, "*"+partSuffix))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return fmt.Errorf("spool entry %s must be a private regular file", filepath.Base(path))
		}
		created, seq, ok := parsePartName(filepath.Base(path))
		if !ok || info.Size() > maxPartBytes {
			return fmt.Errorf("spool entry %s is invalid; remove it manually", filepath.Base(path))
		}
		s.seq = max(s.seq, seq)
		s.parts = append(s.parts, spoolPart{name: filepath.Base(path), created: created, size: info.Size()})
		s.bytes += info.Size()
	}
	s.expire(now)
	s.evictFor(0, now)
	return nil
}

func (s *Spool) Close() {
	if s.lock != nil {
		_ = syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN)
		_ = s.lock.Close()
		s.lock = nil
	}
}

// Enqueue stores parts in order. When the budget is exhausted the oldest
// queued parts are dropped first: fresh telemetry is worth more than old.
func (s *Spool) Enqueue(parts []Batch, now time.Time) error {
	bodies := make([][]byte, 0, len(parts))
	var total int64
	for _, part := range parts {
		body, err := json.Marshal(part)
		if err != nil {
			return fmt.Errorf("encode pulse batch: %w", err)
		}
		bodies = append(bodies, body)
		total += int64(len(body))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(now)
	if total > s.MaxBytes {
		s.notice(Notice{Kind: "dropped", Reason: "budget", Parts: len(bodies), Bytes: total, OccurredAt: now})
		return fmt.Errorf("collection of %d bytes exceeds spool_max_bytes; not queued", total)
	}
	s.evictFor(total, now)
	for _, body := range bodies {
		s.seq++
		part := spoolPart{name: partName(now, s.seq), created: now, size: int64(len(body))}
		if s.Dir == "" {
			part.body = body
		} else if err := atomicWrite(filepath.Join(s.Dir, part.name), body); err != nil {
			return err
		}
		s.parts = append(s.parts, part)
		s.bytes += part.size
	}
	return nil
}

// State reports queue size and the oldest queued part.
func (s *Spool) State() SpoolState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := SpoolState{Parts: len(s.parts), Bytes: s.bytes}
	if len(s.parts) > 0 {
		state.Oldest = s.parts[0].created
	}
	return state
}

// Drain sends queued parts oldest first until the queue is empty or a part
// cannot be delivered now. Rejected items are isolated and quarantined; an
// exhausted split budget pauses FIFO until the next window.
// The returned result is the blocking attempt, or Delivered when empty.
func (s *Spool) Drain(ctx context.Context, client Client) Result {
	for {
		if ctx.Err() != nil {
			return Result{Outcome: Retry, Err: ctx.Err()}
		}
		part, body, err := s.peek(s.now())
		if err != nil {
			return Result{Outcome: Retry, Err: err}
		}
		if part == nil {
			return Result{Outcome: Delivered}
		}
		sendCtx, cancel := context.WithTimeout(ctx, part.created.Add(SpoolMaxAge).Sub(s.now()))
		result := client.Send(sendCtx, body)
		cancel()
		switch result.Outcome {
		case Delivered:
			if err := s.remove(*part); err != nil {
				return Result{Outcome: Retry, Err: err}
			}
		case Rejected:
			if result := s.reject(*part, body, result); result.Outcome != Delivered {
				return result
			}
		default:
			return result
		}
	}
}

func (s *Spool) peek(now time.Time) (*spoolPart, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(now)
	if len(s.parts) == 0 {
		return nil, nil, nil
	}
	part := s.parts[0]
	if s.Dir == "" {
		return &part, part.body, nil
	}
	body, err := os.ReadFile(filepath.Join(s.Dir, part.name))
	if err != nil {
		return nil, nil, fmt.Errorf("read spool entry: %w", err)
	}
	return &part, body, nil
}

// remove deletes a part; a part evicted meanwhile is already gone.
func (s *Spool) remove(part spoolPart) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, queued := range s.parts {
		if queued.name != part.name {
			continue
		}
		if s.Dir != "" {
			if err := os.Remove(filepath.Join(s.Dir, part.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		s.parts = append(s.parts[:i], s.parts[i+1:]...)
		s.bytes -= queued.size
		return nil
	}
	return nil
}

// reject isolates invalid input. Pulse refuses a whole part for one invalid
// item and writes nothing, so a rejected part is split in halves until the
// invalid items stand alone; only those go to quarantine.
func (s *Spool) reject(part spoolPart, body []byte, result Result) Result {
	if halves, ok := halve(body); ok {
		if wait := s.allowSplit(s.now()); wait > 0 {
			return Result{Outcome: Retry, RetryAfter: wait, Message: "rejection split budget exhausted", Status: result.Status}
		}
		if err := s.replace(part, halves); err != nil {
			return Result{Outcome: Retry, Err: fmt.Errorf("split rejected part: %w", err)}
		}
		return Result{Outcome: Delivered}
	}
	s.quarantine(part, body, result)
	return Result{Outcome: Delivered}
}

// maxSplitsPerHour bounds the extra requests when every item is invalid, for
// example after a large clock jump; isolating one bad item needs about 2·log2(n).
const maxSplitsPerHour = 256

// allowSplit returns the wait until the budget resets, or zero if allowed.
func (s *Spool) allowSplit(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.splitWindow.IsZero() || now.Sub(s.splitWindow) >= time.Hour {
		s.splitWindow, s.splits = now, 0
	}
	if s.splits >= maxSplitsPerHour {
		return s.splitWindow.Add(time.Hour).Sub(now)
	}
	s.splits++
	return 0
}

// replace swaps a queued part for its halves at the same queue position.
func (s *Spool) replace(part spoolPart, halves [][]byte) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := -1
	for i, queued := range s.parts {
		if queued.name == part.name {
			index = i
			break
		}
	}
	if index < 0 {
		return nil
	}
	replacement := make([]spoolPart, 0, len(halves))
	defer func() {
		if err == nil || s.Dir == "" {
			return
		}
		// A failed split must not leave halves that Open would replay alongside
		// the original. Include the current write: rename may succeed before sync fails.
		for _, next := range replacement {
			if removeErr := os.Remove(filepath.Join(s.Dir, next.name)); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("clean up split part: %w", removeErr))
			}
		}
	}()
	for _, body := range halves {
		s.seq++
		next := spoolPart{name: partName(part.created, s.seq), created: part.created, size: int64(len(body))}
		replacement = append(replacement, next)
		if s.Dir == "" {
			replacement[len(replacement)-1].body = body
		} else if err := atomicWrite(filepath.Join(s.Dir, next.name), body); err != nil {
			return err
		}
	}
	if s.Dir != "" {
		if err := os.Remove(filepath.Join(s.Dir, part.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	rest := append(replacement, s.parts[index+1:]...)
	s.parts = append(s.parts[:index], rest...)
	for _, next := range replacement {
		s.bytes += next.size
	}
	s.bytes -= part.size
	return nil
}

func (s *Spool) quarantine(part spoolPart, body []byte, result Result) {
	counts := countItems(body)
	item, metric := describeItem(body)
	if strings.Contains(strings.ToLower(result.Message), "type and unit") {
		s.Stats.blockMetric(metric, s.now())
	}
	s.notice(Notice{
		Kind: "rejected", Reason: result.Code, Status: result.Status, Message: result.Message, Item: item,
		Parts: 1, Bytes: part.size, Metrics: counts.Metrics, Events: counts.Events, States: counts.States,
		OccurredAt: s.now().UTC(),
	})
	s.log().Error("pulse rejected telemetry; moved to quarantine", "status", result.Status, "code", result.Code, "message", result.Message,
		"item", item, "metrics", counts.Metrics, "events", counts.Events, "states", counts.States)
	if s.Dir != "" {
		if err := s.writeQuarantine(part, body, result); err != nil {
			s.log().Error("could not keep rejected part in quarantine", "err", err)
		}
	}
	if err := s.remove(part); err != nil {
		s.log().Error("could not remove rejected part from spool", "err", err)
	}
}

func (s *Spool) writeQuarantine(part spoolPart, body []byte, result Result) error {
	dir := filepath.Join(s.Dir, quarantineDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(map[string]any{
		"rejectedAt": s.now().UTC(),
		"status":     result.Status,
		"code":       result.Code,
		"message":    result.Message,
		"batch":      json.RawMessage(body),
	})
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(dir, strings.TrimSuffix(part.name, partSuffix)+".json"), data); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for len(files) > maxQuarantineFiles {
		if err := os.Remove(files[0]); err != nil {
			return err
		}
		files = files[1:]
	}
	return nil
}

// expire drops parts older than SpoolMaxAge. Caller holds s.mu.
func (s *Spool) expire(now time.Time) {
	var dropped int
	var bytes int64
	for len(s.parts) > 0 && now.Sub(s.parts[0].created) >= SpoolMaxAge {
		dropped++
		bytes += s.parts[0].size
		s.drop(0)
	}
	if dropped > 0 {
		s.notice(Notice{Kind: "dropped", Reason: "expired", Parts: dropped, Bytes: bytes, OccurredAt: now})
		s.log().Error("spooled telemetry expired before delivery; data is missing", "parts", dropped, "bytes", bytes)
	}
}

// evictFor drops the oldest parts until extra bytes fit. Caller holds s.mu.
func (s *Spool) evictFor(extra int64, now time.Time) {
	var dropped int
	var bytes int64
	for len(s.parts) > 0 && s.bytes+extra > s.MaxBytes {
		dropped++
		bytes += s.parts[0].size
		s.drop(0)
	}
	if dropped > 0 {
		s.notice(Notice{Kind: "dropped", Reason: "budget", Parts: dropped, Bytes: bytes, OccurredAt: now})
		s.log().Error("spool budget exhausted; dropped oldest telemetry", "parts", dropped, "bytes", bytes)
	}
}

func (s *Spool) drop(i int) {
	part := s.parts[i]
	if s.Dir != "" {
		if err := os.Remove(filepath.Join(s.Dir, part.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log().Error("could not remove spool entry", "err", err)
		}
	}
	s.parts = append(s.parts[:i], s.parts[i+1:]...)
	s.bytes -= part.size
}

func (s *Spool) notice(n Notice) {
	s.Stats.addNotice(n)
}

type itemCounts struct{ Metrics, Events, States int }

type rawBatch struct {
	Metrics []json.RawMessage `json:"metrics"`
	Events  []json.RawMessage `json:"events"`
	States  []json.RawMessage `json:"states"`
}

// halve splits a part with more than one item into two exact halves.
func halve(body []byte) ([][]byte, bool) {
	var batch rawBatch
	if json.Unmarshal(body, &batch) != nil {
		return nil, false
	}
	total := len(batch.Metrics) + len(batch.Events) + len(batch.States)
	if total < 2 {
		return nil, false
	}
	first, second := rawBatch{Metrics: []json.RawMessage{}, Events: []json.RawMessage{}, States: []json.RawMessage{}}, rawBatch{Metrics: []json.RawMessage{}, Events: []json.RawMessage{}, States: []json.RawMessage{}}
	n := 0
	place := func(item json.RawMessage, pick func(*rawBatch) *[]json.RawMessage) {
		target := &first
		if n >= total/2 {
			target = &second
		}
		*pick(target) = append(*pick(target), item)
		n++
	}
	for _, item := range batch.Metrics {
		place(item, func(b *rawBatch) *[]json.RawMessage { return &b.Metrics })
	}
	for _, item := range batch.Events {
		place(item, func(b *rawBatch) *[]json.RawMessage { return &b.Events })
	}
	for _, item := range batch.States {
		place(item, func(b *rawBatch) *[]json.RawMessage { return &b.States })
	}
	a, errA := json.Marshal(first)
	b, errB := json.Marshal(second)
	if errA != nil || errB != nil {
		return nil, false
	}
	return [][]byte{a, b}, true
}

// describeItem names a single rejected item, for example "metric system.load.1m",
// and returns the metric name when the item is a metric.
func describeItem(body []byte) (string, string) {
	var batch rawBatch
	if json.Unmarshal(body, &batch) != nil || len(batch.Metrics)+len(batch.Events)+len(batch.States) != 1 {
		return "", ""
	}
	var item struct {
		Name string `json:"name"`
		Kind string `json:"kind"`
		Key  string `json:"key"`
		Unit string `json:"unit"`
		Type string `json:"type"`
	}
	switch {
	case len(batch.Metrics) == 1:
		_ = json.Unmarshal(batch.Metrics[0], &item)
		return cleanText("metric "+item.Name+" ("+item.Type+", unit "+item.Unit+")", 200), item.Name
	case len(batch.Events) == 1:
		_ = json.Unmarshal(batch.Events[0], &item)
		return cleanText("event "+item.Kind, 200), ""
	default:
		_ = json.Unmarshal(batch.States[0], &item)
		return cleanText("state "+item.Key, 200), ""
	}
}

func countItems(body []byte) itemCounts {
	var batch rawBatch
	_ = json.Unmarshal(body, &batch)
	return itemCounts{Metrics: len(batch.Metrics), Events: len(batch.Events), States: len(batch.States)}
}

func partName(created time.Time, seq uint64) string {
	return fmt.Sprintf("%020d-%010d%s", created.UnixNano(), seq, partSuffix)
}

func parsePartName(name string) (time.Time, uint64, bool) {
	stem, ok := strings.CutSuffix(name, partSuffix)
	if !ok {
		return time.Time{}, 0, false
	}
	nanos, seqText, ok := strings.Cut(stem, "-")
	if !ok {
		return time.Time{}, 0, false
	}
	n, err := strconv.ParseInt(nanos, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, 0, false
	}
	seq, err := strconv.ParseUint(seqText, 10, 64)
	if err != nil {
		return time.Time{}, 0, false
	}
	return time.Unix(0, n).UTC(), seq, true
}

func requirePrivateDir(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("spool directory must be private (0700)")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Geteuid() {
		return errors.New("spool directory must be owned by the ingestor's user")
	}
	return nil
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
