package linuxruntime

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestParsePressure(t *testing.T) {
	samples := parsePressure("memory", []byte("some avg10=1.23 avg60=0.50 avg300=0.10 total=12345\nfull avg10=0.00 avg60=0.01 avg300=0.02 total=99\n"))
	if len(samples) != 2 {
		t.Fatalf("samples = %d", len(samples))
	}
	if samples[0].Resource != "memory" || samples[0].Scope != "some" || samples[0].Avg10 != 1.23 || samples[0].Total != 12345 {
		t.Fatalf("first sample = %#v", samples[0])
	}
	if samples[1].Scope != "full" || samples[1].Avg60 != 0.01 {
		t.Fatalf("second sample = %#v", samples[1])
	}
}

func TestParseProcStatStateHandlesProcessNamesWithSpaces(t *testing.T) {
	got := parseProcStatState([]byte("1234 (worker with ) spaces) S 1 2 3 4"))
	if got != 'S' {
		t.Fatalf("state = %q", got)
	}
	if name := processStateName(got); name != "sleeping" {
		t.Fatalf("state name = %q", name)
	}
}

func TestParseSocketsCountsProtocolFamilyAndState(t *testing.T) {
	data := []byte(`  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000   100        0 1 1
   1: 0100007F:1F91 0100007F:E90C 01 00000000:00000000 00:00000000 00000000   100        0 2 1
   2: 0100007F:1F92 0100007F:E90D 01 00000000:00000000 00:00000000 00000000   100        0 3 1
`)
	samples := parseSockets("tcp", "ipv4", data)
	got := map[string]int{}
	for _, sample := range samples {
		got[sample.State] = sample.Count
		if sample.Protocol != "tcp" || sample.Family != "ipv4" {
			t.Fatalf("sample dims = %#v", sample)
		}
	}
	if got["listen"] != 1 || got["established"] != 2 {
		t.Fatalf("counts = %#v", got)
	}
}

func TestCollectorGracefullyReportsMissingProc(t *testing.T) {
	batch, err := Collector{ProcRoot: filepath.Join(t.TempDir(), "missing")}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "host",
		EntityType: "host",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]any{}
	for _, state := range batch.States {
		states[state.Key] = state.Value
	}
	if states["system.pressure.available"] != false {
		t.Fatalf("pressure available = %#v", states["system.pressure.available"])
	}
	if states["system.processes.available"] != false {
		t.Fatalf("processes available = %#v", states["system.processes.available"])
	}
	if states["system.network.sockets.available"] != false {
		t.Fatalf("sockets available = %#v", states["system.network.sockets.available"])
	}
}

func TestCollectorEmitsRuntimeMetricsFromFixtures(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "pressure"))
	mustMkdir(t, filepath.Join(root, "net"))
	mustMkdir(t, filepath.Join(root, "42"))
	mustWrite(t, filepath.Join(root, "pressure", "cpu"), "some avg10=1.00 avg60=2.00 avg300=3.00 total=400\n")
	mustWrite(t, filepath.Join(root, "42", "stat"), "42 (fixture) R 1 2 3")
	mustWrite(t, filepath.Join(root, "net", "tcp"), "sl local_address rem_address st\n0: 0:0 0:0 0A\n")
	mustWrite(t, filepath.Join(root, "net", "udp"), "sl local_address rem_address st\n0: 0:0 0:0 07\n")

	batch, err := Collector{ProcRoot: root}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "host",
		EntityType: "host",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if countMetric(batch.Metrics, "system.pressure.avg10") != 1 {
		t.Fatalf("pressure metrics = %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "system.processes.total") != 1 {
		t.Fatalf("process metrics = %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "system.network.sockets") != 2 {
		t.Fatalf("socket metrics = %#v", batch.Metrics)
	}
}

func countMetric(metrics []pulse.Metric, name string) int {
	count := 0
	for _, metric := range metrics {
		if metric.Name == name {
			count++
		}
	}
	return count
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
