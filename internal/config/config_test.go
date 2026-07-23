package config

import "testing"

func TestResolveOverlayBeatsFile(t *testing.T) {
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{
			IngestURL:   "https://file.example/ingest",
			IngestToken: "file-token",
		},
		Entity: EntityConfig{ID: "file-node", Label: "File node", Type: "host"},
		Dimensions: map[string]string{
			"environment": "staging",
		},
		HTTP:   HTTPConfig{TimeoutSeconds: 5, MaxRetries: 1, InitialBackoffMS: 100},
		Runner: RunnerConfig{IntervalSeconds: 30, CollectorTimeoutSeconds: 45},
		Docker: DockerConfig{RegistryTimeoutSeconds: 12},
		Linux:  LinuxConfig{Profile: "server", SystemdUnits: []string{"ssh.service"}, PackageTimeoutSeconds: 11, DiskHealthTimeoutSeconds: 12},
	}, Overlay{
		IngestURL:                     "https://env.example/ingest",
		IngestToken:                   "env-token",
		EntityID:                      "env-node",
		EntityLabel:                   "Environment node",
		EntityType:                    "container",
		Dimensions:                    map[string]string{"environment": "production", "region": "eu-central"},
		TimeoutSeconds:                9,
		IntervalSeconds:               60,
		CollectorTimeoutSeconds:       90,
		DockerEnableRegistryChecks:    true,
		DockerRegistryTimeoutSeconds:  13,
		LinuxProfile:                  "docker-host",
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
	if cfg.Entity.ID != "env-node" || cfg.Entity.Label != "Environment node" || cfg.Entity.Type != "container" {
		t.Fatalf("entity = %#v", cfg.Entity)
	}
	if cfg.Dimensions["environment"] != "production" || cfg.Dimensions["region"] != "eu-central" {
		t.Fatalf("dimensions = %#v", cfg.Dimensions)
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
	if cfg.Linux.Profile != "docker-host" {
		t.Fatalf("linux profile = %q", cfg.Linux.Profile)
	}
	if cfg.Linux.PackageTimeoutSeconds != 21 || cfg.Linux.DiskHealthTimeoutSeconds != 22 {
		t.Fatalf("linux = %#v", cfg.Linux)
	}
}

func TestParseDimensions(t *testing.T) {
	dimensions, err := ParseDimensions([]string{"environment=production", "region=eu-central"})
	if err != nil {
		t.Fatal(err)
	}
	if dimensions["environment"] != "production" || dimensions["region"] != "eu-central" {
		t.Fatalf("dimensions = %#v", dimensions)
	}
	if _, err := ParseDimensions([]string{"missing-value"}); err == nil {
		t.Fatal("expected invalid dimension error")
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

func TestResolveMacOSSystemProfilerDefaultsEnabled(t *testing.T) {
	cfg, err := Resolve(Config{}, Overlay{EntityID: "node", AllowMissingPulse: true})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.SystemProfilerEnabled() {
		t.Fatal("expected system profiler enabled")
	}
	if cfg.MacOS.SystemProfilerTimeoutSeconds != 10 {
		t.Fatalf("timeout = %d", cfg.MacOS.SystemProfilerTimeoutSeconds)
	}
}

func TestResolveMacOSSystemProfilerCanBeDisabled(t *testing.T) {
	enabled := false
	cfg, err := Resolve(Config{
		Pulse:  PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{ID: "node"},
		MacOS:  MacOSConfig{EnableSystemProfiler: &enabled, SystemProfilerTimeoutSeconds: 12},
	}, Overlay{
		DisableSystemProfiler:        true,
		SystemProfilerTimeoutSeconds: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SystemProfilerEnabled() {
		t.Fatal("expected system profiler disabled")
	}
	if cfg.MacOS.SystemProfilerTimeoutSeconds != 15 {
		t.Fatalf("timeout = %d", cfg.MacOS.SystemProfilerTimeoutSeconds)
	}
}

func TestResolveProxmoxOverlay(t *testing.T) {
	enableCephAPI := true
	enableLocalCeph := true
	cfg, err := Resolve(Config{
		Pulse:   PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity:  EntityConfig{ID: "node"},
		Proxmox: ProxmoxConfig{PveshPath: "/usr/sbin/pvesh", TimeoutSeconds: 11},
	}, Overlay{
		ProxmoxPveshPath:       "/custom/pvesh",
		ProxmoxTimeoutSeconds:  12,
		ProxmoxEnableCephAPI:   &enableCephAPI,
		ProxmoxEnableLocalCeph: &enableLocalCeph,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Proxmox.PveshPath != "/custom/pvesh" {
		t.Fatalf("proxmox = %#v", cfg.Proxmox)
	}
	if cfg.Proxmox.TimeoutSeconds != 12 || !cfg.ProxmoxCephAPIEnabled() || !cfg.ProxmoxLocalCephEnabled() {
		t.Fatalf("proxmox = %#v", cfg.Proxmox)
	}
}

func TestResolveProxmoxOverlayCanDisableCeph(t *testing.T) {
	enabled := true
	disabled := false
	cfg, err := Resolve(Config{
		Pulse:  PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{ID: "node"},
		Proxmox: ProxmoxConfig{
			EnableCephAPI:   &enabled,
			EnableLocalCeph: &enabled,
		},
	}, Overlay{
		ProxmoxEnableCephAPI:   &disabled,
		ProxmoxEnableLocalCeph: &disabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxmoxCephAPIEnabled() || cfg.ProxmoxLocalCephEnabled() {
		t.Fatalf("expected proxmox ceph disabled: %#v", cfg.Proxmox)
	}
}

func TestResolvePBSOverlay(t *testing.T) {
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{
			ID: "node",
		},
		PBS: PBSConfig{CommandPath: "/usr/sbin/proxmox-backup-debug", TimeoutSeconds: 11},
	}, Overlay{
		PBSCommandPath:    "/custom/proxmox-backup-debug",
		PBSTimeoutSeconds: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PBS.CommandPath != "/custom/proxmox-backup-debug" {
		t.Fatalf("pbs = %#v", cfg.PBS)
	}
	if cfg.PBS.TimeoutSeconds != 12 {
		t.Fatalf("pbs = %#v", cfg.PBS)
	}
}

func TestResolveMergesHostZfsFlag(t *testing.T) {
	enabled := false
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{
			ID: "node",
		},
		Host: HostConfig{EnableZfs: &enabled},
	}, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ZfsEnabled() {
		t.Fatal("expected zfs disabled")
	}
}

func TestResolveHostCephDefaultsEnabled(t *testing.T) {
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{
			ID: "node",
		},
	}, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.CephEnabled() {
		t.Fatal("expected ceph enabled by default")
	}
}

func TestResolveMergesHostCephFlag(t *testing.T) {
	enabled := false
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{
			ID: "node",
		},
		Host: HostConfig{EnableCeph: &enabled},
	}, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CephEnabled() {
		t.Fatal("expected ceph disabled")
	}
}

func TestResolveProxmoxCephDefaultsEnabled(t *testing.T) {
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{
			ID: "node",
		},
	}, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ProxmoxCephAPIEnabled() || !cfg.ProxmoxLocalCephEnabled() {
		t.Fatalf("expected proxmox ceph defaults enabled: %#v", cfg.Proxmox)
	}
}

func TestResolveMergesProxmoxCephOptOut(t *testing.T) {
	enabled := false
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/ingest", IngestToken: "token"},
		Entity: EntityConfig{
			ID: "node",
		},
		Proxmox: ProxmoxConfig{EnableCephAPI: &enabled, EnableLocalCeph: &enabled},
	}, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProxmoxCephAPIEnabled() || cfg.ProxmoxLocalCephEnabled() {
		t.Fatalf("expected proxmox ceph disabled: %#v", cfg.Proxmox)
	}
}

func TestResolveUptimeDefaultsAndTargets(t *testing.T) {
	cfg, err := Resolve(Config{
		Uptime: UptimeConfig{
			Targets: []UptimeTargetConfig{{
				ID:             "pulse",
				Label:          "Pulse",
				Kind:           "http",
				Address:        "https://pulse.example.test/health",
				ExpectedStatus: 200,
				TimeoutSeconds: 3,
			}},
		},
	}, Overlay{
		EntityID:          "probe-01",
		AllowMissingPulse: true,
		UptimeConcurrency: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UptimeDefaultsEnabled() {
		t.Fatal("expected uptime defaults enabled")
	}
	if cfg.Uptime.Concurrency != 8 || cfg.Uptime.TimeoutSeconds != 5 {
		t.Fatalf("uptime = %#v", cfg.Uptime)
	}
	if len(cfg.Uptime.Targets) != 1 || cfg.Uptime.Targets[0].ID != "pulse" {
		t.Fatalf("targets = %#v", cfg.Uptime.Targets)
	}
}

func TestResolveUptimeDefaultsCanBeDisabled(t *testing.T) {
	cfg, err := Resolve(Config{}, Overlay{
		EntityID:              "probe-01",
		AllowMissingPulse:     true,
		UptimeDisableDefaults: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UptimeDefaultsEnabled() {
		t.Fatal("expected uptime defaults disabled")
	}
}

func TestResolveRejectsInvalidUptimeTargets(t *testing.T) {
	tests := []UptimeTargetConfig{
		{ID: "bad id", Label: "Bad", Kind: "dns", Address: "example.com"},
		{ID: "missing-label", Kind: "dns", Address: "example.com"},
		{ID: "bad-kind", Label: "Bad", Kind: "smtp", Address: "example.com"},
		{ID: "bad-icmp", Label: "Bad", Kind: "icmp", Address: "example.com"},
		{ID: "bad-dns", Label: "Bad", Kind: "dns", Address: "https://example.com"},
		{ID: "bad-tcp", Label: "Bad", Kind: "tcp", Address: "example.com"},
		{ID: "bad-http", Label: "Bad", Kind: "http", Address: "file:///tmp/check"},
		{ID: "credentials", Label: "Bad", Kind: "http", Address: "https://user:secret@example.com/"},
		{ID: "bad-status", Label: "Bad", Kind: "http", Address: "https://example.com/", ExpectedStatus: 99},
		{ID: "bad-timeout", Label: "Bad", Kind: "dns", Address: "example.com", TimeoutSeconds: -1},
	}
	for _, target := range tests {
		_, err := Resolve(Config{
			Uptime: UptimeConfig{Targets: []UptimeTargetConfig{target}},
		}, Overlay{EntityID: "probe-01", AllowMissingPulse: true})
		if err == nil {
			t.Fatalf("expected target %#v to fail", target)
		}
	}
}
