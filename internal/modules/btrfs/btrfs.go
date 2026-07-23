package btrfs

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/entity"
	"github.com/valentinkolb/pulse-injestors/internal/modules/filesystem"
	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	ProcRoot string
	HostRoot string
	Timeout  time.Duration
}

func (c Collector) Name() string { return "btrfs" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	if _, err := exec.LookPath("btrfs"); err != nil {
		b.State("system.btrfs.available", false, nil)
		return b.Batch(), nil
	}
	proc := c.ProcRoot
	if proc == "" {
		proc = "/proc"
	}
	hostRoot := c.HostRoot
	if hostRoot == "" {
		hostRoot = "/"
	}
	mounts, err := filesystem.ReadMounts(filepath.Join(proc, "self", "mountinfo"))
	if err != nil {
		b.State("system.btrfs.available", false, nil)
		b.EventDetails("system.btrfs.mounts.failed", map[string]string{"operation": "read_mounts"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return b.Batch(), nil
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	count := 0
	collected := 0
	for _, mount := range mounts {
		if mount.FSType != "btrfs" {
			continue
		}
		count++
		dims := map[string]string{"mount": mount.Point}
		fb := monitoring.NewBuilder(btrfsScope(scope, mount))
		fb.State("system.btrfs.mount.present", true, dims)
		fb.State("system.filesystem.source", mount.Source, dims)
		path := filepath.Join(hostRoot, strings.TrimPrefix(mount.Point, "/"))
		collectCtx, cancel := context.WithTimeout(ctx, timeout)
		out, err := exec.CommandContext(collectCtx, "btrfs", "filesystem", "usage", "-b", path).Output()
		cancel()
		if err != nil {
			fb.State("system.btrfs.usage.available", false, dims)
			fb.EventDetails("system.btrfs.usage.failed", monitoring.MergeDimensions(dims, map[string]string{"operation": "filesystem_usage"}), monitoring.EventDetails{
				Attributes: map[string]any{"error": fmt.Sprintf("btrfs filesystem usage %s: %v", mount.Point, err)},
			})
			b.Merge(fb.Batch())
			continue
		}
		fb.State("system.btrfs.usage.available", true, dims)
		emitUsage(fb, mount, out)
		b.Merge(fb.Batch())
		collected++
	}
	b.State("system.btrfs.available", count > 0, nil)
	if count > 0 {
		b.Metric("system.btrfs.filesystems", "gauge", float64(count), "", nil)
		b.Metric("system.btrfs.filesystems.collected", "gauge", float64(collected), "", nil)
	}
	return b.Batch(), nil
}

func btrfsScope(scope monitoring.Scope, mount filesystem.Mount) monitoring.Scope {
	scope.EntityType = "filesystem"
	scope.EntityID = entity.ID("filesystem", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), entity.Key(mount.Point, "root"))
	scope.Label = mount.Point
	return scope
}

var bytesLine = regexp.MustCompile(`^([A-Za-z ]+):\s+([0-9]+)`)

func emitUsage(b *monitoring.Builder, mount filesystem.Mount, out []byte) {
	dims := map[string]string{"mount": mount.Point}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		m := bytesLine.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		value, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		switch strings.TrimSpace(m[1]) {
		case "Device size":
			b.Metric("system.btrfs.device.size", "gauge", value, "bytes", dims)
		case "Device allocated":
			b.Metric("system.btrfs.device.allocated", "gauge", value, "bytes", dims)
		case "Device unallocated":
			b.Metric("system.btrfs.device.unallocated", "gauge", value, "bytes", dims)
		case "Used":
			b.Metric("system.btrfs.used", "gauge", value, "bytes", dims)
		case "Free":
			b.Metric("system.btrfs.free", "gauge", value, "bytes", dims)
		}
	}
}
