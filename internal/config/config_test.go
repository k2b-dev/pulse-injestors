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
		Runner: RunnerConfig{IntervalSeconds: 30, CollectorTimeoutSeconds: 45},
		Docker: DockerConfig{RegistryTimeoutSeconds: 12},
		Linux:  LinuxConfig{SystemdUnits: []string{"ssh.service"}, PackageTimeoutSeconds: 11, DiskHealthTimeoutSeconds: 12},
	}, Overlay{
		IngestURL:                     "https://env.example/ingest",
		IngestToken:                   "env-token",
		EntityID:                      "env-node",
		EntityType:                    "container",
		TimeoutSeconds:                9,
		IntervalSeconds:               60,
		CollectorTimeoutSeconds:       90,
		DockerEnableRegistryChecks:    true,
		DockerRegistryTimeoutSeconds:  13,
		LinuxSystemdUnits:             []string{"docker.service"},
		LinuxPackageTimeoutSeconds:    21,
		LinuxDiskHealthTimeoutSeconds: 22,
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
	if cfg.Runner.IntervalSeconds != 60 || cfg.Runner.CollectorTimeoutSeconds != 90 {
		t.Fatalf("runner = %#v", cfg.Runner)
	}
	if !cfg.Docker.EnableRegistryChecks || cfg.Docker.RegistryTimeoutSeconds != 13 {
		t.Fatalf("docker = %#v", cfg.Docker)
	}
	if len(cfg.Linux.SystemdUnits) != 1 || cfg.Linux.SystemdUnits[0] != "docker.service" {
		t.Fatalf("linux units = %#v", cfg.Linux.SystemdUnits)
	}
	if cfg.Linux.PackageTimeoutSeconds != 21 || cfg.Linux.DiskHealthTimeoutSeconds != 22 {
		t.Fatalf("linux = %#v", cfg.Linux)
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
