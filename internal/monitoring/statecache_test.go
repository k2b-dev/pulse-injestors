package monitoring

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestStateCacheSendsOnlyChangesAndRefreshes(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), ".states")
	host := &pulse.ResourceRef{Type: "host", ID: "vm-1"}
	states := func(value any) []pulse.State {
		return []pulse.State{
			{Key: "system.service.active", Value: value, Resource: host, Dimensions: map[string]string{"unit": "a"}},
			{Key: "system.service.active", Value: true, Resource: host, Dimensions: map[string]string{"unit": "b"}},
		}
	}
	cache := &StateCache{Path: path}
	if err := cache.Load(); err != nil {
		t.Fatal(err)
	}
	send, commit := cache.Filter(states(true), now)
	if len(send) != 2 {
		t.Fatalf("first cycle sent %d", len(send))
	}
	commit()
	if err := cache.Save(now); err != nil {
		t.Fatal(err)
	}

	restarted := &StateCache{Path: path}
	if err := restarted.Load(); err != nil {
		t.Fatal(err)
	}
	if send, _ := restarted.Filter(states(true), now.Add(time.Minute)); len(send) != 0 {
		t.Fatalf("unchanged states sent after restart: %d", len(send))
	}
	send, commit = restarted.Filter(states(false), now.Add(2*time.Minute))
	if len(send) != 1 || send[0].Dimensions["unit"] != "a" {
		t.Fatalf("changed state: %#v", send)
	}
	// Not committed: the change must be sent again.
	if send, _ := restarted.Filter(states(false), now.Add(3*time.Minute)); len(send) != 1 {
		t.Fatal("uncommitted change was forgotten")
	}
	if send, _ := restarted.Filter(states(true), now.Add(DefaultStateRefresh+time.Minute)); len(send) != 2 {
		t.Fatalf("refresh sent %d", len(send))
	}
}

func TestStateCacheForgetsUnseenIdentities(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	cache := &StateCache{}
	_, commit := cache.Filter([]pulse.State{{Key: "gone", Value: 1}}, now)
	commit()
	if err := cache.Save(now.Add(DefaultStateForget + time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(cache.entries) != 0 {
		t.Fatal("stale identity kept")
	}
}

func TestStateCacheResetResendsStates(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	cache := &StateCache{}
	states := []pulse.State{{Key: "state", Value: true}}
	_, commit := cache.Filter(states, now)
	commit()
	cache.Reset()
	if send, _ := cache.Filter(states, now.Add(time.Minute)); len(send) != 1 {
		t.Fatal("reset did not resend the state")
	}
}

func TestStateCacheKeepsLastDuplicateBeforeComparing(t *testing.T) {
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	cache := &StateCache{}
	states := []pulse.State{{Key: "duplicate", Value: 1}, {Key: "other", Value: true}, {Key: "duplicate", Value: 2}}
	send, commit := cache.Filter(states, now)
	if len(send) != 2 || send[0].Key != "other" || send[1].Key != "duplicate" || send[1].Value != 2 {
		t.Fatalf("last values/order lost: %+v", send)
	}
	commit()
	if send, _ := cache.Filter(states, now.Add(time.Minute)); len(send) != 0 {
		t.Fatalf("earlier duplicate caused a resend: %+v", send)
	}
}

func TestSoftwareRefreshWeeklyAndDailyObservationPreventsForget(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &StateCache{Path: filepath.Join(t.TempDir(), "states")}
	states := []pulse.State{{Key: "software.package.version", Value: "1"}, {Key: "system.os.version", Value: "1"}}
	for day := 0; day <= 15; day++ {
		ts := now.Add(time.Duration(day) * 24 * time.Hour)
		send, commit := c.Filter(states, ts)
		want := 1
		if day%7 == 0 {
			want = 2
		}
		if len(send) != want {
			t.Fatalf("day %d: %v", day, send)
		}
		commit()
		if err := c.Save(ts); err != nil {
			t.Fatal(err)
		}
		// Verify persistence keeps the last-seen timestamp even when not resent.
		if err := c.Load(); err != nil {
			t.Fatal(err)
		}
		if len(c.entries) != 2 {
			t.Fatal("daily observed key forgotten")
		}
	}
	states[0].Value = "2"
	send, _ := c.Filter(states, now.Add(15*24*time.Hour+time.Minute))
	if len(send) != 1 || send[0].Key != "software.package.version" {
		t.Fatalf("change not sent: %v", send)
	}
}
