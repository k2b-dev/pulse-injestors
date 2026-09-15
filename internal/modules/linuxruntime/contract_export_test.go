package linuxruntime

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
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "pressure"))
	mustMkdir(t, filepath.Join(root, "net"))
	mustMkdir(t, filepath.Join(root, "42"))
	mustWrite(t, filepath.Join(root, "pressure", "cpu"), "some avg10=1.00 avg60=2.00 avg300=3.00 total=400\n")
	mustWrite(t, filepath.Join(root, "42", "stat"), "42 (fixture) R 1 2 3")
	mustWrite(t, filepath.Join(root, "net", "tcp"), "sl local_address rem_address st\n0: 0:0 0:0 0A\n")
	mustWrite(t, filepath.Join(root, "net", "udp"), "sl local_address rem_address st\n0: 0:0 0:0 07\n")

	batch, err := Collector{ProcRoot: root}.Collect(context.Background(), monitoring.Scope{
		Resource:  pulse.ResourceRef{Type: "host", ID: "host"},
		Timestamp: time.Unix(1700001000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) == 0 || len(batch.States) == 0 {
		t.Fatal("fixture must cover metrics and states")
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
		if err := os.WriteFile(filepath.Join(directory, "linuxruntime.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
