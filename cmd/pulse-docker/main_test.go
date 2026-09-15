package main

import "testing"

func TestLoadConfigPreservesExplicitResourceID(t *testing.T) {
	cfg, err := loadConfig(cli{ResourceID: "host:server-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "host:server-01" {
		t.Fatalf("resource id = %q", cfg.Resource.ID)
	}
	if cfg.Resource.Type != "host" {
		t.Fatalf("resource type = %q", cfg.Resource.Type)
	}
	if cfg.Resource.Label != "host:server-01" {
		t.Fatalf("resource label = %q", cfg.Resource.Label)
	}
}
