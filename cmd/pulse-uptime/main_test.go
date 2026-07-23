package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigBuildsStableUptimeProbe(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[uptime]
enable_defaults = false

[[uptime.target]]
id = "pulse"
label = "Pulse"
kind = "http"
address = "https://pulse.example.test/health"
expected_status = 200
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{
		Config:   cfgPath,
		EntityID: "host:office:probe",
		Local:    true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Entity.ID != "office_probe" || cfg.Entity.Type != "uptime-probe" {
		t.Fatalf("entity = %#v", cfg.Entity)
	}
	targets, err := configuredTargets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].ID != "pulse" {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestConfiguredTargetsUsesBuiltInProfile(t *testing.T) {
	cfg, err := loadConfig(cli{EntityID: "office", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := configuredTargets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 8 {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestConfiguredTargetsRejectsEmptyProfile(t *testing.T) {
	cfg, err := loadConfig(cli{EntityID: "office", Local: true, DisableDefaults: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuredTargets(cfg); err == nil {
		t.Fatal("expected missing target error")
	}
}

func TestConfiguredTargetsRejectsBuiltInIDConflict(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, []byte(`
[[uptime.target]]
id = "cloudflare-icmp"
label = "Custom"
kind = "icmp"
address = "192.0.2.1"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: cfgPath, EntityID: "office", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuredTargets(cfg); err == nil {
		t.Fatal("expected built-in target conflict")
	}
}
