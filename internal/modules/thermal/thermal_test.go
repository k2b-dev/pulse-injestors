package thermal

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestThermalSensorsRemainOnHostResource(t *testing.T) {
	sys := t.TempDir()
	zone := filepath.Join(sys, "class", "thermal", "thermal_zone0")
	if err := os.MkdirAll(zone, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zone, "temp"), []byte("51200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(zone, "type"), []byte("x86_pkg_temp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	batch, err := Collector{SysRoot: sys}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{
			"host": "server-01",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) != 1 {
		t.Fatalf("metrics = %d", len(batch.Metrics))
	}
	metric := batch.Metrics[0]
	if metric.EntityType != "host" || metric.EntityID != "host:server-01" {
		t.Fatalf("metric entity = %s %s", metric.EntityType, metric.EntityID)
	}
	if metric.Resource == nil || metric.Resource.Label != "server-01" {
		t.Fatalf("resource = %#v", metric.Resource)
	}
	if metric.Dimensions["sensor"] != "thermal_zone0" || metric.Dimensions["type"] != "x86_pkg_temp" {
		t.Fatalf("dimensions = %#v", metric.Dimensions)
	}
}
