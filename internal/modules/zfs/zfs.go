package zfs

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	Timeout time.Duration
}

func (c Collector) Name() string { return "zfs" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	b := monitoring.NewBuilder(scope)

	zpoolPath, zpoolErr := exec.LookPath("zpool")
	zfsPath, zfsErr := exec.LookPath("zfs")
	b.State("system.zfs.zpool.available", zpoolErr == nil, nil)
	b.State("system.zfs.zfs.available", zfsErr == nil, nil)
	b.State("system.zfs.available", zpoolErr == nil || zfsErr == nil, nil)

	if zpoolErr == nil {
		collectPools(ctx, b, zpoolPath, timeout)
		collectPoolStatus(ctx, b, zpoolPath, timeout)
	}
	if zfsErr == nil {
		collectDatasets(ctx, b, zfsPath, timeout)
		collectSnapshots(ctx, b, zfsPath, timeout)
	}
	return b.Batch(), nil
}

func collectPools(ctx context.Context, b *monitoring.Builder, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "list", "-Hp", "-o", "name,size,alloc,free,capacity,fragmentation,health").Output()
	cancel()
	if err != nil {
		b.Event("system.zfs.pools.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	pools := parsePoolList(out)
	b.Metric("system.zfs.pools", "gauge", float64(len(pools)), "count", nil)
	for _, pool := range pools {
		dims := map[string]string{"pool": pool.Name}
		b.State("system.zfs.pool.present", true, dims)
		b.State("system.zfs.pool.health", pool.Health, dims)
		b.State("system.zfs.pool.healthy", pool.Health == "ONLINE", dims)
		b.Metric("system.zfs.pool.size", "gauge", pool.Size, "bytes", dims)
		b.Metric("system.zfs.pool.allocated", "gauge", pool.Allocated, "bytes", dims)
		b.Metric("system.zfs.pool.free", "gauge", pool.Free, "bytes", dims)
		if pool.Capacity >= 0 {
			b.Metric("system.zfs.pool.capacity", "gauge", pool.Capacity, "percent", dims)
		}
		if pool.Fragmentation >= 0 {
			b.Metric("system.zfs.pool.fragmentation", "gauge", pool.Fragmentation, "percent", dims)
		}
	}
}

func collectPoolStatus(ctx context.Context, b *monitoring.Builder, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "status").Output()
	cancel()
	if err != nil {
		b.Event("system.zfs.pool.status.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	for _, scan := range parsePoolScans(out) {
		dims := map[string]string{"pool": scan.Pool}
		if scan.Status != "" {
			b.State("system.zfs.pool.scan.status", scan.Status, dims)
		}
		if scan.CompletedAt != "" {
			b.State("system.zfs.pool.scan.completed_at", scan.CompletedAt, dims)
		}
		if scan.Errors >= 0 {
			b.Metric("system.zfs.pool.scan.errors", "gauge", float64(scan.Errors), "count", dims)
		}
	}
}

func collectDatasets(ctx context.Context, b *monitoring.Builder, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "list", "-Hp", "-t", "filesystem,volume", "-o", "name,type,used,avail,refer,compressratio,mountpoint").Output()
	cancel()
	if err != nil {
		b.Event("system.zfs.datasets.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	datasets := parseDatasetList(out)
	b.Metric("system.zfs.datasets", "gauge", float64(len(datasets)), "count", nil)
	for _, dataset := range datasets {
		dims := map[string]string{"dataset": dataset.Name, "type": dataset.Type}
		b.State("system.zfs.dataset.present", true, dims)
		if dataset.Mountpoint != "" && dataset.Mountpoint != "-" {
			b.State("system.zfs.dataset.mountpoint", dataset.Mountpoint, dims)
		}
		b.Metric("system.zfs.dataset.used", "gauge", dataset.Used, "bytes", dims)
		b.Metric("system.zfs.dataset.available", "gauge", dataset.Available, "bytes", dims)
		b.Metric("system.zfs.dataset.referenced", "gauge", dataset.Referenced, "bytes", dims)
		if dataset.CompressRatio >= 0 {
			b.Metric("system.zfs.dataset.compressratio", "gauge", dataset.CompressRatio, "ratio", dims)
		}
	}
}

func collectSnapshots(ctx context.Context, b *monitoring.Builder, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "list", "-Hp", "-t", "snapshot", "-o", "name").Output()
	cancel()
	if err != nil {
		b.Event("system.zfs.snapshots.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	counts := parseSnapshotCounts(out)
	total := 0
	for dataset, count := range counts {
		total += count
		b.Metric("system.zfs.dataset.snapshots", "gauge", float64(count), "count", map[string]string{"dataset": dataset})
	}
	b.Metric("system.zfs.snapshots", "gauge", float64(total), "count", nil)
}

type Pool struct {
	Name          string
	Size          float64
	Allocated     float64
	Free          float64
	Capacity      float64
	Fragmentation float64
	Health        string
}

func parsePoolList(out []byte) []Pool {
	var pools []Pool
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := splitRecord(sc.Text())
		if len(fields) < 7 {
			continue
		}
		pools = append(pools, Pool{
			Name:          fields[0],
			Size:          parseFloat(fields[1]),
			Allocated:     parseFloat(fields[2]),
			Free:          parseFloat(fields[3]),
			Capacity:      parsePercent(fields[4]),
			Fragmentation: parsePercent(fields[5]),
			Health:        strings.ToUpper(fields[6]),
		})
	}
	return pools
}

type Dataset struct {
	Name          string
	Type          string
	Used          float64
	Available     float64
	Referenced    float64
	CompressRatio float64
	Mountpoint    string
}

func parseDatasetList(out []byte) []Dataset {
	var datasets []Dataset
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := splitRecord(sc.Text())
		if len(fields) < 7 {
			continue
		}
		datasets = append(datasets, Dataset{
			Name:          fields[0],
			Type:          fields[1],
			Used:          parseFloat(fields[2]),
			Available:     parseFloat(fields[3]),
			Referenced:    parseFloat(fields[4]),
			CompressRatio: parseRatio(fields[5]),
			Mountpoint:    fields[6],
		})
	}
	return datasets
}

func parseSnapshotCounts(out []byte) map[string]int {
	counts := map[string]int{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		name := strings.TrimSpace(sc.Text())
		if name == "" {
			continue
		}
		dataset, _, ok := strings.Cut(name, "@")
		if !ok || dataset == "" {
			continue
		}
		counts[dataset]++
	}
	return counts
}

type PoolScan struct {
	Pool        string
	Status      string
	CompletedAt string
	Errors      int
}

func parsePoolScans(out []byte) []PoolScan {
	var scans []PoolScan
	currentPool := ""
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "pool:") {
			currentPool = strings.TrimSpace(strings.TrimPrefix(line, "pool:"))
			continue
		}
		if currentPool == "" || !strings.HasPrefix(line, "scan:") {
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(line, "scan:"))
		scan := PoolScan{Pool: currentPool, Errors: -1}
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) > 0 {
			scan.Status = strings.ToLower(fields[0])
		}
		if idx := strings.LastIndex(text, " on "); idx >= 0 {
			scan.CompletedAt = strings.TrimSpace(text[idx+4:])
		}
		if idx := strings.Index(text, " with "); idx >= 0 {
			rest := strings.TrimSpace(text[idx+6:])
			parts := strings.Fields(rest)
			if len(parts) >= 2 && parts[1] == "errors" {
				if value, err := strconv.Atoi(parts[0]); err == nil {
					scan.Errors = value
				}
			}
		}
		scans = append(scans, scan)
	}
	return scans
}

func splitRecord(line string) []string {
	if strings.Contains(line, "\t") {
		return strings.Split(strings.TrimSpace(line), "\t")
	}
	return strings.Fields(line)
}

func parseFloat(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "-" || value == "" {
		return 0
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func parsePercent(value string) float64 {
	value = strings.TrimSuffix(strings.TrimSpace(value), "%")
	if value == "-" || value == "" {
		return -1
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return -1
	}
	return parsed
}

func parseRatio(value string) float64 {
	value = strings.TrimSuffix(strings.TrimSpace(value), "x")
	if value == "-" || value == "" {
		return -1
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return -1
	}
	return parsed
}
