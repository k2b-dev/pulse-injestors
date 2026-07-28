package validation

import (
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestBatchRejectsDuplicateMetricSeries(t *testing.T) {
	ts := time.Now().UTC()
	err := Batch(pulse.Batch{Metrics: []pulse.Metric{
		{Name: "system.memory.used", Type: "gauge", Value: 1, Unit: "bytes", Timestamp: ts, EntityID: "host:n", EntityType: "host", Resource: &pulse.ResourceRef{Type: "host", ID: "n", Label: "node"}},
		{Name: "system.memory.used", Type: "gauge", Value: 2, Unit: "bytes", Timestamp: ts, EntityID: "host:n", EntityType: "host", Resource: &pulse.ResourceRef{Type: "host", ID: "n", Label: "node"}},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBatchAllowsDockerCPUOverHundred(t *testing.T) {
	for _, metric := range []pulse.Metric{
		{Name: "docker.container.cpu.usage", Type: "gauge", Value: 250, Unit: "percent", Timestamp: time.Now().UTC(), EntityID: "docker-container:server-01:name:api", EntityType: "docker-container", Resource: &pulse.ResourceRef{Type: "docker-container", ID: "server-01:name:api", Label: "api"}},
		{Name: "docker.compose.service.cpu.usage", Type: "gauge", Value: 350, Unit: "percent", Timestamp: time.Now().UTC(), EntityID: "docker-compose-service:server-01:app:api", EntityType: "docker-compose-service", Resource: &pulse.ResourceRef{Type: "docker-compose-service", ID: "server-01:app:api", Label: "app / api"}},
	} {
		if err := Batch(pulse.Batch{Metrics: []pulse.Metric{metric}}); err != nil {
			t.Errorf("%s: %v", metric.Name, err)
		}
	}
}

func TestBatchRejectsStructuredStateValues(t *testing.T) {
	err := Batch(pulse.Batch{States: []pulse.State{
		{Key: "docker.image.repo_tags", Value: []string{"image:latest"}, Timestamp: time.Now().UTC(), EntityID: "docker-image:server-01:abc123", EntityType: "docker-image", Resource: &pulse.ResourceRef{Type: "docker-image", ID: "server-01:abc123", Label: "image:latest"}},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}
