package main

import (
	"fmt"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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

func TestCollectorCadences(t *testing.T) {
	cfg, err := config.Resolve(config.Config{}, config.Overlay{AllowMissingPulse: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Scripts = []config.ScriptConfig{{Name: "a", Command: []string{"true"}}, {Name: "b", Command: []string{"true"}, CadenceSeconds: 120}}
	cfg.Runner.Cadence = map[string]int{"network": 600, "script:b": 180}
	seen := map[string]bool{}
	for _, c := range collectors(cfg) {
		want := linuxCadences[c.Name()]
		if c.Name() == "network" {
			want = 600 * time.Second
		}
		if c.Name() == "script:a" {
			want = 60 * time.Second
		}
		if c.Name() == "script:b" {
			want = 180 * time.Second
		}
		if want == 0 || c.(monitoring.Cadenced).Cadence() != want {
			t.Fatalf("%s cadence = %s, want %s", c.Name(), c.(monitoring.Cadenced).Cadence(), want)
		}
		if seen[c.Name()] {
			t.Fatal("duplicate collector name")
		}
		seen[c.Name()] = true
	}
	for name := range linuxCadences {
		if name == "ceph" {
			if seen[name] {
				t.Fatal("Ceph enabled by default")
			}
			continue
		}
		if !seen[name] {
			t.Fatalf("missing %s", name)
		}
	}
}

func TestLoadConfigValidatesCadenceNames(t *testing.T) {
	for _, name := range []string{"inventroy", "script:missing", "inventory", "script:custom", "thermal"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			body := fmt.Sprintf("[runner.cadence]\n%q = 86400\n[[script]]\nname = \"custom\"\ncommand = [\"true\"]\n", name)
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := loadConfig(cli{Config: path, Local: true})
			if name == "inventroy" || name == "script:missing" {
				if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "valid names:") {
					t.Fatalf("unknown collector error: %v", err)
				}
				for valid := range linuxCadences {
					if !strings.Contains(err.Error(), valid) {
						t.Fatalf("missing valid collector %q: %v", valid, err)
					}
				}
				if !strings.Contains(err.Error(), "script:custom") {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProxmoxAndPBSProfiles(t *testing.T) {
	for profile, want := range map[string]int{"proxmox-node": 7, "pbs": 2} {
		units := profileSystemdUnits(profile)
		if len(units) != want {
			t.Fatalf("%s: %v", profile, units)
		}
		for _, unit := range units {
			if strings.Contains(unit, "*") {
				t.Fatal("systemd module does not expand globs")
			}
		}
	}
}
