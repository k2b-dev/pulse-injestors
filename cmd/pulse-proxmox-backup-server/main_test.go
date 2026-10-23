package main

import (
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigPreservesExplicitResourceID(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(cfgPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(cli{Config: cfgPath, ResourceID: "host:pbs-01", Local: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Resource.ID != "host:pbs-01" {
		t.Fatalf("resource id = %q", cfg.Resource.ID)
	}
	if cfg.Resource.Type != "host" {
		t.Fatalf("resource type = %q", cfg.Resource.Type)
	}
}

func TestSpecializedCollectors(t *testing.T) {
	cfg, err := loadConfig(cli{Local: true})
	if err != nil {
		t.Fatal(err)
	}
	got := collectors(cfg)
	if len(got) != 4 {
		t.Fatalf("unexpected collectors: %v", got)
	}
	for i, want := range []time.Duration{300 * time.Second, 900 * time.Second, 900 * time.Second, 24 * time.Hour} {
		if got[i].(monitoring.Cadenced).Cadence() != want || !strings.HasPrefix(got[i].Name(), "pbs.") {
			t.Fatal(got[i])
		}
	}
}

func TestPBSBudgetsAndStaleConfig(t *testing.T) {
	for _, body := range []string{"", "[pbs]\ngroups_timeout_seconds = 450\nlist_timeout_seconds = 90\nstale_after_hours = 72\n"} {
		path := filepath.Join(t.TempDir(), "pbs.toml")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		cfg, err := loadConfig(cli{Config: path, Local: true})
		if err != nil {
			t.Fatal(err)
		}
		groups, list, stale := 300, 120, 168
		if body != "" {
			groups, list, stale = 450, 90, 72
		}
		if cfg.PBS.GroupsTimeoutSeconds != groups || cfg.PBS.ListTimeoutSeconds != list || cfg.PBS.StaleAfterHours != stale {
			t.Fatal(cfg.PBS)
		}
		for _, c := range collectors(cfg) {
			want := time.Duration(0)
			if c.Name() == "pbs.groups" {
				want = time.Duration(groups) * time.Second
			}
			if got := c.(monitoring.Timeouter).CollectTimeout(); got != want {
				t.Fatalf("%s timeout = %s, want %s", c.Name(), got, want)
			}
		}
	}
	for _, key := range []string{"groups_timeout_seconds", "list_timeout_seconds", "stale_after_hours"} {
		for _, value := range []string{"0", "-1"} {
			path := filepath.Join(t.TempDir(), "pbs.toml")
			if err := os.WriteFile(path, []byte("[pbs]\n"+key+" = "+value+"\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := loadConfig(cli{Config: path, Local: true})
			if err == nil || !strings.Contains(err.Error(), key+" must be positive") {
				t.Fatal(key, value, err)
			}
		}
	}
}
