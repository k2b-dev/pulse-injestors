package btrfs

import (
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/modules/filesystem"
	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestEmitUsageParsesByteMetrics(t *testing.T) {
	builder := monitoring.NewBuilder(monitoring.Scope{
		EntityID:   "host",
		EntityType: "host",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	emitUsage(builder, filesystem.Mount{Point: "/data", Source: "/dev/sdb"}, []byte(`
Overall:
    Device size:              107374182400
    Device allocated:          21474836480
    Device unallocated:        85899345920
    Used:                      10737418240
    Free:                      96636764160
`))

	metrics := map[string]float64{}
	for _, metric := range builder.Batch().Metrics {
		metrics[metric.Name] = metric.Value
		if metric.Dimensions["mount"] != "/data" {
			t.Fatalf("dims = %#v", metric.Dimensions)
		}
		if _, ok := metric.Dimensions["source"]; ok {
			t.Fatalf("runtime source must not be a dimension: %#v", metric.Dimensions)
		}
	}
	if metrics["system.btrfs.device.size"] != 107374182400 {
		t.Fatalf("device size = %v", metrics["system.btrfs.device.size"])
	}
	if metrics["system.btrfs.used"] != 10737418240 {
		t.Fatalf("used = %v", metrics["system.btrfs.used"])
	}
	if metrics["system.btrfs.free"] != 96636764160 {
		t.Fatalf("free = %v", metrics["system.btrfs.free"])
	}
}

func TestBtrfsScopeUsesResourceEntity(t *testing.T) {
	scope := btrfsScope(monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{
			"host": "server-01",
		},
	}, filesystem.Mount{Point: "/data"})

	if scope.EntityType != "filesystem" || scope.EntityID != "filesystem:server-01:data" {
		t.Fatalf("scope = %#v", scope)
	}
}
