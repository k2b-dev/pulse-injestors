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

func TestParseSmartAttributes(t *testing.T) {
	got := parseSmartAttributes([]byte(`
ID# ATTRIBUTE_NAME          FLAG     VALUE WORST THRESH TYPE      UPDATED  WHEN_FAILED RAW_VALUE
  5 Reallocated_Sector_Ct   0x0033   100   100   010    Pre-fail  Always       -       2
  9 Power_On_Hours          0x0032   088   088   000    Old_age   Always       -       12345
194 Temperature_Celsius     0x0022   064   054   000    Old_age   Always       -       36 (Min/Max 20/50)
197 Current_Pending_Sector  0x0012   100   100   000    Old_age   Always       -       1
198 Offline_Uncorrectable   0x0010   100   100   000    Old_age   Offline      -       0
199 UDMA_CRC_Error_Count    0x003e   200   200   000    Old_age   Always       -       7
`))
	values := map[string]float64{}
	for _, attr := range got {
		values[attr.Name] = attr.Value
	}
	if values["reallocated_sector_ct"] != 2 {
		t.Fatalf("reallocated = %v", values["reallocated_sector_ct"])
	}
	if values["power_on_hours"] != 12345 {
		t.Fatalf("power on hours = %v", values["power_on_hours"])
	}
	if values["temperature_celsius"] != 36 {
		t.Fatalf("temperature = %v", values["temperature_celsius"])
	}
	if values["current_pending_sector"] != 1 || values["offline_uncorrectable"] != 0 || values["udma_crc_error_count"] != 7 {
		t.Fatalf("values = %#v", values)
	}
}

func TestEmitSmartAttributesAddsCommonMetrics(t *testing.T) {
	builder := monitoring.NewBuilder(monitoring.Scope{
		EntityID:   "host",
		EntityType: "host",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	emitSmartAttributes(builder, map[string]string{"device": "/dev/sda"}, []byte(`
ID# ATTRIBUTE_NAME          FLAG     VALUE WORST THRESH TYPE      UPDATED  WHEN_FAILED RAW_VALUE
  5 Reallocated_Sector_Ct   0x0033   100   100   010    Pre-fail  Always       -       2
  9 Power_On_Hours          0x0032   088   088   000    Old_age   Always       -       12345
194 Temperature_Celsius     0x0022   064   054   000    Old_age   Always       -       36
`))
	metrics := map[string]int{}
	values := map[string]float64{}
	for _, metric := range builder.Batch().Metrics {
		metrics[metric.Name]++
		values[metric.Name] = metric.Value
	}
	if metrics["system.disk.smart.attribute.raw"] != 3 {
		t.Fatalf("attribute metrics = %d", metrics["system.disk.smart.attribute.raw"])
	}
	if values["system.disk.smart.reallocated_sectors"] != 2 {
		t.Fatalf("reallocated metric = %v", values["system.disk.smart.reallocated_sectors"])
	}
	if values["system.disk.smart.power_on_hours"] != 12345 {
		t.Fatalf("power hours metric = %v", values["system.disk.smart.power_on_hours"])
	}
	if values["system.disk.smart.temperature"] != 36 {
		t.Fatalf("temperature metric = %v", values["system.disk.smart.temperature"])
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
