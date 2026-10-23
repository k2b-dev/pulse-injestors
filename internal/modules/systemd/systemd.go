package systemd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	Run     localexec.Runner
	Units   []string
	Timeout time.Duration
}

func (c Collector) Name() string { return "systemd" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	units := cleanUnits(c.Units)
	b.State("system.systemd.units.configured", len(units) > 0, nil)
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	run := c.Run
	if run == nil {
		run = localexec.Run
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := run(runCtx, "systemctl", "list-units", "--state=failed", "--plain", "--no-legend", "--no-pager")
	cancel()
	if errors.Is(err, exec.ErrNotFound) || unavailable(err) {
		b.State("system.systemd.available", false, nil)
		return b.Batch(), nil
	}
	b.State("system.systemd.available", err == nil, nil)
	if err != nil {
		return b.Batch(), fmt.Errorf("list failed units: %w", err)
	}
	names := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			names = append(names, f[0])
		}
	}
	names = cleanUnits(names)
	sort.Strings(names)
	b.Metric("system.systemd.failed_units", "gauge", float64(len(names)), "", nil)
	joined := []rune(strings.Join(names, ","))
	if len(joined) > 500 {
		joined = joined[:500]
	}
	b.State("system.systemd.failed", string(joined), nil)
	for _, unit := range units {
		ub := monitoring.NewBuilder(unitScope(scope, unit))
		collectUnit(ctx, ub, run, unit, timeout)
		b.Merge(ub.Batch())
	}
	return b.Batch(), nil
}

func unitScope(scope monitoring.Scope, unit string) monitoring.Scope {
	scope.Resource.Type = "service"
	scope.Resource.ID = entity.ID(entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions), "systemd", unit)
	scope.Resource.Label = unit
	return scope
}

func collectUnit(ctx context.Context, b *monitoring.Builder, run localexec.Runner, unit string, timeout time.Duration) {
	dims := map[string]string{"service": unit, "service_manager": "systemd"}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := run(runCtx, "systemctl", "show", unit,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--property=UnitFileState",
		"--property=Description",
		"--no-pager",
	)
	if err != nil {
		b.State("system.service.available", false, dims)
		b.EventDetails("system.service.collect.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "systemctl_show"}), monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	values := keyValues(out)
	active := values["ActiveState"]
	load := values["LoadState"]
	b.State("system.service.available", load != "not-found", dims)
	b.State("system.service.loaded", load == "loaded", dims)
	b.State("system.service.active", active == "active", dims)
	emitString(b, "system.service.load_state", values["LoadState"], dims)
	emitString(b, "system.service.active_state", values["ActiveState"], dims)
	emitString(b, "system.service.sub_state", values["SubState"], dims)
	emitString(b, "system.service.unit_file_state", values["UnitFileState"], dims)
	emitString(b, "system.service.description", values["Description"], dims)
	if active == "failed" {
		b.Event("system.service.failed", dims, nil)
	}
}

func cleanUnits(units []string) []string {
	out := make([]string, 0, len(units))
	seen := map[string]bool{}
	for _, unit := range units {
		unit = strings.TrimSpace(unit)
		if unit == "" || seen[unit] {
			continue
		}
		seen[unit] = true
		out = append(out, unit)
	}
	return out
}

func keyValues(out []byte) map[string]string {
	values := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			values[parts[0]] = parts[1]
		}
	}
	return values
}

func emitString(b *monitoring.Builder, key, value string, dims map[string]string) {
	if value != "" {
		b.State(key, value, dims)
	}
}

func unavailable(err error) bool {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return false
	}
	message := string(exit.Stderr)
	return strings.Contains(message, "System has not been booted with systemd") || strings.Contains(message, "Failed to connect to bus: No such file or directory") || strings.Contains(message, "Failed to connect to system scope bus via local transport: No such file or directory")
}
