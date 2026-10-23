package pulse

import (
	"net/http"
	"sync"
	"time"
)

// Stats records delivery health for self-monitoring. A nil *Stats ignores updates.
type Stats struct {
	mu         sync.Mutex
	delivered  uint64
	failures   uint64
	skew       time.Duration
	skewKnown  bool
	notices    []Notice
	lastNotice map[string]time.Time
	// blocked holds metric names with definition conflicts, until expiry.
	blocked map[string]time.Time
}

// MetricBlockDuration is how long a metric with a definition conflict is not sent.
// A type or unit conflict refuses every later sample, so sending it again
// only costs requests; after the block the metric is tried again.
const MetricBlockDuration = time.Hour

func (s *Stats) blockMetric(name string, now time.Time) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.blocked == nil {
		s.blocked = map[string]time.Time{}
	}
	for key, until := range s.blocked {
		if !now.Before(until) {
			delete(s.blocked, key)
		}
	}
	if name != "" {
		s.blocked[name] = now.Add(MetricBlockDuration)
	}
}

// MetricBlocked reports whether a metric name is currently blocked.
func (s *Stats) MetricBlocked(name string, now time.Time) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.blocked[name]
	if ok && !now.Before(until) {
		delete(s.blocked, name)
		return false
	}
	return ok
}

// Notice is a delivery problem the next collection reports as an event.
type Notice struct {
	Kind    string // "rejected" or "dropped"
	Reason  string // rejection code, "expired" or "budget"
	Status  int
	Message string
	// Item names a single rejected item when one was isolated.
	Item       string
	Parts      int
	Bytes      int64
	Metrics    int
	Events     int
	States     int
	OccurredAt time.Time
}

const maxPendingNotices = 20

// Snapshot is a consistent copy of the counters.
type Snapshot struct {
	Delivered uint64
	Failures  uint64
	Skew      time.Duration
	SkewKnown bool
}

func (s *Stats) addDelivered() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.delivered++
	s.mu.Unlock()
}

func (s *Stats) addFailure() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.failures++
	s.mu.Unlock()
}

// observeServerTime estimates local clock skew from the HTTP Date header,
// using the midpoint of the request as the local reference.
func (s *Stats) observeServerTime(header string, started, finished time.Time) {
	if s == nil || header == "" {
		return
	}
	server, err := http.ParseTime(header)
	if err != nil {
		return
	}
	local := started.Add(finished.Sub(started) / 2)
	s.mu.Lock()
	s.skew = server.Sub(local).Round(time.Second)
	s.skewKnown = true
	s.mu.Unlock()
}

func (s *Stats) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return Snapshot{Delivered: s.delivered, Failures: s.failures, Skew: s.skew, SkewKnown: s.skewKnown}
}

func (s *Stats) addNotice(n Notice) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.notices) >= maxPendingNotices {
		s.notices = s.notices[1:]
	}
	s.notices = append(s.notices, n)
}

// TakeNotices returns and clears pending notices.
func (s *Stats) TakeNotices() []Notice {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.notices
	s.notices = nil
	return out
}

// ShouldLog limits repeated log lines with the same key to one per interval.
func (s *Stats) ShouldLog(key string, now time.Time, interval time.Duration) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lastNotice == nil {
		s.lastNotice = map[string]time.Time{}
	}
	if last, ok := s.lastNotice[key]; ok && now.Sub(last) < interval {
		return false
	}
	s.lastNotice[key] = now
	if len(s.lastNotice) > 1000 {
		for key, last := range s.lastNotice {
			if now.Sub(last) > 24*time.Hour {
				delete(s.lastNotice, key)
			}
		}
	}
	return true
}
