package config

import "testing"

func TestResolveOverlayBeatsFile(t *testing.T) {
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{
			IngestURL:   "https://file.example/ingest",
			IngestToken: "file-token",
		},
		Entity: EntityConfig{ID: "file-node", Type: "host"},
		HTTP:   HTTPConfig{TimeoutSeconds: 5, MaxRetries: 1, InitialBackoffMS: 100},
		Runner: RunnerConfig{IntervalSeconds: 30},
	}, Overlay{
		IngestURL:       "https://env.example/ingest",
		IngestToken:     "env-token",
		EntityID:        "env-node",
		EntityType:      "container",
		TimeoutSeconds:  9,
		IntervalSeconds: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pulse.IngestURL != "https://env.example/ingest" {
		t.Fatalf("ingest url = %q", cfg.Pulse.IngestURL)
	}
	if cfg.Pulse.IngestToken != "env-token" {
		t.Fatalf("ingest token = %q", cfg.Pulse.IngestToken)
	}
	if cfg.Entity.ID != "env-node" || cfg.Entity.Type != "container" {
		t.Fatalf("entity = %#v", cfg.Entity)
	}
	if cfg.HTTP.TimeoutSeconds != 9 || cfg.HTTP.MaxRetries != 1 {
		t.Fatalf("http = %#v", cfg.HTTP)
	}
	if cfg.Runner.IntervalSeconds != 60 {
		t.Fatalf("runner = %#v", cfg.Runner)
	}
}

func TestResolveRequiresPulseEndpointAndToken(t *testing.T) {
	_, err := Resolve(Config{}, Overlay{EntityID: "node"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAllowsMissingPulseForLocalMode(t *testing.T) {
	cfg, err := Resolve(Config{}, Overlay{EntityID: "node", AllowMissingPulse: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Entity.ID != "node" {
		t.Fatalf("entity id = %q", cfg.Entity.ID)
	}
}
