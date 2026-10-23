package system

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
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
	batch, err := Collector{ProcRoot: proc, CPUSampleTime: 10 * time.Millisecond}.Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) == 0 {
		t.Fatal("expected metrics")
	}
}

func TestCPUModesExcludeGuestAndCountCores(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "stat"), []byte("cpu 100 200 300 400 500 600 700 800 90 10\ncpu0 1 2 3\ncpu1 1 2 3\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sample, err := readCPUSample(root)
	if err != nil {
		t.Fatal(err)
	}
	if sample.total != 3600 || sample.idle != 900 || sample.cores != 2 {
		t.Fatalf("sample=%+v", sample)
	}
	batch, err := (Collector{ProcRoot: root, CPUSampleTime: time.Nanosecond}).Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]float64{}
	for _, m := range batch.Metrics {
		if m.Name == "system.cpu.time" {
			if m.Type != "counter" || m.Unit != "seconds" {
				t.Fatalf("metric=%+v", m)
			}
			modes[m.Dimensions["mode"]] = m.Value
		}
	}
	if len(modes) != 8 || modes["user"] != 1 || modes["steal"] != 8 {
		t.Fatalf("modes=%v", modes)
	}
}

func TestZeroAvailableMemoryIsValidButMissingIsNot(t *testing.T) {
	root := t.TempDir()
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{"MemTotal: 1000 kB\nMemAvailable: 0 kB\n", true},
		{"MemTotal: 1000 kB\n", false},
		{"MemTotal: 1000 kB\nMemAvailable: 1001 kB\n", false},
	} {
		if err := os.WriteFile(filepath.Join(root, "meminfo"), []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := readMemInfo(root)
		if (err == nil) != tc.valid {
			t.Fatalf("%q: %v", tc.body, err)
		}
	}
}
