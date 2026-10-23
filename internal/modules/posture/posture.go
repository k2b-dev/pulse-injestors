// Package posture reports workstation protections without changing them.
package posture

import (
	"context"
	"encoding/json"
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
	HostRoot string
	Run      localexec.Runner
}

func (c Collector) Name() string           { return "posture" }
func (c Collector) Cadence() time.Duration { return 24 * time.Hour }
func (c Collector) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c.Run != nil {
		return c.Run(ctx, name, args...)
	}
	return localexec.Output(ctx, 1<<20, name, args...)
}
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	var errs []error
	for _, p := range []struct {
		name  string
		probe func(context.Context) (bool, bool, error)
	}{
		{"disk_encryption", c.encryption}, {"firewall", c.firewall}, {"autoupdate", c.autoupdate},
	} {
		enabled, known, err := p.probe(ctx)
		var value any = "unknown"
		if known {
			value = enabled
		}
		b.State("system."+p.name+".enabled", value, nil)
		b.State("system.posture.available", known, map[string]string{"check": p.name})
		if err != nil && !errors.Is(err, exec.ErrNotFound) {
			errs = append(errs, fmt.Errorf("%s: %w", p.name, err))
		}
	}
	return b.Batch(), errors.Join(errs...)
}

type blockDevice struct {
	Type        string        `json:"type"`
	Mountpoints []*string     `json:"mountpoints"`
	Children    []blockDevice `json:"children"`
}

func (c Collector) encryption(ctx context.Context) (bool, bool, error) {
	out, err := c.command(ctx, "lsblk", "--json", "--output", "NAME,TYPE,MOUNTPOINTS")
	if err != nil {
		return false, false, err
	}
	return parseEncryption(out)
}
func parseEncryption(out []byte) (bool, bool, error) {
	var data struct {
		Devices []blockDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return false, false, errors.New("invalid lsblk response")
	}
	root, encrypted := false, false
	var walk func([]blockDevice, bool, int) error
	walk = func(devices []blockDevice, crypt bool, depth int) error {
		if depth > 32 {
			return errors.New("lsblk nesting limit")
		}
		for _, d := range devices {
			if d.Type == "" {
				return errors.New("lsblk device type missing")
			}
			protected := crypt || d.Type == "crypt"
			for _, m := range d.Mountpoints {
				if m == nil {
					continue
				}
				if *m == "/" {
					root = true
				}
				if *m == "/" || *m == "/home" || strings.HasPrefix(*m, "/home/") {
					encrypted = encrypted || protected
				}
			}
			if err := walk(d.Children, protected, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(data.Devices, false, 0); err != nil {
		return false, false, err
	}
	if !root && !encrypted {
		return false, false, nil
	}
	return encrypted, true, nil
}
func (c Collector) firewall(ctx context.Context) (bool, bool, error) {
	known, enabled := false, false
	var errs []error
	out, err := c.command(ctx, "nft", "--json", "list", "ruleset")
	if err == nil {
		var data struct {
			Rules []map[string]json.RawMessage `json:"nftables"`
		}
		if json.Unmarshal(out, &data) != nil || data.Rules == nil {
			errs = append(errs, errors.New("invalid nft ruleset"))
		} else {
			known = true
			for _, r := range data.Rules {
				for key := range r {
					if key != "metainfo" {
						enabled = true
					}
				}
			}
		}
	} else if !errors.Is(err, exec.ErrNotFound) {
		errs = append(errs, err)
	}
	out, err = c.command(ctx, "firewall-cmd", "--state")
	text := strings.TrimSpace(string(out))
	if text == "" {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			text = strings.TrimSpace(string(exit.Stderr))
		}
	}
	if err == nil && text == "running" {
		known = true
		enabled = true
	} else if text == "not running" && (err == nil || exitCode(err) == 252 || exitCode(err) == 1) {
		known = true
	} else if !errors.Is(err, exec.ErrNotFound) {
		if err == nil {
			err = errors.New("invalid firewalld status")
		}
		errs = append(errs, err)
	}
	out, err = c.command(ctx, "ufw", "status")
	if err == nil {
		switch {
		case strings.HasPrefix(string(out), "Status: active"):
			known = true
			enabled = true
		case strings.HasPrefix(string(out), "Status: inactive"):
			known = true
		default:
			errs = append(errs, errors.New("invalid ufw status"))
		}
	} else if !errors.Is(err, exec.ErrNotFound) {
		errs = append(errs, err)
	}
	// A positive result is decisive; failed probes must not produce a false negative.
	return enabled, enabled || known && len(errs) == 0, errors.Join(errs...)
}
func exitCode(err error) int {
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}
func (c Collector) timer(ctx context.Context, name string) (bool, error) {
	out, err := c.command(ctx, "systemctl", "show", name, "--property=LoadState", "--property=UnitFileState", "--no-pager")
	if err != nil {
		return false, err
	}
	v := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		k, val, ok := strings.Cut(line, "=")
		if ok {
			v[k] = val
		}
	}
	if v["LoadState"] == "not-found" {
		return false, nil
	}
	if v["LoadState"] != "loaded" {
		return false, errors.New("timer load state unknown")
	}
	switch v["UnitFileState"] {
	case "enabled", "enabled-runtime":
		return true, nil
	case "disabled", "masked", "masked-runtime", "static", "indirect":
		return false, nil
	}
	return false, errors.New("timer enablement unknown")
}
func (c Collector) autoupdate(ctx context.Context) (bool, bool, error) {
	root := c.HostRoot
	if root == "" {
		root = "/"
	}
	out, err := os.ReadFile(filepath.Join(root, "etc/os-release"))
	if errors.Is(err, os.ErrNotExist) {
		out, err = os.ReadFile(filepath.Join(root, "usr/lib/os-release"))
	}
	if err != nil {
		return false, false, err
	}
	distro := ""
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if ok && (k == "ID" || k == "ID_LIKE") {
			distro += " " + strings.Trim(v, "\"'")
		}
	}
	ids := " " + strings.Join(strings.Fields(distro), " ") + " "
	if strings.Contains(ids, " debian ") || strings.Contains(ids, " ubuntu ") {
		out, err := c.command(ctx, "apt-config", "dump")
		if err != nil {
			return false, false, err
		}
		periodic, globallyEnabled := false, true
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && (f[0] == "APT::Periodic::Unattended-Upgrade" || f[0] == "APT::Periodic::Enable") {
				n, e := strconv.Atoi(strings.Trim(f[1], "\";"))
				if e != nil {
					return false, false, errors.New("invalid unattended-upgrades preference")
				}
				if f[0] == "APT::Periodic::Enable" {
					globallyEnabled = n > 0
				} else {
					periodic = n > 0
				}
			}
		}
		enabled, err := c.timer(ctx, "apt-daily-upgrade.timer")
		return globallyEnabled && periodic && enabled, err == nil, err
	}
	if strings.Contains(ids, " fedora ") || strings.Contains(ids, " rhel ") {
		var errs []error
		enabled := false
		for _, name := range []string{"dnf-automatic.timer", "dnf-automatic-install.timer"} {
			yes, e := c.timer(ctx, name)
			enabled = enabled || yes
			errs = append(errs, e)
		}
		err := errors.Join(errs...)
		return enabled, enabled || err == nil, err
	}
	return false, false, nil
}
