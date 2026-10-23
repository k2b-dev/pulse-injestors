package posture

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

func TestEncryptionMountedStorage(t *testing.T) {
	for _, tc := range []struct {
		name, data     string
		enabled, known bool
	}{
		{"plain root", `{"blockdevices":[{"type":"part","mountpoints":["/"]}]}`, false, true},
		{"lvm on luks", `{"blockdevices":[{"type":"disk","children":[{"type":"crypt","children":[{"type":"lvm","mountpoints":["/"]}]}]}]}`, true, true},
		{"encrypted home", `{"blockdevices":[{"type":"part","mountpoints":["/"]},{"type":"crypt","mountpoints":["/home"]}]}`, true, true},
		{"unrelated crypt", `{"blockdevices":[{"type":"part","mountpoints":["/"]},{"type":"crypt","mountpoints":["/mnt/backup",null]}]}`, false, true},
		{"no root visible", `{"blockdevices":[{"type":"disk","mountpoints":[null]}]}`, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enabled, known, err := parseEncryption([]byte(tc.data))
			if err != nil || enabled != tc.enabled || known != tc.known {
				t.Fatal(enabled, known, err)
			}
		})
	}
	if _, _, err := parseEncryption([]byte(`{"blockdevices":[{}]}`)); err == nil {
		t.Fatal("accepted incomplete lsblk")
	}
}
func TestFirewallDoesNotHidePermissionFailure(t *testing.T) {
	for _, tc := range []struct {
		nft, ufw       string
		err            error
		enabled, known bool
	}{
		{`{"nftables":[{"metainfo":{}}]}`, "Status: inactive", nil, false, true},
		{`{"nftables":[{"table":{"name":"filter"}}]}`, "Status: inactive", nil, true, true},
		{"", "Status: inactive", os.ErrPermission, false, false},
		{"", "Status: active", os.ErrPermission, true, true},
		{"", "", exec.ErrNotFound, false, false},
	} {
		c := Collector{Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("missing command deadline")
			}
			switch name {
			case "nft":
				return []byte(tc.nft), tc.err
			case "ufw":
				if tc.ufw != "" {
					return []byte(tc.ufw), nil
				}
			}
			return nil, exec.ErrNotFound
		}}
		enabled, known, _ := c.firewall(context.Background())
		if enabled != tc.enabled || known != tc.known {
			t.Fatal(tc, enabled, known)
		}
	}
}
func TestAutoUpdatesDebianAndFedora(t *testing.T) {
	for _, tc := range []struct {
		distro, apt, timer string
		want, known        bool
	}{
		{"debian", `APT::Periodic::Unattended-Upgrade "1";`, "enabled", true, true},
		{"debian", `APT::Periodic::Unattended-Upgrade "0";`, "enabled", false, true},
		{"debian", `APT::Periodic::Unattended-Upgrade "1";`, "disabled", false, true},
		{"debian", `APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::Enable "0";`, "enabled", false, true},
		{"fedora", "", "enabled", true, true}, {"fedora", "", "disabled", false, true}, {"alpine", "", "enabled", false, false},
	} {
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "etc"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "etc/os-release"), []byte("ID="+tc.distro), 0600); err != nil {
			t.Fatal(err)
		}
		c := Collector{HostRoot: root, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if name == "apt-config" {
				return []byte(tc.apt), nil
			}
			if name == "systemctl" {
				return []byte("LoadState=loaded\nUnitFileState=" + tc.timer), nil
			}
			return nil, exec.ErrNotFound
		}}
		enabled, known, err := c.autoupdate(context.Background())
		if err != nil || enabled != tc.want || known != tc.known {
			t.Fatal(tc, enabled, known, err)
		}
	}
}
func TestUnknownStatesAndRecovery(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "etc"), 0700)
	os.WriteFile(filepath.Join(root, "etc/os-release"), []byte("ID=alpine"), 0600)
	c := Collector{HostRoot: root, Run: func(context.Context, string, ...string) ([]byte, error) { return nil, os.ErrPermission }}
	scope := monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "desktop", Label: "Desktop"}, Timestamp: time.Unix(1700000000, 0).UTC()}
	b, err := c.Collect(context.Background(), scope)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatal(err)
	}
	for _, s := range b.States {
		if strings.HasSuffix(s.Key, ".enabled") && s.Value != "unknown" {
			t.Fatal(s)
		}
	}
	if err := validation.Batch(b); err != nil {
		t.Fatal(err)
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, exec.ErrNotFound }
	if _, err := c.Collect(context.Background(), scope); err != nil {
		t.Fatal("missing optional tool should not fail", err)
	}
}
