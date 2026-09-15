package validation

import (
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestBatchRejectsDuplicateMetricSeries(t *testing.T) {
	ts := time.Now().UTC()
	err := Batch(pulse.Batch{Metrics: []pulse.Metric{
		{Name: "system.memory.used", Type: "gauge", Value: 1, Unit: "bytes", Timestamp: ts, Resource: &pulse.ResourceRef{Type: "host", ID: "n", Label: "node"}},
		{Name: "system.memory.used", Type: "gauge", Value: 2, Unit: "bytes", Timestamp: ts, Resource: &pulse.ResourceRef{Type: "host", ID: "n", Label: "node"}},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBatchAllowsDockerCPUOverHundred(t *testing.T) {
	for _, metric := range []pulse.Metric{
		{Name: "docker.container.cpu.usage", Type: "gauge", Value: 250, Unit: "percent", Timestamp: time.Now().UTC(), Resource: &pulse.ResourceRef{Type: "docker-container", ID: "server-01:name:api", Label: "api"}},
		{Name: "docker.compose.service.cpu.usage", Type: "gauge", Value: 350, Unit: "percent", Timestamp: time.Now().UTC(), Resource: &pulse.ResourceRef{Type: "docker-compose-service", ID: "server-01:app:api", Label: "app / api"}},
	} {
		if err := Batch(pulse.Batch{Metrics: []pulse.Metric{metric}}); err != nil {
			t.Errorf("%s: %v", metric.Name, err)
		}
	}
}

func TestBatchRejectsStructuredStateValues(t *testing.T) {
	err := Batch(pulse.Batch{States: []pulse.State{
		{Key: "docker.image.repo_tags", Value: []string{"image:latest"}, Timestamp: time.Now().UTC(), Resource: &pulse.ResourceRef{Type: "docker-image", ID: "server-01:abc123", Label: "image:latest"}},
	}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCurrentPulseContract(t *testing.T) {
	ts := time.Now().UTC()
	valid := pulse.Metric{Name: "custom_metric", Type: "gauge", Timestamp: ts}
	if err := Batch(pulse.Batch{Metrics: []pulse.Metric{valid}}); err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"histogram", "summary"} {
		metric := valid
		metric.Type = typ
		if Batch(pulse.Batch{Metrics: []pulse.Metric{metric}}) == nil {
			t.Fatalf("accepted %s", typ)
		}
	}
	conflicting := valid
	conflicting.Type = "counter"
	conflicting.Timestamp = ts.Add(time.Minute)
	if Batch(pulse.Batch{Metrics: []pulse.Metric{valid, conflicting}}) == nil {
		t.Fatal("definition conflict accepted")
	}
	invalid := valid
	invalid.Resource = &pulse.ResourceRef{Type: "Invalid", ID: "x"}
	if Batch(pulse.Batch{Metrics: []pulse.Metric{invalid}}) == nil {
		t.Fatal("invalid slug accepted")
	}
}

func TestDimensionDelimitersDoNotCreateDuplicateSeries(t *testing.T) {
	a := pulse.Metric{Name: "test.x", Type: "gauge", Timestamp: time.Now().UTC(), Dimensions: map[string]string{"a": "b,c=d"}}
	b := a
	b.Dimensions = map[string]string{"a": "b", "c": "d"}
	if err := Batch(pulse.Batch{Metrics: []pulse.Metric{a, b}}); err != nil {
		t.Fatal(err)
	}
}
