package macos

import (
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
)

func TestKeyValueLines(t *testing.T) {
	values := keyValueLines([]byte("ProductName:\t\tmacOS\nBuildVersion:\t25C56\n"), ":")
	if values["ProductName"] != "macOS" || values["BuildVersion"] != "25C56" {
		t.Fatalf("values = %#v", values)
	}
}

func TestParseHomebrewServices(t *testing.T) {
	services, err := parseHomebrewServices([]byte(`[
  {"name":"postgresql@17","status":"started","user":"valentin","file":"/Users/valentin/Library/LaunchAgents/homebrew.mxcl.postgresql@17.plist"},
  {"name":"redis","status":"none","user":"","file":""},
  {"status":"ignored"}
]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(services) != 2 {
		t.Fatalf("services = %d", len(services))
	}
	if services[0].Name != "postgresql@17" || services[0].Status != "started" || services[0].User != "valentin" {
		t.Fatalf("first = %#v", services[0])
	}
	if services[1].Name != "redis" || services[1].Status != "none" {
		t.Fatalf("second = %#v", services[1])
	}
}

func TestEmitBatteryHealth(t *testing.T) {
	builder := monitoring.NewBuilder(monitoring.Scope{
		EntityID:   "mac",
		EntityType: "macos-device",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	emitBatteryHealth(builder, map[string]int64{
		"AppleRawMaxCapacity": 8000,
		"DesignCapacity":      10000,
		"CycleCount":          250,
		"DesignCycleCount9C":  1000,
	})
	values := map[string]float64{}
	for _, metric := range builder.Batch().Metrics {
		values[metric.Name] = metric.Value
	}
	if values["system.battery.health"] != 80 {
		t.Fatalf("health = %v", values["system.battery.health"])
	}
	if values["system.battery.cycle_usage"] != 25 {
		t.Fatalf("cycle usage = %v", values["system.battery.cycle_usage"])
	}
}

func TestIoregNumbersParsesSignedValues(t *testing.T) {
	values := ioregNumbers(`"Amperage" = -1840
"Voltage" = 12510`)
	if values["Amperage"] != -1840 {
		t.Fatalf("amperage = %d", values["Amperage"])
	}
	if values["Voltage"] != 12510 {
		t.Fatalf("voltage = %d", values["Voltage"])
	}
}

func TestMacOSResourceScopes(t *testing.T) {
	scope := monitoring.Scope{
		EntityID:   "host:macbook-01",
		EntityType: "host",
		Dimensions: map[string]string{
			"host": "macbook-01",
		},
	}

	filesystem := filesystemScope(scope, "/System/Volumes/Data")
	if filesystem.EntityType != "filesystem" || filesystem.EntityID != "filesystem:macbook-01:System_Volumes_Data" {
		t.Fatalf("filesystem scope = %#v", filesystem)
	}
	if filesystem.Label != "/System/Volumes/Data" {
		t.Fatalf("filesystem label = %q", filesystem.Label)
	}
	service := homebrewServiceScope(scope, "postgresql@17")
	if service.EntityType != "service" || service.EntityID != "service:macbook-01:homebrew:postgresql@17" {
		t.Fatalf("service scope = %#v", service)
	}
	if service.Label != "postgresql@17" {
		t.Fatalf("service label = %q", service.Label)
	}
}
