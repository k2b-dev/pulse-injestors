package diskhealth

import (
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestParseSmartScan(t *testing.T) {
	got := parseSmartScan([]byte(`/dev/sda -d sat # /dev/sda
/dev/nvme0 -d nvme # /dev/nvme0
/dev/sda -d sat # duplicate
`))
	if len(got) != 2 || got[0] != "/dev/sda" || got[1] != "/dev/nvme0" {
		t.Fatalf("got=%v", got)
	}
}

func TestParseSmartHealth(t *testing.T) {
	status, healthy := parseSmartHealth([]byte("SMART overall-health self-assessment test result: PASSED\n"))
	if status != "PASSED" || !healthy {
		t.Fatalf("status=%q healthy=%v", status, healthy)
	}
	status, healthy = parseSmartHealth([]byte("SMART Health Status: FAILED\n"))
	if status != "FAILED" || healthy {
		t.Fatalf("status=%q healthy=%v", status, healthy)
	}
}

func TestParseNVMeList(t *testing.T) {
	got := parseNVMeList([]byte(`{"Devices":[{"DevicePath":"/dev/nvme0n1","ModelNumber":"M","SerialNumber":"S"}]}`))
	if len(got) != 1 || got[0].Path != "/dev/nvme0n1" || got[0].Model != "M" {
		t.Fatalf("got=%v", got)
	}
}

func TestJSONNumber(t *testing.T) {
	if v, ok := jsonNumber("42"); !ok || v != 42 {
		t.Fatalf("v=%v ok=%v", v, ok)
	}
}

func TestEmitNVMeSmartAddsCelsiusTemperature(t *testing.T) {
	builder := monitoring.NewBuilder(monitoring.Scope{
		EntityID:   "host",
		EntityType: "host",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	emitNVMeSmart(builder, map[string]string{"device": "/dev/nvme0n1"}, []byte(`{"critical_warning":0,"temperature":300}`))

	metrics := map[string]float64{}
	for _, metric := range builder.Batch().Metrics {
		metrics[metric.Name] = metric.Value
	}
	if metrics["system.disk.nvme.temperature"] != 300 {
		t.Fatalf("kelvin = %v", metrics["system.disk.nvme.temperature"])
	}
	if metrics["system.disk.nvme.temperature.celsius"] < 26.84 || metrics["system.disk.nvme.temperature.celsius"] > 26.86 {
		t.Fatalf("celsius = %v", metrics["system.disk.nvme.temperature.celsius"])
	}
}
