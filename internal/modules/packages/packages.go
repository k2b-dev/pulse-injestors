package packages

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	Timeout time.Duration
}

func (c Collector) Name() string { return "packages" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	b := monitoring.NewBuilder(scope)
	total := 0
	found := false
	for _, manager := range []packageManager{
		{Name: "apt", Command: "apt", Args: []string{"list", "--upgradeable"}, Count: countAptUpdates},
		{Name: "dnf", Command: "dnf", Args: []string{"check-update", "--quiet"}, AllowedExit: 100, Count: countLineUpdates},
		{Name: "pacman", Command: "pacman", Args: []string{"-Qu"}, Count: countLineUpdates},
	} {
		count, ok := collectManager(ctx, b, manager, timeout)
		if ok {
			found = true
			total += count
		}
	}
	b.State("system.packages.available", found, nil)
	if found {
		b.Metric("system.packages.updates.total", "gauge", float64(total), "", nil)
	}
	return b.Batch(), nil
}

type packageManager struct {
	Name        string
	Command     string
	Args        []string
	AllowedExit int
	Count       func([]byte) int
}

func collectManager(ctx context.Context, b *monitoring.Builder, manager packageManager, timeout time.Duration) (int, bool) {
	dims := map[string]string{"manager": manager.Name}
	path, err := exec.LookPath(manager.Command)
	if err != nil {
		b.State("system.packages.manager.available", false, dims)
		return 0, false
	}
	b.State("system.packages.manager.available", true, dims)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := exec.CommandContext(runCtx, path, manager.Args...).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != manager.AllowedExit {
			b.State("system.packages.manager.updates_available", false, dims)
			b.EventDetails("system.packages.manager.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "list_updates"}), monitoring.EventDetails{
				Attributes: map[string]any{"error": err.Error()},
			})
			return 0, false
		}
	}
	count := manager.Count(out)
	b.State("system.packages.manager.updates_available", true, dims)
	b.Metric("system.packages.manager.updates", "gauge", float64(count), "", dims)
	return count, true
}

func countAptUpdates(out []byte) int {
	count := 0
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "Listing...") || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		if strings.Contains(line, "/") {
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
		if strings.Fields(line) != nil {
			count++
		}
	}
	return count
}
