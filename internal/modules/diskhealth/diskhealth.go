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

	"github.com/valentinkolb/pulse-injestors/internal/entity"
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
	collectSmart(ctx, b, scope, timeout)
	collectNVMe(ctx, b, scope, timeout)
	return b.Batch(), nil
}

func collectSmart(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, timeout time.Duration) {
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
		b.EventDetails("system.disk.smart.scan.failed", map[string]string{"operation": "smart_scan"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	devices := parseSmartScan(out)
	b.Metric("system.disk.smart.devices", "gauge", float64(len(devices)), "", nil)
	for _, device := range devices {
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		health, err := exec.CommandContext(runCtx, path, "-H", device).Output()
		cancel()
		dims := map[string]string{"device": device}
		db := monitoring.NewBuilder(diskScope(scope, device, "", ""))
		if err != nil {
			db.State("system.disk.smart.health_available", false, dims)
			db.EventDetails("system.disk.smart.health.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "smart_health"}), monitoring.EventDetails{
				Attributes: map[string]any{"error": err.Error()},
			})
			b.Merge(db.Batch())
			continue
		}
		status, healthy := parseSmartHealth(health)
		db.State("system.disk.smart.health_available", true, dims)
		if status != "" {
			db.State("system.disk.smart.status", status, dims)
		}
		db.State("system.disk.smart.healthy", healthy, dims)
		attrCtx, cancel := context.WithTimeout(ctx, timeout)
		attributes, err := exec.CommandContext(attrCtx, path, "-A", device).Output()
		cancel()
		if err != nil {
			db.State("system.disk.smart.attributes_available", false, dims)
			db.EventDetails("system.disk.smart.attributes.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "smart_attributes"}), monitoring.EventDetails{
				Attributes: map[string]any{"error": err.Error()},
			})
			b.Merge(db.Batch())
			continue
		}
		db.State("system.disk.smart.attributes_available", true, dims)
		emitSmartAttributes(db, dims, attributes)
		b.Merge(db.Batch())
	}
}

func collectNVMe(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, timeout time.Duration) {
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
		b.EventDetails("system.disk.nvme.list.failed", map[string]string{"operation": "nvme_list"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	devices := parseNVMeList(out)
	b.Metric("system.disk.nvme.devices", "gauge", float64(len(devices)), "", nil)
	for _, device := range devices {
		dims := map[string]string{"device": device.Path}
		db := monitoring.NewBuilder(diskScope(scope, device.Path, device.Serial, diskLabel(device.Path, device.Model, device.Serial)))
		db.State("system.disk.nvme.present", true, dims)
		if device.Model != "" {
			db.State("system.disk.model", device.Model, dims)
		}
		if device.Serial != "" {
			db.State("system.disk.serial", device.Serial, dims)
		}
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := exec.CommandContext(runCtx, path, "smart-log", "-o", "json", device.Path).Output()
		cancel()
		if err != nil {
			db.State("system.disk.nvme.smart_available", false, dims)
			db.EventDetails("system.disk.nvme.smart.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "nvme_smart"}), monitoring.EventDetails{
				Attributes: map[string]any{"error": err.Error()},
			})
			b.Merge(db.Batch())
			continue
		}
		emitNVMeSmart(db, dims, out)
		b.Merge(db.Batch())
	}
}

func diskScope(scope monitoring.Scope, device, serial, label string) monitoring.Scope {
	scope.EntityType = "disk"
	scope.EntityID = entity.ID("disk", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), entity.Key(serial, device))
	scope.Label = displayLabel(label, device, serial)
	return scope
}

func displayLabel(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return "unknown"
}

func diskLabel(device, model, serial string) string {
	if model != "" && serial != "" {
		return model + " (" + shortSerial(serial) + ")"
	}
	if model != "" {
		return model
	}
	return device
}

func shortSerial(serial string) string {
	serial = strings.TrimSpace(serial)
	if len(serial) <= 8 {
		return serial
	}
	return serial[:8]
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

type smartAttribute struct {
	Name  string
	Value float64
}

func parseSmartAttributes(out []byte) []smartAttribute {
	var attrs []smartAttribute
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 || !isNumber(fields[0]) {
			continue
		}
		name := normalizeSmartAttribute(fields[1])
		if !wantedSmartAttribute(name) {
			continue
		}
		value, ok := firstNumber(strings.Join(fields[9:], " "))
		if !ok {
			continue
		}
		attrs = append(attrs, smartAttribute{Name: name, Value: value})
	}
	return attrs
}

func emitSmartAttributes(b *monitoring.Builder, dims map[string]string, out []byte) {
	for _, attr := range parseSmartAttributes(out) {
		attrDims := copyDims(dims)
		attrDims["attribute"] = attr.Name
		b.Metric("system.disk.smart.attribute.raw", "gauge", attr.Value, "count", attrDims)
		switch attr.Name {
		case "reallocated_sector_ct":
			b.Metric("system.disk.smart.reallocated_sectors", "gauge", attr.Value, "count", dims)
		case "current_pending_sector":
			b.Metric("system.disk.smart.pending_sectors", "gauge", attr.Value, "count", dims)
		case "offline_uncorrectable":
			b.Metric("system.disk.smart.uncorrectable_sectors", "gauge", attr.Value, "count", dims)
		case "udma_crc_error_count":
			b.Metric("system.disk.smart.udma_crc_errors", "gauge", attr.Value, "count", dims)
		case "power_on_hours":
			b.Metric("system.disk.smart.power_on_hours", "gauge", attr.Value, "hours", dims)
		case "power_cycle_count":
			b.Metric("system.disk.smart.power_cycles", "gauge", attr.Value, "count", dims)
		case "temperature_celsius", "airflow_temperature_cel":
			b.Metric("system.disk.smart.temperature", "gauge", attr.Value, "celsius", dims)
		}
	}
}

func wantedSmartAttribute(name string) bool {
	switch name {
	case "reallocated_sector_ct",
		"current_pending_sector",
		"offline_uncorrectable",
		"udma_crc_error_count",
		"power_on_hours",
		"power_cycle_count",
		"temperature_celsius",
		"airflow_temperature_cel",
		"wear_leveling_count",
		"media_wearout_indicator",
		"percent_lifetime_remain":
		return true
	default:
		return false
	}
}

func normalizeSmartAttribute(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func firstNumber(value string) (float64, bool) {
	for _, field := range strings.Fields(value) {
		field = strings.Trim(field, "(),")
		parsed, err := strconv.ParseFloat(field, 64)
		if err == nil {
			return parsed, true
		}
	}
	return 0, false
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
		b.EventDetails("system.disk.nvme.smart.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "parse_nvme_smart"}), monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	b.State("system.disk.nvme.smart_available", true, dims)
	if v, ok := jsonNumber(raw["critical_warning"]); ok {
		b.Metric("system.disk.nvme.critical_warning", "gauge", v, "", dims)
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

func copyDims(dims map[string]string) map[string]string {
	out := make(map[string]string, len(dims)+1)
	for k, v := range dims {
		out[k] = v
	}
	return out
}

func isNumber(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
