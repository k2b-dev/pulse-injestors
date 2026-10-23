package packages

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	Timeout  time.Duration
	HostRoot string
	Run      localexec.Runner
}

func (c Collector) Name() string { return "packages" }
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	run := c.Run
	if run == nil {
		run = localexec.Run
	}
	b := monitoring.NewBuilder(scope)
	total, security := 0, 0
	found, securityKnown := false, false
	var errs []error
	for _, manager := range []packageManager{
		{Name: "apt", Command: "apt", Args: []string{"list", "--upgradeable"}, Count: countAptUpdates},
		{Name: "dnf", Command: "dnf", Args: []string{"--cacheonly", "check-update", "--quiet"}, AllowedExit: 100, Count: countLineUpdates},
		{Name: "pacman", Command: "pacman", Args: []string{"-Qu"}, AllowedExit: 1, Count: countLineUpdates},
	} {
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := run(runCtx, manager.Command, manager.Args...)
		cancel()
		dims := map[string]string{"manager": manager.Name}
		if errors.Is(err, exec.ErrNotFound) {
			b.State("system.packages.manager.available", false, dims)
			continue
		}
		b.State("system.packages.manager.available", true, dims)
		switch manager.Name {
		case "apt":
			root := c.HostRoot
			if root == "" {
				root = "/"
			}
			_, err := os.Stat(filepath.Join(root, "run/reboot-required"))
			if err == nil || errors.Is(err, os.ErrNotExist) {
				b.State("system.reboot_required", err == nil, nil)
			} else {
				errs = append(errs, err)
			}
		case "dnf":
			rebootCtx, cancel := context.WithTimeout(ctx, timeout)
			_, rebootErr := run(rebootCtx, "needs-restarting", "-r")
			cancel()
			if rebootErr == nil {
				b.State("system.reboot_required", false, nil)
			} else if exitCode(rebootErr) == 1 {
				b.State("system.reboot_required", true, nil)
			} else if !errors.Is(rebootErr, exec.ErrNotFound) {
				errs = append(errs, fmt.Errorf("reboot check: %w", rebootErr))
			}
		}
		if !allowed(err, manager.AllowedExit) {
			b.State("system.packages.manager.updates_available", false, dims)
			b.EventDetails("system.packages.manager.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "list_updates"}), monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
			continue
		}
		b.State("system.packages.manager.updates_available", true, dims)
		found = true
		total += manager.Count(out)
		switch manager.Name {
		case "apt":
			securityKnown = true
			security += countAptSecurity(out)
		case "dnf":
			secCtx, cancel := context.WithTimeout(ctx, timeout)
			out, err := run(secCtx, "dnf", "--cacheonly", "-q", "updateinfo", "list", "--security", "--available")
			cancel()
			if allowed(err, 100) {
				securityKnown = true
				security += countDNFSecurity(out)
			} else {
				errs = append(errs, fmt.Errorf("security updates: %w", err))
			}
		}
	}
	b.State("system.packages.available", found, nil)
	if found {
		b.Metric("system.packages.updates.total", "gauge", float64(total), "", nil)
	}
	if securityKnown {
		b.Metric("system.packages.security_updates", "gauge", float64(security), "", nil)
	}
	return b.Batch(), errors.Join(errs...)
}
func exitCode(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}
func allowed(err error, code int) bool { return err == nil || (code != 0 && exitCode(err) == code) }

type packageManager struct {
	Name, Command string
	Args          []string
	AllowedExit   int
	Count         func([]byte) int
}

func countAptUpdates(out []byte) int {
	count := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line != "" && !strings.HasPrefix(line, "Listing...") && !strings.HasPrefix(line, "WARNING:") && strings.Contains(line, "/") {
			count++
		}
	}
	return count
}
func countAptSecurity(out []byte) int {
	count := 0
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		_, source, ok := strings.Cut(f[0], "/")
		if ok && strings.Contains(source, "-security") {
			count++
		}
	}
	return count
}
func countLineUpdates(out []byte) int {
	count := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "Last metadata expiration check:") || strings.HasPrefix(line, "Obsoleting ") {
			continue
		}
		count++
	}
	return count
}

// countDNFSecurity counts package names, not advisories, versions or arches.
// updateinfo rows contain an advisory ID, severity/type and package NEVRA.
func countDNFSecurity(out []byte) int {
	names := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || !strings.HasSuffix(f[1], "/Sec.") {
			continue
		}
		// Strip architecture, release and version from the right so hyphenated
		// package names and optional epochs in the version remain unambiguous.
		arch := strings.LastIndexByte(f[2], '.')
		if arch <= 0 || arch == len(f[2])-1 {
			continue
		}
		nvr := f[2][:arch]
		release := strings.LastIndexByte(nvr, '-')
		if release <= 0 || release == len(nvr)-1 {
			continue
		}
		nv := nvr[:release]
		version := strings.LastIndexByte(nv, '-')
		if version <= 0 || version == len(nv)-1 {
			continue
		}
		names[nv[:version]] = true
	}
	return len(names)
}
