package monitoring

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

// StateCache suppresses unchanged states. Pulse keeps the current value of a
// state without expiry, so repeating an equal value adds nothing but quota.
// A state is sent when it changes or when Refresh has passed since it was last
// sent. With Path set, the cache survives restarts.
type StateCache struct {
	Path    string
	Refresh time.Duration
	// Forget removes identities that were not observed for this long.
	Forget time.Duration

	entries map[string]stateEntry
}

type stateEntry struct {
	Hash string `json:"h"`
	Sent int64  `json:"s"`
	Seen int64  `json:"o"`
}

const (
	DefaultStateRefresh = 6 * time.Hour
	DefaultStateForget  = 7 * 24 * time.Hour
)

// Load reads the cache. A missing or unreadable cache only causes a resend.
func (c *StateCache) Load() error {
	c.entries = map[string]stateEntry{}
	if c.Path == "" {
		return nil
	}
	data, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, &c.entries); err != nil {
		c.entries = map[string]stateEntry{}
		return errors.New("state cache is unreadable; all states will be sent again")
	}
	return nil
}

// Reset forgets all states so the next collection resends them after queue loss.
func (c *StateCache) Reset() {
	c.entries = nil
}

// Filter returns the states to send now. Commit records them as sent; call it
// only after the batch was queued durably.
func (c *StateCache) Filter(states []pulse.State, now time.Time) ([]pulse.State, func()) {
	if c.entries == nil {
		c.entries = map[string]stateEntry{}
	}
	refresh := c.Refresh
	if refresh <= 0 {
		refresh = DefaultStateRefresh
	}
	type pending struct{ id, hash string }
	send := make([]pulse.State, 0, len(states))
	var sent, seen []pending
	last := make(map[string]int, len(states))
	for i, state := range states {
		last[stateIdentity(state)] = i
	}
	for i, state := range states {
		id := stateIdentity(state)
		if last[id] != i {
			continue
		}
		hash := valueHash(state.Value)
		entry, known := c.entries[id]
		seen = append(seen, pending{id, hash})
		if known && entry.Hash == hash && now.Sub(time.Unix(entry.Sent, 0)) < refresh {
			continue
		}
		send = append(send, state)
		sent = append(sent, pending{id, hash})
	}
	return send, func() {
		for _, s := range seen {
			entry := c.entries[s.id]
			entry.Seen = now.Unix()
			c.entries[s.id] = entry
		}
		for _, s := range sent {
			entry := c.entries[s.id]
			entry.Hash, entry.Sent = s.hash, now.Unix()
			c.entries[s.id] = entry
		}
	}
}

// Save forgets stale identities and writes the cache atomically.
func (c *StateCache) Save(now time.Time) error {
	forget := c.Forget
	if forget <= 0 {
		forget = DefaultStateForget
	}
	for id, entry := range c.entries {
		if now.Sub(time.Unix(entry.Seen, 0)) > forget {
			delete(c.entries, id)
		}
	}
	if c.Path == "" {
		return nil
	}
	data, err := json.Marshal(c.entries)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.Path), ".pending-states-")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.Path)
}

func stateIdentity(state pulse.State) string {
	keys := make([]string, 0, len(state.Dimensions))
	for key := range state.Dimensions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := []string{state.Key, ""}
	if state.Resource != nil {
		parts[1] = state.Resource.Key()
	}
	for _, key := range keys {
		parts = append(parts, key, state.Dimensions[key])
	}
	data, _ := json.Marshal(parts)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

func valueHash(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}
