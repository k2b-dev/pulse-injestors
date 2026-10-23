package main

import (
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigPreservesExplicitResourceID(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: cfgPath, ResourceID: "host:pbs-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "host:pbs-01" {
		t.Fatalf("resource id = %q", cfg.Resource.ID)
	}
	if cfg.Resource.Type != "host" {
		t.Fatalf("resource type = %q", cfg.Resource.Type)
	}
}

func TestSpecializedCollectors(t *testing.T) {
	cfg, err := loadConfig(cli{Local: true})
	if err != nil {
		t.Fatal(err)
	}
	got := collectors(cfg)
	if len(got) != 4 {
		t.Fatalf("unexpected collectors: %v", got)
	}
	for i, want := range []time.Duration{300 * time.Second, 900 * time.Second, 900 * time.Second, 24 * time.Hour} {
		if got[i].(monitoring.Cadenced).Cadence() != want || !strings.HasPrefix(got[i].Name(), "pbs.") {
			t.Fatal(got[i])
		}
	}
}
