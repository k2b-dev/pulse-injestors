package diskhealth

import "testing"

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
