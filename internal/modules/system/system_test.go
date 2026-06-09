package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestCollectorReadsSystemStats(t *testing.T) {
	proc := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(proc, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("loadavg", "0.10 0.20 0.30 1/1 1\n")
	write("uptime", "12.5 10.0\n")
	write("meminfo", "MemTotal: 1000 kB\nMemAvailable: 500 kB\nSwapTotal: 100 kB\nSwapFree: 40 kB\n")
	write("stat", "cpu 100 0 100 100 0 0 0 0 0 0\n")
	go func() {
		time.Sleep(2 * time.Millisecond)
		_ = os.WriteFile(filepath.Join(proc, "stat"), []byte("cpu 200 0 200 200 0 0 0 0 0 0\n"), 0o644)
	}()
	batch, err := Collector{ProcRoot: proc, CPUSampleTime: 10 * time.Millisecond}.Collect(context.Background(), monitoring.Scope{EntityID: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) == 0 {
		t.Fatal("expected metrics")
	}
}
