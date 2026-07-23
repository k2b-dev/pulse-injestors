package monitoring

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

func TestWriteReportShowsResourceDimensions(t *testing.T) {
	ts := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	err := WriteReport(&buf, pulse.Batch{
		Metrics: []pulse.Metric{
			{
				Name:      "docker.compose.service.containers",
				Type:      "gauge",
				Value:     1,
				Timestamp: ts,
				Resource:  &pulse.ResourceRef{Type: "docker-compose-service", ID: "server-01:cloud:app-core", Label: "cloud/app-core"},
				Dimensions: map[string]string{
					"compose_project": "cloud",
					"compose_service": "app-core",
				},
			},
			{
				Name:      "system.ceph.pool.objects",
				Type:      "gauge",
				Value:     100,
				Unit:      "count",
				Timestamp: ts,
				Resource:  &pulse.ResourceRef{Type: "ceph-pool", ID: "cluster:rbd", Label: "rbd"},
				Dimensions: map[string]string{
					"pool": "rbd",
					"node": "pve-01",
				},
			},
			{
				Name:      "system.battery.current_capacity",
				Type:      "gauge",
				Value:     5120,
				Unit:      "milliampere-hour",
				Timestamp: ts,
				Resource:  &pulse.ResourceRef{Type: "host", ID: "macbook-01", Label: "macbook-01"},
			},
			{
				Name:      "uptime.check.duration",
				Type:      "gauge",
				Value:     24.7,
				Unit:      "milliseconds",
				Timestamp: ts,
				Resource:  &pulse.ResourceRef{Type: "uptime-endpoint", ID: "office:pulse", Label: "Pulse"},
				Dimensions: map[string]string{
					"endpoint":   "pulse",
					"check_type": "http",
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"compose_project=cloud",
		"compose_service=app-core",
		"pool=rbd",
		"node=pve-01",
		"5120.00 mAh",
		"endpoint=pulse",
		"24.7 ms",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("report missing %q:\n%s", want, out)
		}
	}
}
