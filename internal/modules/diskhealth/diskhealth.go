package diskhealth

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	Timeout time.Duration
}

func (c Collector) Name() string { return "diskhealth" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	b := monitoring.NewBuilder(scope)
	collectSmart(ctx, b, timeout)
	collectNVMe(ctx, b, timeout)
	return b.Batch(), nil
}

func collectSmart(ctx context.Context, b *monitoring.Builder, timeout time.Duration) {
	path, err := exec.LookPath("smartctl")
	if err != nil {
		b.State("system.disk.smart.available", false, nil)
		return
	}
	b.State("system.disk.smart.available", true, nil)
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(scanCtx, path, "--scan-open").Output()
	cancel()
	if err != nil {
		b.Event("system.disk.smart.scan.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	devices := parseSmartScan(out)
	b.Metric("system.disk.smart.devices", "gauge", float64(len(devices)), "count", nil)
	for _, device := range devices {
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		health, err := exec.CommandContext(runCtx, path, "-H", device).Output()
		cancel()
		dims := map[string]string{"device": device}
		if err != nil {
			b.State("system.disk.smart.health_available", false, dims)
			b.Event("system.disk.smart.health.failed", dims, map[string]any{"error": err.Error()})
			continue
		}
		status, healthy := parseSmartHealth(health)
		b.State("system.disk.smart.health_available", true, dims)
		if status != "" {
			b.State("system.disk.smart.status", status, dims)
		}
		b.State("system.disk.smart.healthy", healthy, dims)
	}
}

func collectNVMe(ctx context.Context, b *monitoring.Builder, timeout time.Duration) {
	path, err := exec.LookPath("nvme")
	if err != nil {
		b.State("system.disk.nvme.available", false, nil)
		return
	}
	b.State("system.disk.nvme.available", true, nil)
	listCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(listCtx, path, "list", "-o", "json").Output()
	cancel()
	if err != nil {
		b.Event("system.disk.nvme.list.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	devices := parseNVMeList(out)
	b.Metric("system.disk.nvme.devices", "gauge", float64(len(devices)), "count", nil)
	for _, device := range devices {
		dims := map[string]string{"device": device.Path}
		if device.Model != "" {
			dims["model"] = device.Model
		}
		if device.Serial != "" {
			dims["serial"] = device.Serial
		}
		b.State("system.disk.nvme.present", true, dims)
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := exec.CommandContext(runCtx, path, "smart-log", "-o", "json", device.Path).Output()
		cancel()
		if err != nil {
			b.State("system.disk.nvme.smart_available", false, dims)
			b.Event("system.disk.nvme.smart.failed", dims, map[string]any{"error": err.Error()})
			continue
		}
		emitNVMeSmart(b, dims, out)
	}
}

func parseSmartScan(out []byte) []string {
	var devices []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	seen := map[string]bool{}
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "/dev/") || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		devices = append(devices, fields[0])
	}
	return devices
}

func parseSmartHealth(out []byte) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		lower := strings.ToLower(line)
		if strings.Contains(lower, "overall-health") || strings.Contains(lower, "smart health status") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				return line, false
			}
			status := strings.TrimSpace(parts[1])
			normalized := strings.ToLower(status)
			return status, normalized == "passed" || normalized == "ok"
		}
	}
	return "", false
}

type nvmeDevice struct {
	Path   string
	Model  string
	Serial string
}

func parseNVMeList(out []byte) []nvmeDevice {
	var parsed struct {
		Devices []struct {
			DevicePath   string `json:"DevicePath"`
			ModelNumber  string `json:"ModelNumber"`
			SerialNumber string `json:"SerialNumber"`
		} `json:"Devices"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return nil
	}
	devices := make([]nvmeDevice, 0, len(parsed.Devices))
	for _, device := range parsed.Devices {
		if device.DevicePath == "" {
			continue
		}
		devices = append(devices, nvmeDevice{Path: device.DevicePath, Model: device.ModelNumber, Serial: device.SerialNumber})
	}
	return devices
}

func emitNVMeSmart(b *monitoring.Builder, dims map[string]string, out []byte) {
	var raw map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		b.State("system.disk.nvme.smart_available", false, dims)
		b.Event("system.disk.nvme.smart.failed", dims, map[string]any{"error": err.Error()})
		return
	}
	b.State("system.disk.nvme.smart_available", true, dims)
	if v, ok := jsonNumber(raw["critical_warning"]); ok {
		b.Metric("system.disk.nvme.critical_warning", "gauge", v, "count", dims)
		b.State("system.disk.nvme.healthy", v == 0, dims)
	}
	emitNVMeTemperature(b, raw, dims)
	emitNumber(b, raw, "percentage_used", "system.disk.nvme.percentage_used", "percent", dims)
	emitNumber(b, raw, "available_spare", "system.disk.nvme.available_spare", "percent", dims)
	emitNumber(b, raw, "media_errors", "system.disk.nvme.media_errors", "count", dims)
	emitNumber(b, raw, "num_err_log_entries", "system.disk.nvme.error_log_entries", "count", dims)
}

func emitNVMeTemperature(b *monitoring.Builder, raw map[string]any, dims map[string]string) {
	v, ok := jsonNumber(raw["temperature"])
	if !ok {
		return
	}
	b.Metric("system.disk.nvme.temperature", "gauge", v, "kelvin", dims)
	b.Metric("system.disk.nvme.temperature.celsius", "gauge", v-273.15, "celsius", dims)
}

func emitNumber(b *monitoring.Builder, raw map[string]any, source, name, unit string, dims map[string]string) {
	if v, ok := jsonNumber(raw[source]); ok {
		b.Metric(name, "gauge", v, unit, dims)
	}
}

func jsonNumber(v any) (float64, bool) {
	switch value := v.(type) {
	case float64:
		return value, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}
