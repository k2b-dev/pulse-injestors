package systemd

import (
	"testing"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestCleanUnits(t *testing.T) {
	got := cleanUnits([]string{" ssh.service ", "", "docker.service", "ssh.service"})
	if len(got) != 2 || got[0] != "ssh.service" || got[1] != "docker.service" {
		t.Fatalf("got=%v", got)
	}
}

func TestUnitScopeUsesResourceEntity(t *testing.T) {
	scope := unitScope(monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{
			"host": "server-01",
		},
	}, "docker.service")

	if scope.EntityType != "service" || scope.EntityID != "service:server-01:systemd:docker.service" {
		t.Fatalf("scope = %#v", scope)
	}
}

func TestKeyValues(t *testing.T) {
	got := keyValues([]byte("LoadState=loaded\nActiveState=active\nDescription=A=B\n"))
	if got["LoadState"] != "loaded" || got["Description"] != "A=B" {
		t.Fatalf("got=%v", got)
	}
}
