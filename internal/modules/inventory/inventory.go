package inventory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	HostRoot, ProcRoot, SysRoot string
	Run                         localexec.Runner
}

func (c Collector) Name() string { return "inventory" }
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	root, proc, sys := c.HostRoot, c.ProcRoot, c.SysRoot
	if root == "" {
		root = "/"
	}
	if proc == "" {
		proc = "/proc"
	}
	if sys == "" {
		sys = "/sys"
	}
	b := monitoring.NewBuilder(scope)
	var errs []error
	raw, err := os.ReadFile(filepath.Join(root, "etc/os-release"))
	if errors.Is(err, os.ErrNotExist) {
		raw, err = os.ReadFile(filepath.Join(root, "usr/lib/os-release"))
	}
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	} else {
		names := map[string]string{"NAME": "name", "VERSION_ID": "version", "PRETTY_NAME": "pretty"}
		for _, line := range strings.Split(string(raw), "\n") {
			key, val, ok := strings.Cut(line, "=")
			if name := names[key]; ok && name != "" {
				b.State("system.os."+name, unquote(val), nil)
			}
		}
	}
	for _, file := range []struct{ path, key string }{
		{filepath.Join(proc, "sys/kernel/osrelease"), "system.kernel.release"},
		{filepath.Join(sys, "class/dmi/id/sys_vendor"), "system.hardware.vendor"},
		{filepath.Join(sys, "class/dmi/id/product_name"), "system.hardware.model"},
	} {
		raw, err := os.ReadFile(file.path)
		if err == nil {
			if value := strings.TrimSpace(string(raw)); value != "" {
				b.State(file.key, value, nil)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	raw, err = os.ReadFile(filepath.Join(proc, "cpuinfo"))
	if err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			key, val, ok := strings.Cut(line, ":")
			if ok && strings.TrimSpace(key) == "model name" {
				b.State("system.hardware.cpu_model", strings.TrimSpace(val), nil)
				break
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if err := c.packages(ctx, b); err != nil {
		errs = append(errs, err)
	}
	return b.Batch(), errors.Join(errs...)
}
func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
		quote := value[0]
		value = value[1 : len(value)-1]
		if quote == '"' {
			value = strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\$`, `$`, "\\`", "`").Replace(value)
		}
	}
	return value
}
func (c Collector) packages(ctx context.Context, b *monitoring.Builder) error {
	run := c.Run
	if run == nil {
		run = localexec.Run
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, err := run(runCtx, "rpm", "-qa", "--qf", "%{NAME}\t%{ARCH}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\n")
	dpkg := errors.Is(err, exec.ErrNotFound)
	if dpkg {
		out, err = run(runCtx, "dpkg-query", "-W", "-f", "${db:Status-Abbrev}\t${Package}\t${Architecture}\t${Version}\n")
	}
	b.State("software.packages.available", err == nil, nil)
	if errors.Is(err, exec.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("package inventory: %w", err)
	}
	// Deduplicate package+arch (rpm permits parallel installed versions). Sort
	// versions lexically for stable output, not a package-version ordering.
	versions := map[string]map[string]bool{}
	skipped := 0
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, "\t")
		if dpkg {
			if len(f) != 4 || len(strings.TrimSpace(f[0])) < 2 {
				skipped++
				continue
			}
			if status := strings.TrimSpace(f[0]); status != "ii" && status != "hi" {
				continue
			}
			f = f[1:]
		}
		if len(f) != 3 || f[0] == "" || f[1] == "" || f[2] == "" {
			skipped++
			continue
		}
		key := f[0] + "\t" + f[1]
		if versions[key] == nil {
			versions[key] = map[string]bool{}
		}
		versions[key][f[2]] = true
	}
	if skipped > 0 {
		b.State("software.packages.skipped", skipped, nil)
	}
	keys := make([]string, 0, len(versions))
	for key := range versions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	b.Metric("software.packages.installed", "gauge", float64(len(keys)), "", nil)
	b.State("software.packages.truncated", len(keys) > 5000, nil)
	for _, key := range keys[:min(len(keys), 5000)] {
		name, arch, _ := strings.Cut(key, "\t")
		values := make([]string, 0, len(versions[key]))
		for version := range versions[key] {
			values = append(values, version)
		}
		sort.Strings(values)
		b.State("software.package.version", strings.Join(values, ", "), map[string]string{"package": name, "arch": arch})
	}
	return nil
}
