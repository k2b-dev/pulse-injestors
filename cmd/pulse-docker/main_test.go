package main

import (
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"testing"
	"time"
)

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

func TestDockerCollectorsKeepIndependentCadencesAndStatsBudget(t *testing.T) {
	cfg, err := loadConfig(cli{ResourceID: "fixture", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Docker.StatsTimeoutSeconds = 147
	found := map[string]bool{}
	for _, collector := range collectors(cfg) {
		if collector.Name() != "docker" && collector.Name() != "docker.stats" && collector.Name() != "docker.inventory" {
			continue
		}
		found[collector.Name()] = true
		cadence, ok := collector.(monitoring.Cadenced)
		if !ok {
			t.Fatalf("%s has no cadence", collector.Name())
		}
		expected := map[string]time.Duration{"docker": time.Minute, "docker.stats": 5 * time.Minute, "docker.inventory": 24 * time.Hour}[collector.Name()]
		if cadence.Cadence() != expected {
			t.Fatalf("%s cadence = %v", collector.Name(), cadence.Cadence())
		}
		if collector.Name() == "docker.stats" {
			timeout, ok := collector.(monitoring.Timeouter)
			if !ok || timeout.CollectTimeout() != 147*time.Second {
				t.Fatalf("stats budget not wired: %#v", collector)
			}
		}
	}
	if len(found) != 3 {
		t.Fatalf("missing Docker phases: %v", found)
	}
}
