package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWatchdogAlive(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	interval, timeout := 5*time.Minute, 30*time.Second
	limit := 2*interval + timeout + time.Minute
	for _, tc := range []struct {
		name string
		last time.Time
		want bool
	}{
		{"no cycle", time.Time{}, false},
		{"just completed", now, true},
		{"long interval", now.Add(-4 * time.Minute), true},
		{"at limit", now.Add(-limit), true},
		{"stalled", now.Add(-limit - time.Nanosecond), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchdogAlive(tc.last, now, interval, timeout); got != tc.want {
				t.Fatalf("alive = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWatchdogIgnoresInvalidEnvironment(t *testing.T) {
	for _, usec := range []string{"", "bad", "0", "-1", "0.001", "9223372036854775807"} {
		watchdogLoop(context.Background(), usec, time.Minute, time.Minute, &atomic.Int64{})
	}
}
