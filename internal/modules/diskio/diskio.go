package diskio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct{ ProcRoot, SysRoot string }

func (c Collector) Name() string { return "diskio" }

var wholeDevice = regexp.MustCompile(`^(sd[a-z]+|vd[a-z]+|xvd[a-z]+|hd[a-z]+|nvme[0-9]+n[0-9]+|mmcblk[0-9]+|md[0-9]+|dm-[0-9]+)$`)

func (c Collector) Collect(_ context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	proc, sys := c.ProcRoot, c.SysRoot
	if proc == "" {
		proc = "/proc"
	}
	if sys == "" {
		sys = "/sys"
	}
	b := monitoring.NewBuilder(scope)
	raw, err := os.ReadFile(filepath.Join(proc, "diskstats"))
	if err != nil {
		b.State("system.disk.io.available", false, nil)
		if errors.Is(err, os.ErrNotExist) {
			err = nil
		}
		return b.Batch(), err
	}
	b.State("system.disk.io.available", true, nil)
	var errs []error
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !wholeDevice.MatchString(f[2]) {
			continue
		}
		device := f[2]
		if _, err := os.Stat(filepath.Join(sys, "class/block", device, "partition")); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		if strings.HasPrefix(device, "dm-") {
			// kpartx partition mappers use partN-* UUIDs; keep ordinary LVM/crypt volumes.
			uuid, err := os.ReadFile(filepath.Join(sys, "class/block", device, "dm/uuid"))
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
				continue
			}
			if strings.HasPrefix(string(uuid), "part") {
				continue
			}
		}
		if len(f) < 14 {
			errs = append(errs, fmt.Errorf("diskstats %s: incomplete row", device))
			continue
		}
		values := make([]uint64, 11)
		valid := true
		for i := range values {
			v, err := strconv.ParseUint(f[i+3], 10, 64)
			if err != nil {
				valid = false
				break
			}
			values[i] = v
		}
		if !valid {
			errs = append(errs, fmt.Errorf("diskstats %s: invalid counter", device))
			continue
		}
		ds := scope
		// /dev paths match diskhealth's fallback identity. NVMe health can use a
		// controller serial; diskstats measures individual namespace/block devices.
		ds.Resource = pulse.ResourceRef{Type: "disk", ID: entity.ID(entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions), "/dev/"+device), Label: device}
		db := monitoring.NewBuilder(ds)
		for _, metric := range []struct {
			name, unit string
			index      int
			scale      float64
		}{
			{"read_bytes", "bytes", 2, 512}, {"write_bytes", "bytes", 6, 512}, {"reads", "", 0, 1}, {"writes", "", 4, 1},
			{"busy_time", "seconds", 9, .001}, {"read_time", "seconds", 3, .001}, {"write_time", "seconds", 7, .001},
		} {
			db.Metric("system.disk.io."+metric.name, "counter", float64(values[metric.index])*metric.scale, metric.unit, nil)
		}
		b.Merge(db.Batch())
	}
	return b.Batch(), errors.Join(errs...)
}
