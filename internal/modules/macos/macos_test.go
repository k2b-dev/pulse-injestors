package macos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

func scope() monitoring.Scope {
	return monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "mac", Label: "Mac"}, Timestamp: time.Unix(1700000000, 0).UTC()}
}
func fixture(t *testing.T, outputs map[string]string) localexec.Runner {
	t.Helper()
	return func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("unbounded command")
		}
		key := strings.TrimSpace(name + " " + strings.Join(args, " "))
		out, ok := outputs[key]
		if !ok {
			return nil, exec.ErrNotFound
		}
		return []byte(out), nil
	}
}
func state(b pulse.Batch, key string) any {
	for _, s := range b.States {
		if s.Key == key {
			return s.Value
		}
	}
	return nil
}
func metric(b pulse.Batch, key string) float64 {
	for _, m := range b.Metrics {
		if m.Name == key {
			return m.Value
		}
	}
	return -1
}
func TestHealthFixtures(t *testing.T) {
	for _, size := range []uint64{4096, 16384} {
		c := Collector{Group: "health", Run: fixture(t, map[string]string{
			"sysctl -n vm.loadavg": "{ 1.0 2.0 3.0 }", "sysctl -n hw.memsize": fmt.Sprint(size * 100),
			"vm_stat":            fmt.Sprintf("Mach Virtual Memory Statistics: (page size of %d bytes)\nPages free: 10.\nPages inactive: 20.\nPages speculative: 5.\n", size),
			"top -l 2 -n 0 -s 1": "CPU usage: 3.0% user, 2.0% sys, 95.0% idle\nCPU usage: 10.0% user, 15.0% sys, 75.0% idle\n",
		})}
		b, err := c.Collect(context.Background(), scope())
		if err != nil {
			t.Fatal(err)
		}
		if metric(b, "system.cpu.usage") != 25 || metric(b, "system.memory.used") != float64(size*65) || metric(b, "system.memory.total") != -1 {
			t.Fatal(b)
		}
		if err := validation.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
	for _, out := range []string{"", "CPU usage: 1% user, 1% sys, 98% idle", "CPU usage: 1% user, 1% sys, 198% idle\nCPU usage: 1% user, 1% sys, 198% idle"} {
		if _, err := parseCPU([]byte(out)); err == nil {
			t.Fatal("accepted bad CPU sample")
		}
	}
	if _, err := parseMemory([]byte("page size of 16384 bytes\nPages free: 1."), 100000); err == nil {
		t.Fatal("accepted partial memory sample")
	}
}
func TestOnlySystemAndDataFilesystems(t *testing.T) {
	c := Collector{Group: "filesystem", Run: fixture(t, map[string]string{"df -k -P -l": "Filesystem 1024-blocks Used Available Capacity Mounted on\n/dev/disk3s1s1 1000 100 700 13% /\n/dev/disk3s5 1000 200 700 23% /System/Volumes/Data\n/dev/disk3s2 1000 10 700 2% /System/Volumes/Preboot\n/dev/disk4 1000 20 700 3% /Volumes/External\n"})}
	b, err := c.Collect(context.Background(), scope())
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Metrics) != 8 || metric(b, "system.filesystem.usage") != 10 {
		t.Fatal(b)
	}
	for _, m := range b.Metrics {
		if m.Resource.Type != "filesystem" {
			t.Fatal(m)
		}
	}
	if err := validation.Batch(b); err != nil {
		t.Fatal(err)
	}
}
func TestBatteryLaptopAndDesktop(t *testing.T) {
	for _, out := range []string{"", `"BatteryInstalled" = Yes
"AppleRawMaxCapacity" = 4000
"DesignCapacity" = 5000
"CycleCount" = 250
"PermanentFailureStatus" = 0
"Voltage" = 12000
"IsCharging" = Yes`} {
		c := Collector{Group: "battery", Run: fixture(t, map[string]string{"ioreg -r -n AppleSmartBattery": out})}
		b, err := c.Collect(context.Background(), scope())
		if err != nil {
			t.Fatal(err)
		}
		if out == "" {
			if len(b.Metrics) != 0 || state(b, "system.battery.available") != false {
				t.Fatal(b)
			}
			continue
		}
		if len(b.Metrics) != 2 || metric(b, "system.battery.max_capacity") != 80 || state(b, "system.battery.health") != "Normal" {
			t.Fatal(b)
		}
	}
}
func TestPostureUnknownAndRecovery(t *testing.T) {
	outputs := map[string]string{
		"fdesetup status": "FileVault is On.",
		"/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate": "Firewall is disabled. (State = 0)",
		"csrutil status": "System Integrity Protection status: enabled.", "spctl --status": "assessments enabled",
		"defaults read /Library/Preferences/com.apple.SoftwareUpdate AutomaticCheckEnabled":            "1",
		"defaults read /Library/Preferences/com.apple.SoftwareUpdate AutomaticallyInstallMacOSUpdates": "0",
	}
	c := Collector{Group: "posture", Run: fixture(t, outputs)}
	b, err := c.Collect(context.Background(), scope())
	if err != nil {
		t.Fatal(err)
	}
	if state(b, "macos.filevault.enabled") != true || state(b, "macos.firewall.enabled") != false || state(b, "macos.autoupdate.enabled") != false {
		t.Fatal(b)
	}
	outputs["fdesetup status"] = "FileVault status cannot be determined"
	b, err = c.Collect(context.Background(), scope())
	if err == nil || state(b, "macos.filevault.enabled") != "unknown" {
		t.Fatal(b, err)
	}
	c.Run = func(context.Context, string, ...string) ([]byte, error) { return nil, context.DeadlineExceeded }
	b, err = c.Collect(context.Background(), scope())
	if !errors.Is(err, context.DeadlineExceeded) || state(b, "macos.sip.enabled") != "unknown" {
		t.Fatal(b, err)
	}
}
func TestUpdates(t *testing.T) {
	for _, out := range []string{"Software Update Tool\nNo new software available.", "Software Update Tool\n* Label: macOS 15\n Title: macOS 15\n   * Safari-18\n"} {
		c := Collector{Group: "updates", EnableSoftwareUpdate: true, EnableHomebrew: true, Run: fixture(t, map[string]string{"softwareupdate -l": out, "brew outdated --json=v2": `{"formulae":[{}],"casks":[]}`})}
		b, err := c.Collect(context.Background(), scope())
		if err != nil {
			t.Fatal(err)
		}
		want := 0.
		if strings.Contains(out, "Label:") {
			want = 2
		}
		if metric(b, "system.packages.updates.total") != want || metric(b, "system.packages.homebrew.outdated.total") != 1 {
			t.Fatal(b)
		}
	}
	c := Collector{Group: "updates", EnableHomebrew: true, Run: fixture(t, nil)}
	b, err := c.Collect(context.Background(), scope())
	if err != nil || state(b, "package.homebrew.available") != false {
		t.Fatal(b, err)
	}
}
func TestInventoryCapsAndVersions(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 1001; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("%04d.app", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	calls := 0
	c := Collector{Group: "software", ApplicationsDir: root, EnableHomebrew: true, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing deadline")
		}
		if name == "brew" {
			items := []string{}
			for i := 0; i < 2001; i++ {
				items = append(items, fmt.Sprintf(`{"name":"f%d","installed":[{"version":"1"}]}`, i))
			}
			return []byte(`{"formulae":[` + strings.Join(items, ",") + `],"casks":[]}`), nil
		}
		calls++
		if args[1] == "CFBundleIdentifier" {
			return []byte("org.example." + filepath.Base(filepath.Dir(filepath.Dir(args[len(args)-1])))), nil
		}
		return []byte("2.0"), nil
	}}
	b, err := c.Collect(context.Background(), scope())
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, s := range b.States {
		if s.Key == "software.package.version" {
			counts[s.Dimensions["arch"]]++
		}
	}
	if counts["app"] != 1000 || counts["brew"] != 2000 || calls != 2000 {
		t.Fatal(counts, calls)
	}
	if err := validation.Batch(b); err != nil {
		t.Fatal(err)
	}
	versions, err := parseBrewInventory([]byte(`{"formulae":[{"name":"foo","installed":[{"version":"2"},{"version":"1"}]}],"casks":[{"token":"foo","installed":"3"},{"token":"bar","installed":["4"]}]}`))
	if err != nil || versions["formula:foo"] != "1, 2" || versions["cask:foo"] != "3" || versions["cask:bar"] != "4" {
		t.Fatal(versions, err)
	}
}
func TestSmartStatus(t *testing.T) {
	for _, status := range []string{"Verified", "Failing", "Not Supported"} {
		c := Collector{Group: "disk", Run: fixture(t, map[string]string{"diskutil list internal physical": "/dev/disk0 (internal, physical):\n", "diskutil info /dev/disk0": "SMART Status: " + status})}
		b, err := c.Collect(context.Background(), scope())
		if err != nil {
			t.Fatal(err)
		}
		if status == "Not Supported" {
			if state(b, "system.disk.smart.health_available") != false {
				t.Fatal(b)
			}
		} else if state(b, "system.disk.smart.healthy") != (status == "Verified") {
			t.Fatal(b)
		}
		if err := validation.Batch(b); err != nil {
			t.Fatal(err)
		}
	}
}

func TestHardwareInventory(t *testing.T) {
	c := Collector{Group: "hardware", Run: fixture(t, map[string]string{
		"sw_vers":            "ProductName: macOS\nProductVersion: 15.5\nBuildVersion: 24F74",
		"sysctl -n hw.model": "Mac14,7", "sysctl -n machdep.cpu.brand_string": "Apple M2", "sysctl -n hw.memsize": "17179869184",
	})}
	b, err := c.Collect(context.Background(), scope())
	if err != nil {
		t.Fatal(err)
	}
	if state(b, "system.os.name") != "macOS" || state(b, "system.os.build") != "24F74" || state(b, "system.hardware.cpu_model") != "Apple M2" || metric(b, "system.memory.total") != 17179869184 {
		t.Fatal(b)
	}
	if err := validation.Batch(b); err != nil {
		t.Fatal(err)
	}
}

func TestLinkedAppsAndVersionFallback(t *testing.T) {
	root := t.TempDir()
	bundle := filepath.Join(t.TempDir(), "Example.app")
	if err := os.Mkdir(bundle, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bundle, filepath.Join(root, "Example.app")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "Zed.app"), 0700); err != nil {
		t.Fatal(err)
	}
	c := Collector{Group: "software", ApplicationsDir: root, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
		switch args[1] {
		case "CFBundleIdentifier":
			return []byte(filepath.Base(filepath.Dir(filepath.Dir(args[len(args)-1])))), nil
		case "CFBundleVersion":
			return []byte("42"), nil
		}
		return nil, errors.New("key missing")
	}}
	b, err := c.Collect(context.Background(), scope())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, s := range b.States {
		if s.Key == "software.package.version" {
			count++
			if s.Value != "42" {
				t.Fatal(s)
			}
		}
	}
	if count != 2 {
		t.Fatal(b)
	}
}
