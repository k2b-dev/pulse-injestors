package validation

import (
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

func TestBatchRejectsDuplicateMetricSeries(t *testing.T) {
	ts := time.Now().UTC()
	err := Batch(pulse.Batch{Metrics: []pulse.Metric{
		{Name: "system.memory.used", Type: "gauge", Value: 1, Unit: "bytes", Timestamp: ts, EntityID: "n", EntityType: "host"},
		{Name: "system.memory.used", Type: "gauge", Value: 2, Unit: "bytes", Timestamp: ts, EntityID: "n", EntityType: "host"},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBatchAllowsDockerCPUOverHundred(t *testing.T) {
	err := Batch(pulse.Batch{Metrics: []pulse.Metric{
		{Name: "docker.container.cpu.usage", Type: "gauge", Value: 250, Unit: "percent", Timestamp: time.Now().UTC(), EntityID: "n", EntityType: "docker-host"},
	}})
	if err != nil {
		t.Fatal(err)
	}
}
