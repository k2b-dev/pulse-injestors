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
	cfg, err := loadConfig(cli{Config: cfgPath, ResourceID: "host:pve-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "host:pve-01" {
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
	for i, want := range []time.Duration{60 * time.Second, 300 * time.Second, 900 * time.Second, 24 * time.Hour} {
		if got[i].(monitoring.Cadenced).Cadence() != want || !strings.HasPrefix(got[i].Name(), "proxmox.") {
			t.Fatal(got[i])
		}
	}
}

func TestConfiguredBackupBudgetSurvivesWrappers(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte("[proxmox]\nbackup_timeout_seconds = 240\nlist_timeout_seconds = 150\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: cfgPath, Local: true})
	if err != nil {
		t.Fatal(err)
	}
	r := monitoring.Runner{Timeout: cfg.CollectorTimeout(), Collectors: collectors(cfg)}
	if got := r.MaxCycleTimeout(); got != 240*time.Second+time.Duration(len(r.Collectors)-1)*cfg.CollectorTimeout() {
		t.Fatalf("cycle budget: %s", got)
	}
	for _, c := range r.Collectors {
		want := time.Duration(0)
		if c.Name() == "proxmox.backups" {
			want = 240 * time.Second
		}
		if got := c.(monitoring.Timeouter).CollectTimeout(); got != want {
			t.Fatalf("%s: %s", c.Name(), got)
		}
	}
}
