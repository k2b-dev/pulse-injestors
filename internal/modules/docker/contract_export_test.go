package docker

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

// Export all Docker phases through the same local Unix HTTP fixture used by the
// cadence regression, without requiring a daemon or contacting any registry.
func TestPulseContractExport(t *testing.T) {
	scope := monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "fixture", Label: "Fixture"}, Timestamp: time.Unix(1700001000, 0).UTC()}
	fake := newFakeDocker(t, 1, nil)
	base := fake.collector()
	var batch pulse.Batch
	for _, collector := range []monitoring.Collector{base, StatsCollector{Collector: base}, InventoryCollector{Collector: base}} {
		part, err := collector.Collect(context.Background(), scope)
		if err != nil {
			t.Fatal(err)
		}
		monitoring.Merge(&batch, part)
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
		if err := os.WriteFile(filepath.Join(directory, "docker.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
