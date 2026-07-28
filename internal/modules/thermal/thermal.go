package thermal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	SysRoot string
}

func (c Collector) Name() string { return "thermal" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	_ = ctx
	sys := c.SysRoot
	if sys == "" {
		sys = "/sys"
	}
	b := monitoring.NewBuilder(scope)
	var errs []error
	count := 0

	zones, err := filepath.Glob(filepath.Join(sys, "class", "thermal", "thermal_zone*"))
	if err != nil {
		errs = append(errs, err)
	}
	for _, zone := range zones {
		temp, err := readMilliCelsius(filepath.Join(zone, "temp"))
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", zone, err))
			continue
		}
		typ := readTrimmed(filepath.Join(zone, "type"))
		dims := map[string]string{
			"sensor": filepath.Base(zone),
			"type":   typ,
		}
		b.Metric("system.temperature", "gauge", temp, "celsius", dims)
		count++
	}
	hwmons, err := filepath.Glob(filepath.Join(sys, "class", "hwmon", "hwmon*"))
	if err != nil {
		errs = append(errs, err)
	}
	for _, hwmon := range hwmons {
		name := readTrimmed(filepath.Join(hwmon, "name"))
		temps, _ := filepath.Glob(filepath.Join(hwmon, "temp*_input"))
		for _, input := range temps {
			temp, err := readMilliCelsius(input)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", input, err))
				continue
			}
			prefix := strings.TrimSuffix(filepath.Base(input), "_input")
			label := readTrimmed(filepath.Join(hwmon, prefix+"_label"))
			sensor := filepath.Base(hwmon) + "." + prefix
			dims := map[string]string{
				"sensor": sensor,
				"chip":   name,
				"label":  label,
			}
			b.Metric("system.temperature", "gauge", temp, "celsius", dims)
			count++
		}
	}
	b.State("system.temperature.available", count > 0, nil)
	if count > 0 || len(errs) == 0 {
		return b.Batch(), nil
	}
	return b.Batch(), errors.Join(errs...)
}

func readMilliCelsius(path string) (float64, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
	if err != nil {
		return 0, err
	}
	return v / 1000, nil
}

func readTrimmed(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
