package entity

import "testing"

func TestStableHostIDNormalizesKnownPrefixesAndSeparators(t *testing.T) {
	if got := StableHostID("host:/server:01"); got != "server_01" {
		t.Fatalf("stable host id = %q", got)
	}
}

func TestEntityIDsSanitizeComponents(t *testing.T) {
	if got := HostID("server:01"); got != "host:server_01" {
		t.Fatalf("host id = %q", got)
	}
	if got := ID("proxmox-vm", "pve/01", "100"); got != "proxmox-vm:pve_01:100" {
		t.Fatalf("entity id = %q", got)
	}
}
