package systemd

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	Units   []string
	Timeout time.Duration
}

func (c Collector) Name() string { return "systemd" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	units := cleanUnits(c.Units)
	if len(units) == 0 {
		b.State("system.systemd.units.configured", false, nil)
		return b.Batch(), nil
	}
	b.State("system.systemd.units.configured", true, nil)
	path, err := exec.LookPath("systemctl")
	if err != nil {
		b.State("system.systemd.available", false, nil)
		return b.Batch(), nil
	}
	b.State("system.systemd.available", true, nil)
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	for _, unit := range units {
		ub := monitoring.NewBuilder(unitScope(scope, unit))
		collectUnit(ctx, ub, path, unit, timeout)
		b.Merge(ub.Batch())
	}
	return b.Batch(), nil
}

func unitScope(scope monitoring.Scope, unit string) monitoring.Scope {
	scope.EntityType = "service"
	scope.EntityID = entity.ID("service", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), "systemd", unit)
	scope.Label = unit
	return scope
}

func collectUnit(ctx context.Context, b *monitoring.Builder, systemctl, unit string, timeout time.Duration) {
	dims := map[string]string{"service": unit, "service_manager": "systemd"}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(runCtx, systemctl, "show", unit,
		"--property=LoadState",
		"--property=ActiveState",
		"--property=SubState",
		"--property=UnitFileState",
		"--property=Description",
		"--no-pager",
	).Output()
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
