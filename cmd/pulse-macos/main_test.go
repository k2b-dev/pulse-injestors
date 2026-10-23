package main

import (
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigPreservesExplicitResourceID(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: cfgPath, ResourceID: "host:macbook-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "host:macbook-01" {
		t.Fatalf("resource id = %q", cfg.Resource.ID)
	}
	if cfg.Resource.Type != "host" {
		t.Fatalf("resource type = %q", cfg.Resource.Type)
	}
}

func TestWorkstationDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: path, Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RunnerInterval() != 300*time.Second || !cfg.SoftwareUpdateEnabled() {
		t.Fatal("workstation defaults")
	}
	seen := map[string]bool{}
	for _, c := range collectors(cfg) {
		if seen[c.Name()] {
			t.Fatal("duplicate collector")
		}
		seen[c.Name()] = true
		want := 24 * time.Hour
		switch c.Name() {
		case "macos-health", "macos-filesystem":
			want = 300 * time.Second
		case "macos-updates", "macos-posture":
			want = time.Hour
		}
		if c.(monitoring.Cadenced).Cadence() != want {
			t.Fatal(c.Name())
		}
	}
	if len(seen) != 8 {
		t.Fatal(seen)
	}
}
