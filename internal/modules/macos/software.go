package macos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

import "github.com/k2b-dev/pulse-injestors/internal/monitoring"

func (c Collector) brewTimeout() time.Duration {
	if c.HomebrewTimeout > 0 {
		return c.HomebrewTimeout
	}
	return 20 * time.Second
}
func (c Collector) updateTimeout() time.Duration {
	if c.SoftwareUpdateTimeout > 0 {
		return c.SoftwareUpdateTimeout
	}
	return 60 * time.Second
}
func (c Collector) brew(ctx context.Context, args ...string) ([]byte, error) {
	if c.Run != nil {
		return c.command(ctx, c.brewTimeout(), "brew", args...)
	}
	path, err := exec.LookPath("brew")
	if err != nil {
		for _, candidate := range []string{"/opt/homebrew/bin/brew", "/usr/local/bin/brew"} {
			if p, e := exec.LookPath(candidate); e == nil {
				path = p
				err = nil
				break
			}
		}
	}
	if err != nil {
		return nil, exec.ErrNotFound
	}
	argv := append([]string{"HOMEBREW_NO_AUTO_UPDATE=1", "HOMEBREW_NO_ANALYTICS=1", path}, args...)
	return c.command(ctx, c.brewTimeout(), "/usr/bin/env", argv...)
}
func (c Collector) updates(ctx context.Context, b *monitoring.Builder) error {
	var errs []error
	b.State("macos.softwareupdate.enabled", c.EnableSoftwareUpdate, nil)
	if c.EnableSoftwareUpdate {
		out, err := c.command(ctx, c.updateTimeout(), "softwareupdate", "-l")
		count := 0
		recognized := false
		if err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "- Label:") {
					count++
					recognized = true
				}
				if strings.Contains(line, "No new software available") {
					recognized = true
				}
			}
			if !recognized {
				err = errors.New("unrecognized softwareupdate listing")
			}
		}
		b.State("macos.softwareupdate.available", err == nil, nil)
		if err == nil {
			b.Metric("system.packages.updates.total", "gauge", float64(count), "", nil)
		} else {
			errs = append(errs, optional(err))
		}
	} else {
		b.State("macos.softwareupdate.available", false, nil)
	}
	b.State("package.homebrew.enabled", c.EnableHomebrew, nil)
	if c.EnableHomebrew {
		out, err := c.brew(ctx, "outdated", "--json=v2")
		b.State("package.homebrew.available", !errors.Is(err, exec.ErrNotFound), nil)
		var parsed struct {
			Formulae []json.RawMessage `json:"formulae"`
			Casks    []json.RawMessage `json:"casks"`
		}
		if err == nil {
			err = json.Unmarshal(out, &parsed)
			if err == nil && (parsed.Formulae == nil || parsed.Casks == nil) {
				err = errors.New("incomplete brew outdated response")
			}
		}
		b.State("package.homebrew.outdated.available", err == nil, nil)
		if err == nil {
			b.Metric("system.packages.homebrew.outdated.total", "gauge", float64(len(parsed.Formulae)+len(parsed.Casks)), "", nil)
		} else {
			errs = append(errs, optional(err))
		}
	} else {
		b.State("package.homebrew.available", false, nil)
		b.State("package.homebrew.outdated.available", false, nil)
	}
	return errors.Join(errs...)
}
func (c Collector) software(ctx context.Context, b *monitoring.Builder) error {
	// Even direct callers get a total deadline, independent of the app count.
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	appErr := c.apps(ctx, b)
	brewErr := c.brewInventory(ctx, b)
	return errors.Join(appErr, brewErr)
}
func (c Collector) apps(ctx context.Context, b *monitoring.Builder) error {
	root := c.ApplicationsDir
	if root == "" {
		root = "/Applications"
	}
	count, failed := 0, 0
	truncated := false
	versions := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !strings.HasSuffix(d.Name(), ".app") || (!d.IsDir() && d.Type()&fs.ModeSymlink == 0) {
			return nil
		}
		// WalkDir does not follow symlinks, but a linked app bundle is a valid app.
		skip := error(nil)
		if d.IsDir() {
			skip = fs.SkipDir
		}
		if count == 1000 {
			truncated = true
			return fs.SkipAll
		}
		count++
		plist := filepath.Join(path, "Contents", "Info.plist")
		id, e := c.probe(ctx, "plutil", "-extract", "CFBundleIdentifier", "raw", "-o", "-", plist)
		if e != nil || len(strings.TrimSpace(string(id))) == 0 {
			failed++
			return skip
		}
		version, e := c.probe(ctx, "plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-", plist)
		if e != nil {
			version, e = c.probe(ctx, "plutil", "-extract", "CFBundleVersion", "raw", "-o", "-", plist)
		}
		if e != nil || len(strings.TrimSpace(string(version))) == 0 {
			failed++
			return skip
		}
		versions[strings.TrimSpace(string(id))] = strings.TrimSpace(string(version))
		return skip
	})
	dims := map[string]string{"arch": "app"}
	b.State("software.packages.available", err == nil && failed == 0, dims)
	b.State("software.packages.truncated", truncated, dims)
	emitVersions(b, versions, "app", 1000)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if failed > 0 {
		return fmt.Errorf("application inventory: %d unreadable bundle records", failed)
	}
	return nil
}
func (c Collector) brewInventory(ctx context.Context, b *monitoring.Builder) error {
	dims := map[string]string{"arch": "brew"}
	if !c.EnableHomebrew {
		b.State("software.packages.available", false, dims)
		return nil
	}
	out, err := c.brew(ctx, "info", "--json=v2", "--installed")
	versions := map[string]string{}
	if err == nil {
		versions, err = parseBrewInventory(out)
	}
	b.State("software.packages.available", err == nil, dims)
	if err != nil {
		return optional(err)
	}
	b.State("software.packages.truncated", len(versions) > 2000, dims)
	emitVersions(b, versions, "brew", 2000)
	return nil
}
func parseBrewInventory(out []byte) (map[string]string, error) {
	var data struct {
		Formulae []struct {
			Name      string `json:"name"`
			Installed []struct {
				Version string `json:"version"`
			} `json:"installed"`
		} `json:"formulae"`
		Casks []struct {
			Token     string          `json:"token"`
			Installed json.RawMessage `json:"installed"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(out, &data); err != nil {
		return nil, err
	}
	if data.Formulae == nil || data.Casks == nil {
		return nil, errors.New("incomplete brew inventory")
	}
	versions := map[string]string{}
	for _, f := range data.Formulae {
		vals := []string{}
		for _, i := range f.Installed {
			if i.Version != "" {
				vals = append(vals, i.Version)
			}
		}
		sort.Strings(vals)
		if f.Name == "" || len(vals) == 0 {
			return nil, errors.New("invalid installed formula")
		}
		versions["formula:"+f.Name] = strings.Join(vals, ", ")
	}
	for _, c := range data.Casks {
		var v string
		var vals []string
		if err := json.Unmarshal(c.Installed, &v); err != nil {
			if err = json.Unmarshal(c.Installed, &vals); err != nil {
				return nil, errors.New("invalid cask versions")
			}
			sort.Strings(vals)
			v = strings.Join(vals, ", ")
		}
		if c.Token == "" || v == "" {
			return nil, errors.New("invalid installed cask")
		}
		versions["cask:"+c.Token] = v
	}
	return versions, nil
}
func emitVersions(b *monitoring.Builder, versions map[string]string, arch string, limit int) {
	names := make([]string, 0, len(versions))
	for name := range versions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names[:min(len(names), limit)] {
		b.State("software.package.version", versions[name], map[string]string{"package": name, "arch": arch})
	}
}
