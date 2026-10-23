package kernel

import (
	"context"
	"errors"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBootOOMAndJournalAcrossCollections(t *testing.T) {
	root := t.TempDir()
	now := time.Unix(1700001000, 0).UTC()
	write := func(name, text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("stat", "btime 1700000900\n")
	write("uptime", "100 1\n")
	write("vmstat", "oom_kill 3\n")
	var calls [][]string
	c := &Collector{ProcRoot: root, Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "journalctl" {
			t.Fatal(name)
		}
		calls = append(calls, args)
		if len(calls) == 1 {
			return []byte(strings.Repeat(strings.Repeat("x", 400)+"\n", 8)), nil
		}
		return nil, nil
	}}
	scope := monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "vm"}, Timestamp: now}
	first, err := c.Collect(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(first); err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 2 || first.Events[0].Kind != "system.boot" || !first.Events[0].Timestamp.Equal(time.Unix(1700000900, 0)) {
		t.Fatalf("events=%+v", first.Events)
	}
	event := first.Events[1]
	if event.Attributes["count"] != 8 {
		t.Fatal(event)
	}
	lines := strings.Split(event.Attributes["sample"].(string), "\n")
	if len(lines) != 5 || len(lines[0]) != 300 {
		t.Fatal("unbounded sample")
	}
	if !strings.Contains(strings.Join(calls[0], " "), "--since=-10min") {
		t.Fatal(calls)
	}
	write("vmstat", "oom_kill 5\n")
	scope.Timestamp = now.Add(time.Minute)
	second, err := c.Collect(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].Kind != "system.oom_kill" || second.Events[0].Attributes["count"] != uint64(2) {
		t.Fatalf("events=%+v", second.Events)
	}
	if !strings.Contains(strings.Join(calls[1], " "), "--since=2023-11-14 22:30:00 UTC") {
		t.Fatal(calls[1])
	}
	write("vmstat", "oom_kill 1\n")
	third, err := c.Collect(context.Background(), scope)
	if err != nil || len(third.Events) != 0 {
		t.Fatalf("reset: %+v %v", third, err)
	}
}
func TestCursorAndUnavailableJournal(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	for name, body := range map[string]string{"stat": "btime 1700000000\n", "uptime": "1000 0\n", "vmstat": "oom_kill 0\n"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	c := &Collector{ProcRoot: root, StateDir: state, Run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--cursor-file="+filepath.Join(state, "kernel.cursor")) {
			t.Fatal(args)
		}
		if (calls == 0) != strings.Contains(joined, "--since=-10min") {
			t.Fatal(args)
		}
		calls++
		return nil, exec.ErrNotFound
	}}
	batch, err := c.Collect(context.Background(), monitoring.Scope{})
	if err != nil || len(batch.Events) > 0 {
		t.Fatalf("missing tool: %v %+v", err, batch)
	}
	if err := os.WriteFile(filepath.Join(state, "kernel.cursor"), []byte("cursor"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Collect(context.Background(), monitoring.Scope{}); err != nil {
		t.Fatal(err)
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("denied") }
	if _, err := c.Collect(context.Background(), monitoring.Scope{}); err == nil {
		t.Fatal("unexpected error hidden")
	}
}
