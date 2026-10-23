package packages

import (
	"context"
	"errors"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCountAptUpdates(t *testing.T) {
	got := countAptUpdates([]byte(`Listing...
openssl/jammy-updates 3.0 amd64 [upgradable from: 2.9]
WARNING: apt does not have a stable CLI interface.
bash/jammy 5.1 amd64 [upgradable from: 5.0]
`))
	if got != 2 {
		t.Fatalf("got=%d", got)
	}
}

func TestCountLineUpdatesSkipsMetadata(t *testing.T) {
	got := countLineUpdates([]byte(`Last metadata expiration check: 0:01:00 ago
kernel.x86_64 1.2 repo
Obsoleting Packages
vim.x86_64 9.0 repo
`))
	if got != 2 {
		t.Fatalf("got=%d", got)
	}
}

func TestAptSecurityAndReboot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "run/reboot-required")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	c := Collector{HostRoot: root, Run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "apt" {
			return []byte("Listing...\nopenssl/stable-security,stable-updates 3.0 amd64\nbash/stable-updates 5.2 amd64\n"), nil
		}
		return nil, exec.ErrNotFound
	}}
	for _, required := range []bool{true, false} {
		if !required {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		b, err := c.Collect(context.Background(), monitoring.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		values := map[string]float64{}
		for _, m := range b.Metrics {
			values[m.Name] = m.Value
		}
		if len(values) != 2 || values["system.packages.updates.total"] != 2 || values["system.packages.security_updates"] != 1 {
			t.Fatal(values)
		}
		found := false
		for _, s := range b.States {
			if s.Key == "system.reboot_required" {
				found = true
				if s.Value != required {
					t.Fatal(s)
				}
			}
		}
		if !found {
			t.Fatal("missing reboot state")
		}
	}
}
func TestDNFExitCodesAndMissingRebootTool(t *testing.T) {
	exitErr := func(code string) error { return exec.Command("sh", "-c", "exit "+code).Run() }
	code100, code1 := exitErr("100"), exitErr("1")
	for _, rebootErr := range []error{nil, code1, exec.ErrNotFound} {
		c := Collector{Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name == "needs-restarting" {
				return nil, rebootErr
			}
			if name != "dnf" {
				return nil, exec.ErrNotFound
			}
			if args[0] != "--cacheonly" {
				t.Fatal("dnf could access network")
			}
			if strings.Contains(strings.Join(args, " "), "updateinfo") {
				return []byte("FEDORA-1 Important/Sec. openssl-1:3.0-1.fc40.x86_64\nFEDORA-2 Critical/Sec. openssl-1:3.1-2.fc40.i686\n"), code100
			}
			return []byte("openssl.x86_64 3.0 updates\nbash.x86_64 5.2 updates\n"), code100
		}}
		b, err := c.Collect(context.Background(), monitoring.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Metrics) != 2 || b.Metrics[0].Value != 2 || b.Metrics[1].Value != 1 {
			t.Fatal(b.Metrics)
		}
		found := false
		for _, s := range b.States {
			if s.Key == "system.reboot_required" {
				found = true
				if s.Value != (rebootErr == code1) {
					t.Fatal(s)
				}
			}
		}
		if found == (rebootErr == exec.ErrNotFound) {
			t.Fatal("missing tool must omit reboot state")
		}
	}
}

func TestRebootMarkerIndependentOfFailedUpdateQuery(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "run/reboot-required")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	c := Collector{HostRoot: root, Run: func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "apt" {
			return nil, errors.New("query failed")
		}
		return nil, exec.ErrNotFound
	}}
	b, err := c.Collect(context.Background(), monitoring.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range b.States {
		if s.Key == "system.reboot_required" && s.Value == true {
			return
		}
	}
	t.Fatal("failed update query hid reboot requirement")
}

func TestDNFSecurityCountsUniquePackageNames(t *testing.T) {
	out := []byte(`Last metadata expiration check: 0:01:00 ago
FEDORA-1 Important/Sec. openssl-libs-1:3.0.1-1.fc40.x86_64
FEDORA-2 Critical/Sec. openssl-libs-1:3.0.2-2.fc40.x86_64
FEDORA-2 Critical/Sec. openssl-libs-1:3.0.2-2.fc40.i686
FEDORA-3 Moderate/Sec. python3-requests-2.32.0-1.fc40.noarch
FEDORA-4 Low/Sec. kernel-6.10.1-1.fc40.x86_64
FEDORA-5 bugfix ignored-1.0-1.fc40.x86_64
FEDORA-6 Important/Sec. invalid
malformed
`)
	if got := countDNFSecurity(out); got != 3 {
		t.Fatalf("unique security packages = %d, want 3", got)
	}
	if got := countDNFSecurity(nil); got != 0 {
		t.Fatalf("empty output: %d", got)
	}
}
