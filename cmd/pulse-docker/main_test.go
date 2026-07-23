package main

import "testing"

func TestLoadConfigNormalizesDockerEntityID(t *testing.T) {
	cfg, err := loadConfig(cli{EntityID: "host:server-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Entity.ID != "server-01" {
		t.Fatalf("entity id = %q", cfg.Entity.ID)
	}
	if cfg.Entity.Type != "host" {
		t.Fatalf("entity type = %q", cfg.Entity.Type)
	}
	if cfg.Entity.Label != "server-01" {
		t.Fatalf("entity label = %q", cfg.Entity.Label)
	}
}
