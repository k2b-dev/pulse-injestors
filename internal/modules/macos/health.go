package macos

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
)

func (c Collector) health(ctx context.Context, b *monitoring.Builder) error {
	var errs []error
	raw, err := c.probe(ctx, "sysctl", "-n", "vm.loadavg")
	if err == nil {
		fields := strings.Fields(strings.NewReplacer("{", "", "}", "").Replace(string(raw)))
		if len(fields) != 3 {
			err = errors.New("invalid load averages")
		} else {
			for i, key := range []string{"system.load.1m", "system.load.5m", "system.load.15m"} {
				v, e := strconv.ParseFloat(fields[i], 64)
				if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
					err = errors.New("invalid load average")
					break
				}
				b.Metric(key, "gauge", v, "", nil)
			}
		}
	}
	errs = append(errs, err)
	total, err := c.sysctlUint(ctx, "hw.memsize")
	if err == nil {
		raw, err = c.probe(ctx, "vm_stat")
		if err == nil {
			var available uint64
			available, err = parseMemory(raw, total)
			if err == nil {
				b.Metric("system.memory.available", "gauge", float64(available), "bytes", nil)
				b.Metric("system.memory.used", "gauge", float64(total-available), "bytes", nil)
				b.Metric("system.memory.usage", "gauge", float64(total-available)/float64(total)*100, "percent", nil)
			}
		}
	}
	errs = append(errs, err)
	// The second top sample covers one second rather than the since-boot average.
	raw, err = c.probe(ctx, "top", "-l", "2", "-n", "0", "-s", "1")
	if err == nil {
		var usage float64
		usage, err = parseCPU(raw)
		if err == nil {
			b.Metric("system.cpu.usage", "gauge", usage, "percent", nil)
		}
	}
	errs = append(errs, err)
	return errors.Join(errs...)
}

var pageSizePattern = regexp.MustCompile(`page size of ([0-9]+) bytes`)
var cpuPattern = regexp.MustCompile(`CPU usage:.*?([0-9.]+)% idle`)

func parseCPU(out []byte) (float64, error) {
	matches := cpuPattern.FindAllSubmatch(out, -1)
	if len(matches) < 2 {
		return 0, errors.New("missing second CPU sample")
	}
	idle, err := strconv.ParseFloat(string(matches[len(matches)-1][1]), 64)
	if err != nil || idle < 0 || idle > 100 {
		return 0, errors.New("invalid CPU idle percentage")
	}
	return 100 - idle, nil
}
func parseMemory(out []byte, total uint64) (uint64, error) {
	m := pageSizePattern.FindSubmatch(out)
	if len(m) != 2 {
		return 0, errors.New("missing vm_stat page size")
	}
	size, err := strconv.ParseUint(string(m[1]), 10, 64)
	if err != nil || size == 0 {
		return 0, errors.New("invalid page size")
	}
	values := keyValueLines(out, ":")
	var pages uint64
	for _, key := range []string{"Pages free", "Pages inactive", "Pages speculative"} {
		v, err := strconv.ParseUint(strings.TrimSuffix(values[key], "."), 10, 64)
		if err != nil || v > total/size || pages > total/size-v {
			return 0, errors.New("invalid vm_stat memory counters")
		}
		pages += v
	}
	return pages * size, nil
}
func filesystemScope(scope monitoring.Scope, mount string) monitoring.Scope {
	scope.Resource.Type = "filesystem"
	scope.Resource.ID = entity.ID(entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions), entity.Key(mount, "root"))
	scope.Resource.Label = mount
	return scope
}
func (c Collector) filesystems(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope) error {
	out, err := c.probe(ctx, "df", "-k", "-P", "-l")
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		mount := strings.Join(f[5:], " ")
		if mount != "/" && mount != "/System/Volumes/Data" || seen[mount] {
			continue
		}
		var values [3]uint64
		for i := range values {
			v, e := strconv.ParseUint(f[i+1], 10, 64)
			if e != nil || v > math.MaxUint64/1024 {
				return errors.New("invalid df size")
			}
			values[i] = v * 1024
		}
		if values[0] == 0 || values[1] > values[0] || values[2] > values[0] {
			return errors.New("inconsistent df capacity")
		}
		seen[mount] = true
		dims := map[string]string{"mount": mount}
		fb := monitoring.NewBuilder(filesystemScope(scope, mount))
		fb.State("system.filesystem.source", f[0], dims)
		fb.Metric("system.filesystem.total", "gauge", float64(values[0]), "bytes", dims)
		fb.Metric("system.filesystem.used", "gauge", float64(values[1]), "bytes", dims)
		fb.Metric("system.filesystem.available", "gauge", float64(values[2]), "bytes", dims)
		// Match Linux: percentage of total, not df's used/(used+available).
		fb.Metric("system.filesystem.usage", "gauge", float64(values[1])/float64(values[0])*100, "percent", dims)
		b.Merge(fb.Batch())
	}
	if !seen["/"] {
		return errors.New("system volume absent from df")
	}
	return nil
}

var internalDiskPattern = regexp.MustCompile(`(?m)^/dev/(disk[0-9]+) \(internal, physical\):`)

func (c Collector) disk(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope) error {
	out, err := c.probe(ctx, "diskutil", "list", "internal", "physical")
	if err != nil {
		b.State("system.disk.smart.available", false, nil)
		return optional(err)
	}
	disks := internalDiskPattern.FindAllSubmatch(out, 9)
	b.State("system.disk.smart.available", len(disks) > 0, nil)
	if len(disks) > 8 {
		return errors.New("too many internal disks")
	}
	var errs []error
	for _, d := range disks {
		device := "/dev/" + string(d[1])
		ds := scope
		ds.Resource.Type = "disk"
		ds.Resource.ID = entity.ID(entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions), entity.Key("", device))
		ds.Resource.Label = device
		db := monitoring.NewBuilder(ds)
		dims := map[string]string{"device": device}
		out, err := c.probe(ctx, "diskutil", "info", device)
		status := keyValueLines(out, ":")["SMART Status"]
		known := err == nil && (status == "Verified" || status == "Failing")
		db.State("system.disk.smart.health_available", known, dims)
		if known {
			db.State("system.disk.smart.status", status, dims)
			db.State("system.disk.smart.healthy", status == "Verified", dims)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("diskutil info: %w", err))
		}
		b.Merge(db.Batch())
	}
	return errors.Join(errs...)
}
