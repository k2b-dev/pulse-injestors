// Package kernel collects reboot, OOM and kernel journal health.
package kernel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	ProcRoot, StateDir   string
	Run                  localexec.Runner
	lastOOM              uint64
	haveOOM, bootChecked bool
	lastJournal          time.Time
}

func (c *Collector) Name() string { return "kernel" }
func (c *Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	proc := c.ProcRoot
	if proc == "" {
		proc = "/proc"
	}
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now().UTC()
	}
	b := monitoring.NewBuilder(scope)
	var errs []error
	raw, err := os.ReadFile(filepath.Join(proc, "vmstat"))
	if err != nil {
		b.State("system.oom.available", false, nil)
		if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	} else {
		n, ok, err := readCounter(raw, "oom_kill")
		b.State("system.oom.available", ok && err == nil, nil)
		if err != nil {
			errs = append(errs, err)
		} else if ok {
			b.Metric("system.oom_kills", "counter", float64(n), "", nil)
			if c.haveOOM && n > c.lastOOM {
				b.EventDetails("system.oom_kill", nil, monitoring.EventDetails{Attributes: map[string]any{"count": n - c.lastOOM}})
			}
			c.lastOOM, c.haveOOM = n, true
		}
	}
	raw, err = os.ReadFile(filepath.Join(proc, "stat"))
	if err != nil {
		errs = append(errs, err)
	} else {
		seconds, ok, err := readCounter(raw, "btime")
		if err != nil {
			errs = append(errs, err)
		} else if !ok {
			errs = append(errs, errors.New("stat btime missing"))
		} else if seconds > 253402300799 {
			errs = append(errs, errors.New("stat btime out of range"))
		} else {
			boot := time.Unix(int64(seconds), 0).UTC()
			b.State("system.boot_time", boot.Format(time.RFC3339), nil)
			if !c.bootChecked {
				uptime, err := os.ReadFile(filepath.Join(proc, "uptime"))
				if err != nil {
					errs = append(errs, err)
				} else {
					f := strings.Fields(string(uptime))
					age := float64(-1)
					if len(f) > 0 {
						age, err = strconv.ParseFloat(f[0], 64)
					}
					if err != nil || age < 0 {
						errs = append(errs, errors.New("invalid uptime"))
					} else {
						c.bootChecked = true
						if age < 900 {
							bs := scope
							bs.Timestamp = boot
							eb := monitoring.NewBuilder(bs)
							eb.Event("system.boot", nil, nil)
							b.Merge(eb.Batch())
						}
					}
				}
			}
		}
	}
	if err := c.collectJournal(ctx, scope, b); err != nil {
		errs = append(errs, err)
	}
	return b.Batch(), errors.Join(errs...)
}
func readCounter(raw []byte, key string) (uint64, bool, error) {
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == key {
			if len(f) != 2 {
				return 0, true, fmt.Errorf("invalid %s", key)
			}
			n, err := strconv.ParseUint(f[1], 10, 64)
			return n, true, err
		}
	}
	return 0, false, nil
}
func (c *Collector) collectJournal(ctx context.Context, scope monitoring.Scope, b *monitoring.Builder) error {
	args := []string{"-k", "-p", "0..3", "--no-pager", "-q", "-o", "short-iso"}
	if c.StateDir != "" {
		if err := os.MkdirAll(c.StateDir, 0700); err != nil {
			return err
		}
		cursor := filepath.Join(c.StateDir, "kernel.cursor")
		args = append(args, "--cursor-file="+cursor)
		if _, err := os.Stat(cursor); errors.Is(err, os.ErrNotExist) {
			args = append(args, "--since=-10min")
		} else if err != nil {
			return err
		}
	} else if c.lastJournal.IsZero() {
		args = append(args, "--since=-10min")
	} else {
		args = append(args, "--since="+c.lastJournal.UTC().Format("2006-01-02 15:04:05.999999 UTC"))
	}
	run := c.Run
	if run == nil {
		run = localexec.Run
	}
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := run(runCtx, "journalctl", args...)
	if errors.Is(err, exec.ErrNotFound) {
		b.State("system.kernel.journal.available", false, nil)
		return nil
	}
	b.State("system.kernel.journal.available", err == nil, nil)
	if err != nil {
		return fmt.Errorf("kernel journal: %w", err)
	}
	c.lastJournal = scope.Timestamp
	var sample []string
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		count++
		if len(sample) < 5 {
			r := []rune(line)
			if len(r) > 300 {
				r = r[:300]
			}
			sample = append(sample, string(r))
		}
	}
	b.Metric("system.kernel.errors", "gauge", float64(count), "", nil)
	if count > 0 {
		b.EventDetails("system.kernel.error", nil, monitoring.EventDetails{Attributes: map[string]any{"count": count, "sample": strings.Join(sample, "\n")}})
	}
	return nil
}
