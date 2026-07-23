package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigNormalizesHostEntityID(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: cfgPath, EntityID: "host:pve-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Entity.ID != "pve-01" {
		t.Fatalf("entity id = %q", cfg.Entity.ID)
	}
	if cfg.Entity.Type != "host" {
		t.Fatalf("entity type = %q", cfg.Entity.Type)
	}
}
