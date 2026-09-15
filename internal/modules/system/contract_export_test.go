package system

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

// TestPulseContractExport exercises mocked collection even without the external contract harness.
func TestPulseContractExport(t *testing.T) {
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
	batch, err := Collector{ProcRoot: proc, CPUSampleTime: 10 * time.Millisecond}.Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) < 10 {
		t.Fatal("fixture must cover load, memory, swap, uptime, and CPU metrics")
	}
	if batch.Metrics == nil {
		batch.Metrics = []pulse.Metric{}
	}
	if batch.Events == nil {
		batch.Events = []pulse.Event{}
	}
	if batch.States == nil {
		batch.States = []pulse.State{}
	}
	if directory := os.Getenv("PULSE_CONTRACT_OUTPUT"); directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "system.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
