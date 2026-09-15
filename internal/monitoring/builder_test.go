package monitoring

import (
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestInjectAddsScopeAndDimensions(t *testing.T) {
	ts := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	batch := Inject(pulse.Batch{
		Metrics: []pulse.Metric{{Name: "x", Type: "gauge", Value: 1, Dimensions: map[string]string{"b": "2"}}},
	}, Scope{
		Resource:   pulse.ResourceRef{Type: "host", ID: "node", Label: "node-01"},
		Timestamp:  ts,
		Dimensions: map[string]string{"a": "1"},
	}, map[string]string{"collector": "test"})
	got := batch.Metrics[0]
	if got.Resource.Key() != "host:node" || got.Resource.Type != "host" || !got.Timestamp.Equal(ts) {
		t.Fatalf("scope not injected: %#v", got)
	}
	if got.Dimensions["a"] != "1" || got.Dimensions["b"] != "2" || got.Dimensions["collector"] != "test" {
		t.Fatalf("dimensions = %#v", got.Dimensions)
	}
	if got.Resource == nil || got.Resource.ID != "node" || got.Resource.Label != "node-01" {
		t.Fatalf("resource = %#v", got.Resource)
	}
}

func TestInjectPreservesExplicitResource(t *testing.T) {
	ts := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	batch := Inject(pulse.Batch{
		Metrics: []pulse.Metric{{
			Name:       "custom.service.latency",
			Type:       "gauge",
			Value:      12,
			Unit:       "milliseconds",
			Resource:   &pulse.ResourceRef{Type: "custom-service", ID: "server-01:redis", Label: "redis"},
			Dimensions: map[string]string{"service": "redis"},
		}},
		Events: []pulse.Event{{
			Kind:       "custom.service.restart",
			Resource:   &pulse.ResourceRef{Type: "custom-service", ID: "server-01:redis", Label: "redis"},
			Dimensions: map[string]string{"service": "redis"},
		}},
		States: []pulse.State{{
			Key:        "custom.service.online",
			Value:      true,
			Resource:   &pulse.ResourceRef{Type: "custom-service", ID: "server-01:redis", Label: "redis"},
			Dimensions: map[string]string{"service": "redis"},
		}},
	}, Scope{
		Resource:   pulse.ResourceRef{Type: "host", ID: "server-01", Label: "server-01"},
		Timestamp:  ts,
		Dimensions: map[string]string{"host": "server-01"},
	}, map[string]string{"collector": "script"})

	if batch.Metrics[0].Resource == nil || batch.Metrics[0].Resource.Label != "redis" {
		t.Fatalf("metric resource = %#v", batch.Metrics[0].Resource)
	}
	if batch.Events[0].Resource == nil || batch.Events[0].Resource.Label != "redis" {
		t.Fatalf("event resource = %#v", batch.Events[0].Resource)
	}
	if batch.States[0].Resource == nil || batch.States[0].Resource.Label != "redis" {
		t.Fatalf("state resource = %#v", batch.States[0].Resource)
	}
	if batch.Metrics[0].Dimensions["host"] != "server-01" || batch.Metrics[0].Dimensions["collector"] != "script" {
		t.Fatalf("dimensions = %#v", batch.Metrics[0].Dimensions)
	}
}

func TestInjectUsesUTCAndDefaultMetricType(t *testing.T) {
	ts := time.Date(2026, 9, 15, 12, 0, 0, 0, time.FixedZone("offset", 7200))
	batch := Inject(pulse.Batch{Metrics: []pulse.Metric{{Name: "test.value", Timestamp: ts}}, Events: []pulse.Event{{Kind: "test.event", Timestamp: ts}}, States: []pulse.State{{Key: "test.state", Timestamp: ts}}}, Scope{}, nil)
	if batch.Metrics[0].Type != "gauge" {
		t.Fatal("missing default gauge")
	}
	for _, got := range []time.Time{batch.Metrics[0].Timestamp, batch.Events[0].Timestamp, batch.States[0].Timestamp} {
		if got.Location() != time.UTC || !got.Equal(ts) {
			t.Fatalf("timestamp: %v", got)
		}
	}
}
