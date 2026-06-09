package system

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	ProcRoot      string
	CPUSampleTime time.Duration
}

func (c Collector) Name() string { return "system" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	proc := c.ProcRoot
	if proc == "" {
		proc = "/proc"
	}
	window := c.CPUSampleTime
	if window <= 0 {
		window = 250 * time.Millisecond
	}
	b := monitoring.NewBuilder(scope)
	var errs []error

	if load, err := readLoadAvg(proc); err != nil {
		errs = append(errs, fmt.Errorf("loadavg: %w", err))
	} else {
		b.Metric("system.load.1m", "gauge", load[0], "load", nil)
		b.Metric("system.load.5m", "gauge", load[1], "load", nil)
		b.Metric("system.load.15m", "gauge", load[2], "load", nil)
	}
	if uptime, err := readUptime(proc); err != nil {
		errs = append(errs, fmt.Errorf("uptime: %w", err))
	} else {
		b.Metric("system.uptime", "gauge", uptime, "seconds", nil)
	}
	if mem, err := readMemInfo(proc); err != nil {
		errs = append(errs, fmt.Errorf("meminfo: %w", err))
	} else {
		used := mem.total - mem.available
		b.Metric("system.memory.total", "gauge", float64(mem.total), "bytes", nil)
		b.Metric("system.memory.available", "gauge", float64(mem.available), "bytes", nil)
		b.Metric("system.memory.used", "gauge", float64(used), "bytes", nil)
		if mem.total > 0 {
			b.Metric("system.memory.usage", "gauge", (float64(used)/float64(mem.total))*100, "percent", nil)
		}
		if mem.swapTotal > 0 {
			swapUsed := mem.swapTotal - mem.swapFree
			b.Metric("system.swap.used", "gauge", float64(swapUsed), "bytes", nil)
			b.Metric("system.swap.usage", "gauge", (float64(swapUsed)/float64(mem.swapTotal))*100, "percent", nil)
		}
	}
	if usage, err := sampleCPUUsage(ctx, proc, window); err != nil {
		errs = append(errs, fmt.Errorf("cpu: %w", err))
	} else {
		b.Metric("system.cpu.usage", "gauge", usage, "percent", nil)
	}
	return b.Batch(), errors.Join(errs...)
}

func readLoadAvg(procRoot string) ([3]float64, error) {
	var out [3]float64
	raw, err := os.ReadFile(filepath.Join(procRoot, "loadavg"))
	if err != nil {
		return out, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 3 {
		return out, errors.New("expected at least 3 load fields")
	}
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return out, err
		}
		out[i] = v
	}
	return out, nil
}

func readUptime(procRoot string) (float64, error) {
	raw, err := os.ReadFile(filepath.Join(procRoot, "uptime"))
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, errors.New("missing uptime field")
	}
	return strconv.ParseFloat(fields[0], 64)
}

type memInfo struct {
	total     uint64
	available uint64
	swapTotal uint64
	swapFree  uint64
}

func readMemInfo(procRoot string) (memInfo, error) {
	f, err := os.Open(filepath.Join(procRoot, "meminfo"))
	if err != nil {
		return memInfo{}, err
	}
	defer f.Close()

	var out memInfo
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			out.total = kb * 1024
		case "MemAvailable":
			out.available = kb * 1024
		case "SwapTotal":
			out.swapTotal = kb * 1024
		case "SwapFree":
			out.swapFree = kb * 1024
		}
	}
	if err := sc.Err(); err != nil {
		return memInfo{}, err
	}
	if out.total == 0 || out.available == 0 {
		return memInfo{}, errors.New("MemTotal or MemAvailable missing")
	}
	return out, nil
}

type cpuSample struct {
	idle  uint64
	total uint64
}

func sampleCPUUsage(ctx context.Context, procRoot string, window time.Duration) (float64, error) {
	first, err := readCPUSample(procRoot)
	if err != nil {
		return 0, err
	}
	timer := time.NewTimer(window)
	select {
	case <-ctx.Done():
		timer.Stop()
		return 0, ctx.Err()
	case <-timer.C:
	}
	second, err := readCPUSample(procRoot)
	if err != nil {
		return 0, err
	}
	total := second.total - first.total
	if total == 0 {
		return 0, errors.New("zero cpu delta")
	}
	idle := second.idle - first.idle
	return (1 - float64(idle)/float64(total)) * 100, nil
}

func readCPUSample(procRoot string) (cpuSample, error) {
	raw, err := os.ReadFile(filepath.Join(procRoot, "stat"))
	if err != nil {
		return cpuSample{}, err
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[0] != "cpu" {
			continue
		}
		vals := make([]uint64, 0, len(fields)-1)
		for _, field := range fields[1:] {
			v, err := strconv.ParseUint(field, 10, 64)
			if err != nil {
				return cpuSample{}, err
			}
			vals = append(vals, v)
		}
		var total uint64
		for _, v := range vals {
			total += v
		}
		idle := vals[3]
		if len(vals) > 4 {
			idle += vals[4]
		}
		return cpuSample{idle: idle, total: total}, nil
	}
	return cpuSample{}, errors.New("aggregate cpu row missing")
}
