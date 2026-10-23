package config

import (
	"fmt"
	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveOverlayBeatsFile(t *testing.T) {
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{
			IngestURL:   "https://file.example/api/pulse/ingest",
			IngestToken: "file-token",
		},
		Resource: ResourceConfig{ID: "file-node", Label: "File node", Type: "host"},
		Dimensions: map[string]string{
			"environment": "staging",
		},
		HTTP:   HTTPConfig{TimeoutSeconds: 5, MaxRetries: 1, InitialBackoffMS: 100},
		Runner: RunnerConfig{IntervalSeconds: 30, CollectorTimeoutSeconds: 45},
		Docker: DockerConfig{RegistryTimeoutSeconds: 12},
		Linux:  LinuxConfig{Profile: "server", SystemdUnits: []string{"ssh.service"}, PackageTimeoutSeconds: 11, DiskHealthTimeoutSeconds: 12},
	}, Overlay{
		IngestURL:                     "https://env.example/api/pulse/ingest",
		IngestToken:                   "env-token",
		ResourceID:                    "env-node",
		ResourceLabel:                 "Environment node",
		ResourceType:                  "container",
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
	if cfg.Pulse.IngestURL != "https://env.example/api/pulse/ingest" {
		t.Fatalf("ingest url = %q", cfg.Pulse.IngestURL)
	}
	if cfg.Pulse.IngestToken != "env-token" {
		t.Fatalf("ingest token = %q", cfg.Pulse.IngestToken)
	}
	if cfg.Resource.ID != "env-node" || cfg.Resource.Label != "Environment node" || cfg.Resource.Type != "container" {
		t.Fatalf("entity = %#v", cfg.Resource)
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
	_, err := Resolve(Config{}, Overlay{ResourceID: "node"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveAllowsMissingPulseForLocalMode(t *testing.T) {
	cfg, err := Resolve(Config{}, Overlay{ResourceID: "node", AllowMissingPulse: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "node" {
		t.Fatalf("resource id = %q", cfg.Resource.ID)
	}
}

func TestResolveMacOSSystemProfilerDefaultsEnabled(t *testing.T) {
	cfg, err := Resolve(Config{}, Overlay{ResourceID: "node", AllowMissingPulse: true})
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
		Pulse:    PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{ID: "node"},
		MacOS:    MacOSConfig{EnableSystemProfiler: &enabled, SystemProfilerTimeoutSeconds: 12},
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
		Pulse:    PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{ID: "node"},
		Proxmox:  ProxmoxConfig{PveshPath: "/usr/sbin/pvesh", TimeoutSeconds: 11},
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
		Pulse:    PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{ID: "node"},
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
		Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{
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
		Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{
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

func TestResolveHostCephDefaultsDisabled(t *testing.T) {
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{
			ID: "node",
		},
	}, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CephEnabled() {
		t.Fatal("expected ceph disabled by default")
	}
}

func TestResolveMergesHostCephFlag(t *testing.T) {
	enabled := false
	cfg, err := Resolve(Config{
		Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{
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
		Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{
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
		Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "token"},
		Resource: ResourceConfig{
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
		ResourceID:        "probe-01",
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
		ResourceID:            "probe-01",
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
		}, Overlay{ResourceID: "probe-01", AllowMissingPulse: true})
		if err == nil {
			t.Fatalf("expected target %#v to fail", target)
		}
	}
}

func TestSpoolRequiresExplicitByteBudget(t *testing.T) {
	_, err := Resolve(Config{Pulse: PulseConfig{SpoolDir: "/tmp/private-spool"}}, Overlay{AllowMissingPulse: true})
	if err == nil {
		t.Fatal("spool without byte budget accepted")
	}
	cfg, err := Resolve(Config{}, Overlay{AllowMissingPulse: true, SpoolDir: "/tmp/private-spool", SpoolMaxBytes: 1024})
	if err != nil || cfg.Pulse.SpoolMaxBytes != 1024 {
		t.Fatalf("spool config: %#v %v", cfg.Pulse, err)
	}
}

func TestLoadFileRejectsLegacyAndUnknownConfig(t *testing.T) {
	for _, data := range []string{"[entity]\nid = \"old\"\n", "[pulse]\nunknown = true\n"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadFile(path, true); err == nil {
			t.Fatal("unknown config accepted")
		}
	}
}

func TestResolveReadsPrivateTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte("file-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	base := Config{Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestTokenFile: path}}
	cfg, err := Resolve(base, Overlay{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pulse.IngestToken != "file-secret" {
		t.Fatalf("token = %q", cfg.Pulse.IngestToken)
	}
	// The environment token replaces the configured file.
	cfg, err = Resolve(base, Overlay{IngestToken: "env-secret"})
	if err != nil || cfg.Pulse.IngestToken != "env-secret" {
		t.Fatalf("env override: %q %v", cfg.Pulse.IngestToken, err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(base, Overlay{}); err == nil || !strings.Contains(err.Error(), "other users") {
		t.Fatalf("world-readable token accepted: %v", err)
	}
	both := Config{Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestToken: "a", IngestTokenFile: path}}
	if _, err := Resolve(both, Overlay{}); err == nil {
		t.Fatal("token and token file accepted together")
	}
	if err := os.WriteFile(path, []byte("two tokens\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(base, Overlay{}); err == nil {
		t.Fatal("malformed token file accepted")
	}
}

func TestResolveUsesSystemdCredential(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, CredentialName), []byte("cred-secret"), 0400); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	cfg, err := Resolve(Config{Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest"}}, Overlay{})
	if err != nil || cfg.Pulse.IngestToken != "cred-secret" {
		t.Fatalf("credential: %q %v", cfg.Pulse.IngestToken, err)
	}
}

func TestResolveLocalSkipsTokenFilesAndCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CREDENTIALS_DIRECTORY", dir)
	for _, path := range []string{filepath.Join(dir, "missing-token"), ""} {
		base := Config{Pulse: PulseConfig{IngestURL: "https://example.com/api/pulse/ingest", IngestTokenFile: path}}
		cfg, err := Resolve(base, Overlay{AllowMissingPulse: true})
		if err != nil || cfg.Pulse.IngestToken != "" {
			t.Fatalf("local resolution: token=%q err=%v", cfg.Pulse.IngestToken, err)
		}
		if _, err := Resolve(base, Overlay{}); err == nil {
			t.Fatal("non-local resolution accepted missing token file/credential")
		}
	}
}

func TestLinuxSliceConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	body := `[runner.cadence]
network = 600
"script:custom" = 120
[linux]
network_exclude = []
certificate_paths = ["/etc/certs/*.pem"]
[[backup]]
name = "db"
marker_file = "/var/lib/backup/ok"
[[script]]
name = "custom"
command = ["true"]
cadence_seconds = 120
`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := LoadFile(path, true)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Resolve(file, Overlay{AllowMissingPulse: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Linux.NetworkExclude == nil || cfg.Runner.Cadence["network"] != 600 || len(cfg.Backups) != 1 || cfg.Scripts[0].CadenceSeconds != 120 {
		t.Fatalf("config lost: %+v", cfg)
	}
	for _, bad := range []Config{
		{Runner: RunnerConfig{Cadence: map[string]int{"network": 59}}},
		{Runner: RunnerConfig{Cadence: map[string]int{"": 60}}},
		{Linux: LinuxConfig{NetworkExclude: []string{"["}}},
		{Linux: LinuxConfig{CertificatePaths: []string{"["}}},
		{Backups: []BackupConfig{{Name: "db"}}},
		{Backups: []BackupConfig{{Name: "db", MarkerFile: "a", Command: []string{"true"}}}},
		{Backups: []BackupConfig{{Name: "db", MarkerFile: "a"}, {Name: "db", MarkerFile: "b"}}},
		{Scripts: []ScriptConfig{{Name: "x", Command: []string{"true"}, CadenceSeconds: 59}}},
	} {
		if _, err := Resolve(bad, Overlay{AllowMissingPulse: true}); err == nil {
			t.Fatalf("accepted invalid config: %+v", bad)
		}
	}
}

func TestReservedHostDimension(t *testing.T) {
	const want = "dimension 'host' is reserved; the resource id identifies the host"
	for _, value := range []string{"other-host", ""} {
		for _, overlay := range []bool{false, true} {
			file, overrides := Config{}, Overlay{AllowMissingPulse: true}
			if overlay {
				overrides.Dimensions = map[string]string{"host": value}
			} else {
				file.Dimensions = map[string]string{"host": value}
			}
			if _, err := Resolve(file, overrides); err == nil || err.Error() != want {
				t.Fatalf("value=%q overlay=%v: %v", value, overlay, err)
			}
		}
	}
	if _, err := ParseDimensions([]string{"host=other-host"}); err == nil || err.Error() != want {
		t.Fatalf("CLI host dimension: %v", err)
	}
	cfg, err := Resolve(Config{Resource: ResourceConfig{ID: "resource-host"}, Dimensions: map[string]string{"site": "test"}},
		Overlay{AllowMissingPulse: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := entity.StableHostIDFromScope(cfg.Resource.ID, cfg.Dimensions); got != "resource-host" {
		t.Fatalf("host identity = %q", got)
	}
}

func TestProxmoxPBSListTimeouts(t *testing.T) {
	cfg, err := Resolve(Config{}, Overlay{AllowMissingPulse: true})
	if err != nil || cfg.Proxmox.ListTimeoutSeconds != 60 || cfg.PBS.ListTimeoutSeconds != 60 {
		t.Fatal(cfg.Proxmox, cfg.PBS, err)
	}
	for _, section := range []string{"proxmox", "pbs"} {
		for _, value := range []int{-1, 0, 45} {
			file := filepath.Join(t.TempDir(), "config.toml")
			if err := os.WriteFile(file, []byte(fmt.Sprintf("[%s]\nlist_timeout_seconds = %d\n", section, value)), 0600); err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadFile(file, true)
			if value <= 0 {
				if err == nil {
					t.Fatalf("accepted %s %d", section, value)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := Resolve(loaded, Overlay{AllowMissingPulse: true})
			if err != nil {
				t.Fatal(err)
			}
			if section == "proxmox" && resolved.Proxmox.ListTimeoutSeconds != value || section == "pbs" && resolved.PBS.ListTimeoutSeconds != value {
				t.Fatal(resolved.Proxmox, resolved.PBS)
			}
		}
	}
}
