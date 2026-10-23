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
		Config:     cfgPath,
		ResourceID: "host:office:probe",
		Local:      true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "host:office:probe" || cfg.Resource.Type != "uptime-probe" {
		t.Fatalf("resource = %#v", cfg.Resource)
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
	cfg, err := loadConfig(cli{ResourceID: "office", Local: true})
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
	cfg, err := loadConfig(cli{ResourceID: "office", Local: true, DisableDefaults: true})
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
	cfg, err := loadConfig(cli{Config: cfgPath, ResourceID: "office", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := configuredTargets(cfg); err == nil {
		t.Fatal("expected built-in target conflict")
	}
}

func TestConfiguredScriptChecksAndCadences(t *testing.T) {
	cfg, err := loadConfig(cli{Config: "../../configs/pulse-uptime.example.toml", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	targets, err := configuredTargets(cfg)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, target := range targets {
		found[target.Kind] = true
		switch target.Kind {
		case "ports":
			if target.Timeout.Seconds() != 900 || target.Cadence.Seconds() != 86400 || len(target.Expected) != 2 {
				t.Fatalf("ports=%+v", target)
			}
		case "dns_records":
			if len(target.Records) == 0 || target.Name == "" || target.Resolver == "" {
				t.Fatalf("records=%+v", target)
			}
		case "tls":
			if target.WarnDays != 14 {
				t.Fatal(target)
			}
		case "dnsbl":
			if len(target.Lists) != 4 {
				t.Fatal(target)
			}
		}
	}
	for _, kind := range []string{"icmp", "dns", "tcp", "http", "ports", "dns_records", "tls", "dnsbl"} {
		if !found[kind] {
			t.Fatal(kind)
		}
	}
	for _, seconds := range []int{0, 17} {
		for i := range cfg.Uptime.Targets {
			if cfg.Uptime.Targets[i].Kind == "ports" {
				cfg.Uptime.Targets[i].TimeoutSeconds = seconds
			}
		}
		targets, err := configuredTargets(cfg)
		if err != nil {
			t.Fatal(err)
		}
		want := seconds
		if want == 0 {
			want = 900
		}
		for _, target := range targets {
			if target.Kind == "ports" && target.Timeout.Seconds() != float64(want) {
				t.Fatalf("ports budget=%v want %d seconds", target.Timeout, want)
			}
		}
	}
}
