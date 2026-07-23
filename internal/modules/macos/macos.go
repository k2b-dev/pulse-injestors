package macos

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/entity"
	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	EnableHomebrew        bool
	HomebrewTimeout       time.Duration
	EnableSoftwareUpdate  bool
	SoftwareUpdateTimeout time.Duration
	EnableSystemProfiler  bool
	SystemProfilerTimeout time.Duration
}

func (c Collector) Name() string { return "macos" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	if err := collectOS(ctx, b); err != nil {
		reportSubmoduleError(b, "os", err)
	}
	if err := collectSystem(ctx, b); err != nil {
		reportSubmoduleError(b, "system", err)
	}
	if err := collectFilesystems(ctx, b, scope); err != nil {
		reportSubmoduleError(b, "filesystem", err)
	}
	if err := collectBattery(ctx, b); err != nil {
		reportSubmoduleError(b, "battery", err)
	}
	if c.EnableSystemProfiler {
		if err := collectDisplays(ctx, b, scope, c.SystemProfilerTimeout); err != nil {
			reportSubmoduleError(b, "display", err)
		}
	} else {
		b.State("system.display.available", false, nil)
		b.State("system.display.unavailable_reason", "system_profiler_disabled", nil)
		b.State("macos.system_profiler.enabled", false, nil)
	}
	if c.EnableHomebrew {
		if err := collectHomebrew(ctx, b, scope, c.HomebrewTimeout); err != nil {
			reportSubmoduleError(b, "homebrew", err)
		}
	}
	if c.EnableSoftwareUpdate {
		if err := collectSoftwareUpdate(ctx, b, c.SoftwareUpdateTimeout); err != nil {
			reportSubmoduleError(b, "softwareupdate", err)
		}
	}
	return b.Batch(), nil
}

func reportSubmoduleError(b *monitoring.Builder, submodule string, err error) {
	dims := map[string]string{"submodule": submodule}
	b.State("macos.submodule.ok", false, dims)
	b.EventDetails("macos.submodule.failed", dims, monitoring.EventDetails{
		Attributes: map[string]any{"error": err.Error()},
	})
}

func collectOS(ctx context.Context, b *monitoring.Builder) error {
	out, err := run(ctx, 3*time.Second, nil, "sw_vers")
	if err != nil {
		return fmt.Errorf("sw_vers: %w", err)
	}
	values := keyValueLines(out, ":")
	b.State("macos.product_name", values["ProductName"], nil)
	b.State("macos.version", values["ProductVersion"], nil)
	b.State("macos.build", values["BuildVersion"], nil)
	return nil
}

func collectSystem(ctx context.Context, b *monitoring.Builder) error {
	var errs []error
	if load, err := macLoad(ctx); err != nil {
		errs = append(errs, err)
	} else {
		b.Metric("system.load.1m", "gauge", load[0], "load", nil)
		b.Metric("system.load.5m", "gauge", load[1], "load", nil)
		b.Metric("system.load.15m", "gauge", load[2], "load", nil)
	}
	if v, err := sysctlUint(ctx, "hw.memsize"); err != nil {
		errs = append(errs, err)
	} else if mem, err := vmStat(ctx, v); err != nil {
		errs = append(errs, err)
	} else {
		b.Metric("system.memory.total", "gauge", float64(mem.total), "bytes", nil)
		b.Metric("system.memory.available", "gauge", float64(mem.available), "bytes", nil)
		b.Metric("system.memory.used", "gauge", float64(mem.used), "bytes", nil)
		if mem.total > 0 {
			b.Metric("system.memory.usage", "gauge", (float64(mem.used)/float64(mem.total))*100, "percent", nil)
		}
	}
	if v, err := sysctlUint(ctx, "hw.logicalcpu"); err == nil {
		b.Metric("system.cpu.cores.logical", "gauge", float64(v), "", nil)
	}
	if v, err := sysctlUint(ctx, "hw.physicalcpu"); err == nil {
		b.Metric("system.cpu.cores.physical", "gauge", float64(v), "", nil)
	}
	if uptime, err := macUptime(ctx); err != nil {
		errs = append(errs, err)
	} else {
		b.Metric("system.uptime", "gauge", uptime, "seconds", nil)
	}
	return errors.Join(errs...)
}

func collectFilesystems(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope) error {
	out, err := run(ctx, 5*time.Second, nil, "df", "-k", "-P", "-l")
	if err != nil {
		return fmt.Errorf("df: %w", err)
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			first = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 6 {
			continue
		}
		totalKB, _ := strconv.ParseUint(fields[1], 10, 64)
		usedKB, _ := strconv.ParseUint(fields[2], 10, 64)
		availKB, _ := strconv.ParseUint(fields[3], 10, 64)
		usePct, _ := strconv.ParseFloat(strings.TrimSuffix(fields[4], "%"), 64)
		mount := fields[5]
		if strings.HasPrefix(mount, "/System/Volumes/Data/") {
			continue
		}
		dims := map[string]string{"mount": mount}
		fb := monitoring.NewBuilder(filesystemScope(scope, mount))
		fb.State("system.filesystem.source", fields[0], dims)
		fb.Metric("system.filesystem.total", "gauge", float64(totalKB*1024), "bytes", dims)
		fb.Metric("system.filesystem.used", "gauge", float64(usedKB*1024), "bytes", dims)
		fb.Metric("system.filesystem.available", "gauge", float64(availKB*1024), "bytes", dims)
		fb.Metric("system.filesystem.usage", "gauge", usePct, "percent", dims)
		b.Merge(fb.Batch())
	}
	return sc.Err()
}

func collectHomebrew(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	path, err := exec.LookPath("brew")
	if err != nil {
		b.State("package.homebrew.available", false, nil)
		return nil
	}
	b.State("package.homebrew.available", true, nil)
	if out, err := run(ctx, 3*time.Second, nil, path, "--prefix"); err == nil {
		b.State("package.homebrew.prefix", strings.TrimSpace(string(out)), nil)
	}
	if out, err := run(ctx, 3*time.Second, nil, path, "--version"); err == nil {
		version := strings.Split(strings.TrimSpace(string(out)), "\n")[0]
		b.State("package.homebrew.version", version, nil)
	}
	collectHomebrewServices(ctx, b, scope, path, timeout)
	env := []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1"}
	out, err := run(ctx, timeout, env, path, "outdated", "--json=v2")
	if err != nil {
		b.State("package.homebrew.outdated.available", false, nil)
		return fmt.Errorf("brew outdated: %w", err)
	}
	var parsed struct {
		Formulae []json.RawMessage `json:"formulae"`
		Casks    []json.RawMessage `json:"casks"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		b.State("package.homebrew.outdated.available", false, nil)
		return fmt.Errorf("brew outdated json: %w", err)
	}
	b.State("package.homebrew.outdated.available", true, nil)
	b.Metric("system.packages.homebrew.outdated.formulae", "gauge", float64(len(parsed.Formulae)), "", nil)
	b.Metric("system.packages.homebrew.outdated.casks", "gauge", float64(len(parsed.Casks)), "", nil)
	b.Metric("system.packages.homebrew.outdated.total", "gauge", float64(len(parsed.Formulae)+len(parsed.Casks)), "", nil)
	return nil
}

func collectHomebrewServices(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, path string, timeout time.Duration) {
	out, err := run(ctx, timeout, []string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1"}, path, "services", "info", "--all", "--json")
	if err != nil {
		b.State("package.homebrew.services.available", false, nil)
		b.EventDetails("package.homebrew.services.failed", map[string]string{"operation": "services_info"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	services, err := parseHomebrewServices(out)
	if err != nil {
		b.State("package.homebrew.services.available", false, nil)
		b.EventDetails("package.homebrew.services.failed", map[string]string{"operation": "services_parse"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	b.State("package.homebrew.services.available", true, nil)
	b.Metric("system.service.homebrew.services", "gauge", float64(len(services)), "", nil)
	states := map[string]int{}
	for _, service := range services {
		dims := map[string]string{"service": service.Name, "service_manager": "homebrew"}
		sb := monitoring.NewBuilder(homebrewServiceScope(scope, service.Name))
		sb.State("system.service.homebrew.present", true, dims)
		if service.Status != "" {
			sb.State("system.service.homebrew.status", service.Status, dims)
			states[service.Status]++
		}
		if service.User != "" {
			sb.State("system.service.homebrew.user", service.User, dims)
		}
		if service.File != "" {
			sb.State("system.service.homebrew.file", service.File, dims)
		}
		sb.State("system.service.homebrew.running", service.Status == "started", dims)
		b.Merge(sb.Batch())
	}
	for state, count := range states {
		b.Metric("system.service.homebrew.by_status", "gauge", float64(count), "", map[string]string{"status": state})
	}
}

func collectSoftwareUpdate(ctx context.Context, b *monitoring.Builder, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	out, err := run(ctx, timeout, nil, "softwareupdate", "-l")
	if err != nil {
		b.State("macos.softwareupdate.available", false, nil)
		return fmt.Errorf("softwareupdate: %w", err)
	}
	text := string(out)
	count := strings.Count(text, "\n   * ")
	b.State("macos.softwareupdate.available", true, nil)
	b.Metric("system.packages.macos.updates", "gauge", float64(count), "", nil)
	return nil
}

func collectBattery(ctx context.Context, b *monitoring.Builder) error {
	out, err := run(ctx, 3*time.Second, nil, "ioreg", "-r", "-n", "AppleSmartBattery")
	if err != nil {
		b.State("system.battery.available", false, nil)
		return fmt.Errorf("ioreg AppleSmartBattery: %w", err)
	}
	text := string(out)
	nums := ioregNumbers(text)
	bools := ioregBools(text)
	stringsMap := ioregStrings(text)
	b.State("system.battery.available", true, nil)
	stateMappings := map[string]string{
		"BatteryInstalled":      "system.battery.installed",
		"IsCharging":            "system.battery.charging",
		"FullyCharged":          "system.battery.fully_charged",
		"ExternalConnected":     "system.power.external_connected",
		"ExternalChargeCapable": "system.power.external_charge_capable",
		"AtCriticalLevel":       "system.battery.critical_level",
		"built-in":              "system.battery.built_in",
	}
	for source, key := range stateMappings {
		if v, ok := bools[source]; ok {
			b.State(key, v, nil)
		}
	}
	if serial := stringsMap["Serial"]; serial != "" {
		b.State("system.battery.serial", serial, nil)
	}
	if device := stringsMap["DeviceName"]; device != "" {
		b.State("system.battery.device", device, nil)
	}
	metricMappings := map[string]struct {
		name string
		unit string
	}{
		"CurrentCapacity":         {"system.battery.charge", "percent"},
		"AppleRawCurrentCapacity": {"system.battery.current_capacity", "milliampere-hour"},
		"AppleRawMaxCapacity":     {"system.battery.max_capacity", "milliampere-hour"},
		"DesignCapacity":          {"system.battery.design_capacity", "milliampere-hour"},
		"NominalChargeCapacity":   {"system.battery.nominal_charge_capacity", "milliampere-hour"},
		"CycleCount":              {"system.battery.cycle_count", ""},
		"Voltage":                 {"system.battery.voltage", "millivolt"},
		"AppleRawBatteryVoltage":  {"system.battery.raw_voltage", "millivolt"},
		"Amperage":                {"system.battery.amperage", "milliampere"},
		"InstantAmperage":         {"system.battery.instant_amperage", "milliampere"},
		"AvgTimeToEmpty":          {"system.battery.time_to_empty", "minutes"},
		"AvgTimeToFull":           {"system.battery.time_to_full", "minutes"},
		"TimeRemaining":           {"system.battery.time_remaining", "minutes"},
		"DesignCycleCount9C":      {"system.battery.design_cycle_count", ""},
		"Watts":                   {"system.power.adapter.watts", "watt"},
		"AdapterVoltage":          {"system.power.adapter.voltage", "millivolt"},
		"SystemPowerIn":           {"system.power.input", "milliwatt"},
		"BatteryPower":            {"system.battery.power", "milliwatt"},
	}
	for source, target := range metricMappings {
		if v, ok := nums[source]; ok && v != 65535 {
			b.Metric(target.name, "gauge", float64(v), target.unit, nil)
		}
	}
	emitBatteryHealth(b, nums)
	if v, ok := nums["Temperature"]; ok {
		b.Metric("system.battery.temperature", "gauge", float64(v)/100, "celsius", nil)
	}
	if v, ok := nums["VirtualTemperature"]; ok {
		b.Metric("system.battery.virtual_temperature", "gauge", float64(v)/100, "celsius", nil)
	}
	b.State("macos.thermal.cpu.available", false, map[string]string{"source": "powermetrics"})
	b.State("macos.thermal.cpu.unavailable_reason", "privileged_or_tool_required", map[string]string{"source": "powermetrics"})
	b.State("macos.thermal.gpu.available", false, map[string]string{"source": "powermetrics"})
	b.State("macos.thermal.gpu.unavailable_reason", "privileged_or_tool_required", map[string]string{"source": "powermetrics"})
	return nil
}

func emitBatteryHealth(b *monitoring.Builder, nums map[string]int64) {
	maxCapacity, maxOK := nums["AppleRawMaxCapacity"]
	if !maxOK {
		maxCapacity, maxOK = nums["NominalChargeCapacity"]
	}
	designCapacity, designOK := nums["DesignCapacity"]
	if maxOK && designOK && designCapacity > 0 {
		b.Metric("system.battery.health", "gauge", (float64(maxCapacity)/float64(designCapacity))*100, "percent", nil)
	}
	cycles, cyclesOK := nums["CycleCount"]
	designCycles, designCyclesOK := nums["DesignCycleCount9C"]
	if cyclesOK && designCyclesOK && designCycles > 0 {
		b.Metric("system.battery.cycle_usage", "gauge", (float64(cycles)/float64(designCycles))*100, "percent", nil)
	}
}

type HomebrewService struct {
	Name   string
	Status string
	User   string
	File   string
}

func parseHomebrewServices(out []byte) ([]HomebrewService, error) {
	var raw []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		User   string `json:"user"`
		File   string `json:"file"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	services := make([]HomebrewService, 0, len(raw))
	for _, service := range raw {
		if service.Name == "" {
			continue
		}
		services = append(services, HomebrewService{
			Name:   service.Name,
			Status: strings.ToLower(strings.TrimSpace(service.Status)),
			User:   service.User,
			File:   service.File,
		})
	}
	return services, nil
}

func collectDisplays(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, timeout time.Duration) error {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	b.State("macos.system_profiler.enabled", true, nil)
	out, err := run(ctx, timeout, nil, "system_profiler", "SPDisplaysDataType", "-json")
	if err != nil {
		b.State("system.display.available", false, nil)
		return fmt.Errorf("system_profiler SPDisplaysDataType: %w", err)
	}
	var parsed struct {
		Displays []struct {
			Name    string `json:"_name"`
			Metal   string `json:"spdisplays_mtlgpufamilysupport"`
			Vendor  string `json:"spdisplays_vendor"`
			Bus     string `json:"sppci_bus"`
			Cores   string `json:"sppci_cores"`
			Model   string `json:"sppci_model"`
			Devices []struct {
				Name       string `json:"_name"`
				Pixels     string `json:"_spdisplays_pixels"`
				Resolution string `json:"_spdisplays_resolution"`
				Type       string `json:"spdisplays_display_type"`
				Connection string `json:"spdisplays_connection_type"`
				Main       string `json:"spdisplays_main"`
				Mirror     string `json:"spdisplays_mirror"`
				Online     string `json:"spdisplays_online"`
			} `json:"spdisplays_ndrvs"`
		} `json:"SPDisplaysDataType"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return fmt.Errorf("system_profiler display json: %w", err)
	}
	displayCount := 0
	for i, gpu := range parsed.Displays {
		dims := map[string]string{"gpu": firstNonEmpty(gpu.Model, gpu.Name, fmt.Sprintf("gpu-%d", i))}
		b.State("system.gpu.model", firstNonEmpty(gpu.Model, gpu.Name), dims)
		b.State("system.gpu.vendor", gpu.Vendor, dims)
		b.State("system.gpu.metal", gpu.Metal, dims)
		b.State("system.gpu.bus", gpu.Bus, dims)
		if cores, err := strconv.ParseFloat(gpu.Cores, 64); err == nil {
			b.Metric("system.gpu.cores", "gauge", cores, "", dims)
		}
		for _, display := range gpu.Devices {
			displayCount++
			dd := map[string]string{"display": firstNonEmpty(display.Name, fmt.Sprintf("display-%d", displayCount)), "gpu": dims["gpu"]}
			b.State("system.display.online", display.Online == "spdisplays_yes", dd)
			b.State("system.display.main", display.Main == "spdisplays_yes", dd)
			b.State("system.display.mirror", display.Mirror, dd)
			b.State("system.display.type", display.Type, dd)
			b.State("system.display.connection", display.Connection, dd)
			b.State("system.display.resolution", display.Resolution, dd)
			if width, height, ok := parsePixels(display.Pixels); ok {
				b.Metric("system.display.width", "gauge", float64(width), "pixels", dd)
				b.Metric("system.display.height", "gauge", float64(height), "pixels", dd)
			}
			if hz, ok := parseRefreshRate(display.Resolution); ok {
				b.Metric("system.display.refresh_rate", "gauge", hz, "hertz", dd)
			}
		}
	}
	b.State("system.display.available", displayCount > 0, nil)
	b.Metric("system.display.count", "gauge", float64(displayCount), "", nil)
	return nil
}

func filesystemScope(scope monitoring.Scope, mount string) monitoring.Scope {
	scope.EntityType = "filesystem"
	scope.EntityID = entity.ID("filesystem", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), entity.Key(mount, "root"))
	scope.Label = mount
	return scope
}

func homebrewServiceScope(scope monitoring.Scope, service string) monitoring.Scope {
	scope.EntityType = "service"
	scope.EntityID = entity.ID("service", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), "homebrew", service)
	scope.Label = service
	return scope
}

type memory struct {
	total     uint64
	available uint64
	used      uint64
}

func vmStat(ctx context.Context, total uint64) (memory, error) {
	out, err := run(ctx, 3*time.Second, nil, "vm_stat")
	if err != nil {
		return memory{}, fmt.Errorf("vm_stat: %w", err)
	}
	pageSize := uint64(4096)
	var free, inactive, speculative uint64
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "page size of") {
			for _, field := range strings.Fields(line) {
				if v, err := strconv.ParseUint(field, 10, 64); err == nil {
					pageSize = v
					break
				}
			}
			continue
		}
		key, raw, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value, err := strconv.ParseUint(strings.Trim(strings.TrimSpace(raw), "."), 10, 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Pages free":
			free = value
		case "Pages inactive":
			inactive = value
		case "Pages speculative":
			speculative = value
		}
	}
	if err := sc.Err(); err != nil {
		return memory{}, err
	}
	available := (free + inactive + speculative) * pageSize
	if available > total {
		available = total
	}
	return memory{total: total, available: available, used: total - available}, nil
}

func macLoad(ctx context.Context) ([3]float64, error) {
	var out [3]float64
	raw, err := run(ctx, 3*time.Second, nil, "sysctl", "-n", "vm.loadavg")
	if err != nil {
		return out, fmt.Errorf("sysctl vm.loadavg: %w", err)
	}
	fields := strings.Fields(strings.NewReplacer("{", "", "}", "").Replace(string(raw)))
	if len(fields) < 3 {
		return out, fmt.Errorf("unexpected vm.loadavg output %q", strings.TrimSpace(string(raw)))
	}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return out, err
		}
		out[i] = v
	}
	return out, nil
}

func macUptime(ctx context.Context) (float64, error) {
	raw, err := run(ctx, 3*time.Second, nil, "sysctl", "-n", "kern.boottime")
	if err != nil {
		return 0, fmt.Errorf("sysctl kern.boottime: %w", err)
	}
	text := string(raw)
	start := strings.Index(text, "sec = ")
	if start == -1 {
		return 0, fmt.Errorf("unexpected kern.boottime output %q", strings.TrimSpace(text))
	}
	start += len("sec = ")
	end := strings.Index(text[start:], ",")
	if end == -1 {
		return 0, fmt.Errorf("unexpected kern.boottime output %q", strings.TrimSpace(text))
	}
	sec, err := strconv.ParseInt(strings.TrimSpace(text[start:start+end]), 10, 64)
	if err != nil {
		return 0, err
	}
	return time.Since(time.Unix(sec, 0)).Seconds(), nil
}

func sysctlUint(ctx context.Context, name string) (uint64, error) {
	out, err := run(ctx, 3*time.Second, nil, "sysctl", "-n", name)
	if err != nil {
		return 0, fmt.Errorf("sysctl %s: %w", name, err)
	}
	return strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
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

func parsePixels(raw string) (int, int, bool) {
	parts := strings.Split(raw, " x ")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	return width, height, err1 == nil && err2 == nil
}

var refreshPattern = regexp.MustCompile(`@\s*([0-9.]+)Hz`)

func parseRefreshRate(raw string) (float64, bool) {
	match := refreshPattern.FindStringSubmatch(raw)
	if len(match) != 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(match[1], 64)
	return v, err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func run(ctx context.Context, timeout time.Duration, env []string, name string, args ...string) ([]byte, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}
	out, err := cmd.Output()
	if runCtx.Err() != nil {
		return out, runCtx.Err()
	}
	return out, err
}
