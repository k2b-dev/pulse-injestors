package systemd

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestCleanUnits(t *testing.T) {
	got := cleanUnits([]string{" ssh.service ", "", "docker.service", "ssh.service"})
	if len(got) != 2 || got[0] != "ssh.service" || got[1] != "docker.service" {
		t.Fatalf("got=%v", got)
	}
}

func TestUnitScopeUsesResourceEntity(t *testing.T) {
	scope := unitScope(monitoring.Scope{
		Resource: pulse.ResourceRef{Type: "host", ID: "server-01"},
	}, "docker.service")

	if scope.Resource.Type != "service" || scope.Resource.Key() != "service:server-01:systemd:docker.service" {
		t.Fatalf("scope = %#v", scope)
	}
}

func TestKeyValues(t *testing.T) {
	got := keyValues([]byte("LoadState=loaded\nActiveState=active\nDescription=A=B\n"))
	if got["LoadState"] != "loaded" || got["Description"] != "A=B" {
		t.Fatalf("got=%v", got)
	}
}

func TestFailedUnitsWithoutConfiguredUnits(t *testing.T) {
	var argsSeen []string
	c := Collector{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "systemctl" {
			t.Fatal(name)
		}
		argsSeen = args
		return []byte("z.service loaded failed failed Z\na.service loaded failed failed A\n"), nil
	}}
	b, err := c.Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "vm"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argsSeen, " ") != "list-units --state=failed --plain --no-legend --no-pager" || len(b.Metrics) != 1 || b.Metrics[0].Value != 2 {
		t.Fatalf("%v %+v", argsSeen, b)
	}
	if b.States[len(b.States)-1].Value != "a.service,z.service" {
		t.Fatal(b.States)
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	b, err = c.Collect(context.Background(), monitoring.Scope{})
	if err != nil || b.Metrics[0].Value != 0 || b.States[len(b.States)-1].Value != "" {
		t.Fatalf("none: %+v %v", b, err)
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) {
		return []byte(strings.Repeat("x", 600) + ".service loaded failed failed\n"), nil
	}
	b, err = c.Collect(context.Background(), monitoring.Scope{})
	if err != nil || len(b.States[len(b.States)-1].Value.(string)) != 500 {
		t.Fatal("unbounded failed units")
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, exec.ErrNotFound }
	b, err = c.Collect(context.Background(), monitoring.Scope{})
	if err != nil || len(b.Events) > 0 || len(b.Metrics) > 0 {
		t.Fatalf("missing: %+v %v", b, err)
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) {
		return nil, &exec.ExitError{Stderr: []byte("System has not been booted with systemd as init system")}
	}
	if _, err := c.Collect(context.Background(), monitoring.Scope{}); err != nil {
		t.Fatal(err)
	}
}
