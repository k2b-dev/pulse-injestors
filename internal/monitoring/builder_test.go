package monitoring

import (
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

func TestInjectAddsScopeAndDimensions(t *testing.T) {
	ts := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	batch := Inject(pulse.Batch{
		Metrics: []pulse.Metric{{Name: "x", Type: "gauge", Value: 1, Dimensions: map[string]string{"b": "2"}}},
	}, Scope{
		EntityID:   "node",
		EntityType: "host",
		Timestamp:  ts,
		Dimensions: map[string]string{"a": "1"},
	}, map[string]string{"collector": "test"})
	got := batch.Metrics[0]
	if got.EntityID != "node" || got.EntityType != "host" || !got.Timestamp.Equal(ts) {
		t.Fatalf("scope not injected: %#v", got)
	}
	if got.Dimensions["a"] != "1" || got.Dimensions["b"] != "2" || got.Dimensions["collector"] != "test" {
		t.Fatalf("dimensions = %#v", got.Dimensions)
	}
}
