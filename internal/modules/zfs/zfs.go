package zfs

import (
	"bufio"
	"bytes"
	"context"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/entity"
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
		collectPools(ctx, b, scope, zpoolPath, timeout)
		collectPoolStatus(ctx, b, scope, zpoolPath, timeout)
	}
	if zfsErr == nil {
		collectDatasets(ctx, b, scope, zfsPath, timeout)
		collectSnapshots(ctx, b, scope, zfsPath, timeout)
	}
	return b.Batch(), nil
}

func collectPools(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "list", "-Hp", "-o", "name,size,alloc,free,capacity,fragmentation,health").Output()
	cancel()
	if err != nil {
		b.EventDetails("system.zfs.pools.failed", map[string]string{"operation": "pool_list"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	pools := parsePoolList(out)
	b.Metric("system.zfs.pools", "gauge", float64(len(pools)), "", nil)
	for _, pool := range pools {
		dims := map[string]string{"pool": pool.Name}
		pb := monitoring.NewBuilder(poolScope(scope, pool.Name))
		pb.State("system.zfs.pool.present", true, dims)
		pb.State("system.zfs.pool.health", pool.Health, dims)
		pb.State("system.zfs.pool.healthy", pool.Health == "ONLINE", dims)
		pb.Metric("system.zfs.pool.size", "gauge", pool.Size, "bytes", dims)
		pb.Metric("system.zfs.pool.allocated", "gauge", pool.Allocated, "bytes", dims)
		pb.Metric("system.zfs.pool.free", "gauge", pool.Free, "bytes", dims)
		if pool.Capacity >= 0 {
			pb.Metric("system.zfs.pool.capacity", "gauge", pool.Capacity, "percent", dims)
		}
		if pool.Fragmentation >= 0 {
			pb.Metric("system.zfs.pool.fragmentation", "gauge", pool.Fragmentation, "percent", dims)
		}
		b.Merge(pb.Batch())
	}
}

func collectPoolStatus(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "status").Output()
	cancel()
	if err != nil {
		b.EventDetails("system.zfs.pool.status.failed", map[string]string{"operation": "pool_status"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	for _, scan := range parsePoolScans(out) {
		dims := map[string]string{"pool": scan.Pool}
		pb := monitoring.NewBuilder(poolScope(scope, scan.Pool))
		if scan.Status != "" {
			pb.State("system.zfs.pool.scan.status", scan.Status, dims)
		}
		if scan.CompletedAt != "" {
			pb.State("system.zfs.pool.scan.completed_at", scan.CompletedAt, dims)
		}
		if scan.Errors >= 0 {
			pb.Metric("system.zfs.pool.scan.errors", "gauge", float64(scan.Errors), "count", dims)
		}
		b.Merge(pb.Batch())
	}
}

func collectDatasets(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "list", "-Hp", "-t", "filesystem,volume", "-o", "name,type,used,avail,refer,compressratio,mountpoint").Output()
	cancel()
	if err != nil {
		b.EventDetails("system.zfs.datasets.failed", map[string]string{"operation": "dataset_list"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	datasets := parseDatasetList(out)
	b.Metric("system.zfs.datasets", "gauge", float64(len(datasets)), "", nil)
	for _, dataset := range datasets {
		dims := map[string]string{"dataset": dataset.Name}
		db := monitoring.NewBuilder(datasetScope(scope, dataset.Name))
		db.State("system.zfs.dataset.present", true, dims)
		db.State("system.zfs.dataset.type", dataset.Type, dims)
		if dataset.Mountpoint != "" && dataset.Mountpoint != "-" {
			db.State("system.zfs.dataset.mountpoint", dataset.Mountpoint, dims)
		}
		db.Metric("system.zfs.dataset.used", "gauge", dataset.Used, "bytes", dims)
		db.Metric("system.zfs.dataset.available", "gauge", dataset.Available, "bytes", dims)
		db.Metric("system.zfs.dataset.referenced", "gauge", dataset.Referenced, "bytes", dims)
		if dataset.CompressRatio >= 0 {
			db.Metric("system.zfs.dataset.compressratio", "gauge", dataset.CompressRatio, "ratio", dims)
		}
		b.Merge(db.Batch())
	}
}

func collectSnapshots(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "list", "-Hp", "-t", "snapshot", "-o", "name").Output()
	cancel()
	if err != nil {
		b.EventDetails("system.zfs.snapshots.failed", map[string]string{"operation": "snapshot_list"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	counts := parseSnapshotCounts(out)
	total := 0
	for dataset, count := range counts {
		total += count
		db := monitoring.NewBuilder(datasetScope(scope, dataset))
		db.Metric("system.zfs.dataset.snapshots", "gauge", float64(count), "", map[string]string{"dataset": dataset})
		b.Merge(db.Batch())
	}
	b.Metric("system.zfs.snapshots", "gauge", float64(total), "", nil)
}

func poolScope(scope monitoring.Scope, pool string) monitoring.Scope {
	scope.EntityType = "zfs-pool"
	scope.EntityID = entity.ID("zfs-pool", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), pool)
	scope.Label = pool
	return scope
}

func datasetScope(scope monitoring.Scope, dataset string) monitoring.Scope {
	scope.EntityType = "zfs-dataset"
	scope.EntityID = entity.ID("zfs-dataset", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), dataset)
	scope.Label = dataset
	return scope
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
