package macos

import "testing"

func TestKeyValueLines(t *testing.T) {
	values := keyValueLines([]byte("ProductName:\t\tmacOS\nBuildVersion:\t25C56\n"), ":")
	if values["ProductName"] != "macOS" || values["BuildVersion"] != "25C56" {
		t.Fatalf("values = %#v", values)
	}
}
