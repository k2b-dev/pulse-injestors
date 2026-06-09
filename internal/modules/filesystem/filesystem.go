package filesystem

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	ProcRoot string
	HostRoot string
}

type Mount struct {
	Point   string
	Source  string
	FSType  string
	Options string
}

func (c Collector) Name() string { return "filesystem" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	_ = ctx
	proc := c.ProcRoot
	if proc == "" {
		proc = "/proc"
	}
	hostRoot := c.HostRoot
	if hostRoot == "" {
		hostRoot = "/"
	}
	mounts, err := ReadMounts(filepath.Join(proc, "self", "mountinfo"))
	if err != nil {
		return pulse.Batch{}, err
	}
	b := monitoring.NewBuilder(scope)
	var errs []error
	collected := 0
	seen := map[string]bool{}
	for _, mount := range mounts {
		if skipFSType(mount.FSType) || seen[mount.Point] {
			continue
		}
		seen[mount.Point] = true
		path := filepath.Join(hostRoot, strings.TrimPrefix(mount.Point, "/"))
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			errs = append(errs, fmt.Errorf("statfs %s: %w", mount.Point, err))
			continue
		}
		blockSize := uint64(st.Bsize)
		total := st.Blocks * blockSize
		avail := st.Bavail * blockSize
		free := st.Bfree * blockSize
		used := total - free
		dims := map[string]string{
			"mount":  mount.Point,
			"fstype": mount.FSType,
			"source": mount.Source,
		}
		b.Metric("system.filesystem.total", "gauge", float64(total), "bytes", dims)
		b.Metric("system.filesystem.available", "gauge", float64(avail), "bytes", dims)
		b.Metric("system.filesystem.used", "gauge", float64(used), "bytes", dims)
		if total > 0 {
			b.Metric("system.filesystem.usage", "gauge", (float64(used)/float64(total))*100, "percent", dims)
		}
		if st.Files > 0 {
			filesUsed := st.Files - st.Ffree
			b.Metric("system.filesystem.inodes.used", "gauge", float64(filesUsed), "count", dims)
			b.Metric("system.filesystem.inodes.usage", "gauge", (float64(filesUsed)/float64(st.Files))*100, "percent", dims)
		}
		b.State("system.filesystem.readonly", strings.Contains(","+mount.Options+",", ",ro,"), dims)
		collected++
	}
	if collected > 0 {
		return b.Batch(), nil
	}
	return b.Batch(), errors.Join(errs...)
}

func ReadMounts(path string) ([]Mount, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var mounts []Mount
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		sep := -1
		for i, field := range fields {
			if field == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) <= sep+3 {
			continue
		}
		mounts = append(mounts, Mount{
			Point:   unescapeMount(fields[4]),
			Options: fields[5],
			FSType:  fields[sep+1],
			Source:  unescapeMount(fields[sep+2]),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return mounts, nil
}

func unescapeMount(s string) string {
	for _, code := range []string{"040", "011", "012", "134"} {
		v, err := strconv.ParseInt(code, 8, 32)
		if err == nil {
			s = strings.ReplaceAll(s, `\`+code, string(rune(v)))
		}
	}
	return s
}

func skipFSType(fs string) bool {
	switch fs {
	case "autofs", "binfmt_misc", "bpf", "cgroup", "cgroup2", "configfs", "debugfs", "devpts", "devtmpfs", "efivarfs", "fusectl", "hugetlbfs", "mqueue", "nsfs", "overlay", "proc", "pstore", "rpc_pipefs", "securityfs", "sysfs", "tmpfs", "tracefs":
		return true
	default:
		return false
	}
}
