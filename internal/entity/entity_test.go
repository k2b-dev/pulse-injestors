package entity

import "testing"

func TestResourceComponentsDoNotCollide(t *testing.T) {
	values := []string{"/", "/root", "root", "/var/lib", "/var_lib", "var:lib", "var_lib", "var%3Alib", "var lib"}
	seen := map[string]string{}
	for _, value := range values {
		id := ID("server-01", value)
		if previous, ok := seen[id]; ok {
			t.Fatalf("%q and %q collide: %q", previous, value, id)
		}
		seen[id] = value
	}
	if ID("a:b", "c") == ID("a", "b:c") {
		t.Fatal("component boundaries collide")
	}
}

func TestStableHostIDIsNotALegacyParser(t *testing.T) {
	if got := StableHostID(" host:server-01 "); got != "host:server-01" {
		t.Fatalf("got %q", got)
	}
}
