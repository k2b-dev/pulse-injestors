package macos

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

// Collector groups keep expensive workstation probes off the health cadence.
type Collector struct {
	Group                 string
	EnableHomebrew        bool
	HomebrewTimeout       time.Duration
	EnableSoftwareUpdate  bool
	SoftwareUpdateTimeout time.Duration
	Run                   localexec.Runner
	ApplicationsDir       string
}

func (c Collector) Name() string { return "macos-" + c.Group }
func (c Collector) Cadence() time.Duration {
	switch c.Group {
	case "health", "filesystem":
		return 300 * time.Second
	case "updates", "posture":
		return time.Hour
	default:
		return 24 * time.Hour
	}
}
func (c Collector) CollectTimeout() time.Duration {
	switch c.Group {
	case "updates":
		return c.brewTimeout() + c.updateTimeout() + 5*time.Second
	case "software":
		return 120 * time.Second
	default:
		return 60 * time.Second
	}
}
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	var err error
	switch c.Group {
	case "health":
		err = c.health(ctx, b)
	case "filesystem":
		err = c.filesystems(ctx, b, scope)
	case "hardware":
		err = c.hardware(ctx, b)
	case "battery":
		err = c.battery(ctx, b)
	case "disk":
		err = c.disk(ctx, b, scope)
	case "updates":
		err = c.updates(ctx, b)
	case "posture":
		err = c.posture(ctx, b)
	case "software":
		err = c.software(ctx, b)
	default:
		return b.Batch(), fmt.Errorf("unknown macOS group %q", c.Group)
	}
	b.State("macos.submodule.ok", err == nil, map[string]string{"submodule": c.Group})
	return b.Batch(), err
}
func (c Collector) command(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if c.Run != nil {
		return c.Run(ctx, name, args...)
	}
	if name == "softwareupdate" {
		return localexec.CombinedOutput(ctx, 8<<20, name, args...)
	}
	return localexec.Output(ctx, 8<<20, name, args...)
}
func (c Collector) probe(ctx context.Context, name string, args ...string) ([]byte, error) {
	return c.command(ctx, 5*time.Second, name, args...)
}
func optional(err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	return err
}
func (c Collector) hardware(ctx context.Context, b *monitoring.Builder) error {
	var errs []error
	out, err := c.probe(ctx, "sw_vers")
	if err == nil {
		v := keyValueLines(out, ":")
		for source, key := range map[string]string{"ProductName": "system.os.name", "ProductVersion": "system.os.version", "BuildVersion": "system.os.build"} {
			if v[source] == "" {
				errs = append(errs, errors.New("incomplete sw_vers output"))
			} else {
				b.State(key, v[source], nil)
			}
		}
	} else {
		errs = append(errs, err)
	}
	for name, key := range map[string]string{"hw.model": "system.hardware.model", "machdep.cpu.brand_string": "system.hardware.cpu_model"} {
		out, err := c.probe(ctx, "sysctl", "-n", name)
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			b.State(key, strings.TrimSpace(string(out)), nil)
		} else {
			errs = append(errs, fmt.Errorf("hardware %s unavailable", name))
		}
	}
	total, err := c.sysctlUint(ctx, "hw.memsize")
	if err != nil {
		errs = append(errs, err)
	} else {
		b.Metric("system.memory.total", "gauge", float64(total), "bytes", nil)
	}
	return errors.Join(errs...)
}
func (c Collector) sysctlUint(ctx context.Context, name string) (uint64, error) {
	out, err := c.probe(ctx, "sysctl", "-n", name)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil || v == 0 {
		return 0, errors.New("invalid sysctl memory size")
	}
	return v, nil
}
func (c Collector) posture(ctx context.Context, b *monitoring.Builder) error {
	var errs []error
	for _, p := range []struct {
		key, name string
		args      []string
		yes, no   string
	}{
		{"filevault", "fdesetup", []string{"status"}, "FileVault is On.", "FileVault is Off."},
		{"firewall", "/usr/libexec/ApplicationFirewall/socketfilterfw", []string{"--getglobalstate"}, "Firewall is enabled. (State = 1)", "Firewall is disabled. (State = 0)"},
		{"sip", "csrutil", []string{"status"}, "System Integrity Protection status: enabled.", "System Integrity Protection status: disabled."},
		{"gatekeeper", "spctl", []string{"--status"}, "assessments enabled", "assessments disabled"},
	} {
		out, err := c.probe(ctx, p.name, p.args...)
		value := any("unknown")
		if err == nil {
			switch strings.TrimSpace(string(out)) {
			case p.yes:
				value = true
			case p.no:
				value = false
			default:
				err = errors.New("unrecognized posture response")
			}
		}
		b.State("macos."+p.key+".enabled", value, nil)
		b.State("macos.posture.available", value != "unknown", map[string]string{"check": p.key})
		if err = optional(err); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p.key, err))
		}
	}
	enabled, known := true, true
	for _, key := range []string{"AutomaticCheckEnabled", "AutomaticallyInstallMacOSUpdates"} {
		out, err := c.probe(ctx, "defaults", "read", "/Library/Preferences/com.apple.SoftwareUpdate", key)
		v := strings.TrimSpace(string(out))
		if err != nil || (v != "0" && v != "1") {
			known = false
			if err == nil {
				err = errors.New("invalid update preference")
			}
			if optional(err) != nil {
				errs = append(errs, fmt.Errorf("automatic updates: %w", err))
			}
		} else {
			enabled = enabled && v == "1"
		}
	}
	if known {
		b.State("macos.autoupdate.enabled", enabled, nil)
	} else {
		b.State("macos.autoupdate.enabled", "unknown", nil)
	}
	b.State("macos.posture.available", known, map[string]string{"check": "autoupdate"})
	return errors.Join(errs...)
}
func (c Collector) battery(ctx context.Context, b *monitoring.Builder) error {
	out, err := c.probe(ctx, "ioreg", "-r", "-n", "AppleSmartBattery")
	if err != nil {
		b.State("system.battery.available", false, nil)
		return optional(err)
	}
	nums := ioregNumbers(string(out))
	bools := ioregBools(string(out))
	installed, present := bools["BatteryInstalled"]
	if !present && len(bytes.TrimSpace(out)) > 0 {
		b.State("system.battery.available", false, nil)
		return errors.New("unrecognized battery response")
	}
	b.State("system.battery.available", installed, nil)
	if !installed {
		return nil
	}
	health := "unknown"
	if condition := ioregStrings(string(out))["BatteryHealth"]; condition != "" {
		health = condition
	} else if failed, ok := bools["PermanentFailureStatus"]; ok && failed {
		health = "Service Recommended"
	} else if status, ok := nums["PermanentFailureStatus"]; ok {
		if status == 0 {
			health = "Normal"
		} else {
			health = "Service Recommended"
		}
	}
	b.State("system.battery.health", health, nil)
	if n, ok := nums["CycleCount"]; ok && n >= 0 {
		b.Metric("system.battery.cycle_count", "gauge", float64(n), "", nil)
	}
	capacity, ok := nums["AppleRawMaxCapacity"]
	if !ok {
		capacity, ok = nums["NominalChargeCapacity"]
	}
	if ok && capacity > 0 && nums["DesignCapacity"] > 0 {
		b.Metric("system.battery.max_capacity", "gauge", float64(capacity)/float64(nums["DesignCapacity"])*100, "percent", nil)
	}
	return nil
}

func keyValueLines(out []byte, sep string) map[string]string {
	values := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), sep)
		if ok {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return values
}

var (
	ioregNumberPattern = regexp.MustCompile(`"([^"]+)" = (-?[0-9]+)`)
	ioregBoolPattern   = regexp.MustCompile(`"([^"]+)" = (Yes|No)`)
	ioregStringPattern = regexp.MustCompile(`"([^"]+)" = "([^"]*)"`)
)

func ioregNumbers(text string) map[string]int64 {
	out := map[string]int64{}
	for _, match := range ioregNumberPattern.FindAllStringSubmatch(text, -1) {
		if v, err := strconv.ParseInt(match[2], 10, 64); err == nil {
			out[match[1]] = v
		}
	}
	return out
}

func ioregBools(text string) map[string]bool {
	out := map[string]bool{}
	for _, match := range ioregBoolPattern.FindAllStringSubmatch(text, -1) {
		out[match[1]] = match[2] == "Yes"
	}
	return out
}

func ioregStrings(text string) map[string]string {
	out := map[string]string{}
	for _, match := range ioregStringPattern.FindAllStringSubmatch(text, -1) {
		out[match[1]] = match[2]
	}
	return out
}
