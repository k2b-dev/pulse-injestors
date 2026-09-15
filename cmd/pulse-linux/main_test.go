package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/k2b-dev/pulse-injestors/internal/config"
)

func TestLoadConfigPreservesExplicitResourceID(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: cfgPath, ResourceID: "host:server-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "host:server-01" {
		t.Fatalf("resource id = %q", cfg.Resource.ID)
	}
	if cfg.Resource.Type != "host" {
		t.Fatalf("resource type = %q", cfg.Resource.Type)
	}
}

func TestLinuxSystemdUnitsMergesProfileAndConfiguredUnits(t *testing.T) {
	got := linuxSystemdUnits(config.Config{
		Linux: config.LinuxConfig{
			Profile:      "docker-host",
			SystemdUnits: []string{"docker.service", "custom.service"},
		},
	})
	want := []string{"docker.service", "containerd.service", "ssh.service", "sshd.service", "custom.service"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("units = %#v", got)
	}
}

func TestProfileSystemdUnitsUnknownProfileIsNoop(t *testing.T) {
	if got := profileSystemdUnits("unknown"); got != nil {
		t.Fatalf("units = %#v", got)
	}
}
