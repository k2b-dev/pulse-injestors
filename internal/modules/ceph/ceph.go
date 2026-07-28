package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Collector struct {
	Timeout time.Duration
}

func (c Collector) Name() string { return "ceph" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	b := monitoring.NewBuilder(scope)
	path, err := exec.LookPath("ceph")
	if err != nil {
		b.State("system.ceph.available", false, nil)
		return b.Batch(), nil
	}
	status, unavailable, err := readStatus(ctx, path, timeout)
	if err != nil {
		b.State("system.ceph.available", false, nil)
		if !unavailable {
			b.EventDetails("system.ceph.status.failed", map[string]string{"operation": "status"}, monitoring.EventDetails{
				Attributes: map[string]any{"error": err.Error()},
			})
		}
		return b.Batch(), nil
	}
	b.State("system.ceph.available", true, nil)
	clusterScope := cephClusterScope(scope, status.FSID)
	cb := monitoring.NewBuilder(clusterScope)
	cb.State("system.ceph.present", true, nil)
	if status.FSID != "" {
		cb.State("system.ceph.fsid", status.FSID, nil)
	}
	emitStatus(cb, status)
	collectDF(ctx, cb, clusterScope, path, timeout)
	b.Merge(cb.Batch())
	return b.Batch(), nil
}

func readStatus(ctx context.Context, path string, timeout time.Duration) (Status, bool, error) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "status", "--format", "json").CombinedOutput()
	cancel()
	if err != nil {
		return Status{}, isCephUnavailable(out, err), err
	}
	status, err := parseStatus(out)
	if err != nil {
		return Status{}, false, err
	}
	return status, false, nil
}

func emitStatus(b *monitoring.Builder, status Status) {
	if status.Health != "" {
		b.State("system.ceph.health.status", status.Health, nil)
		b.State("system.ceph.health.healthy", status.Health == "HEALTH_OK", nil)
	}
	b.Metric("system.ceph.osds.total", "gauge", float64(status.OSDsTotal), "", nil)
	b.Metric("system.ceph.osds.up", "gauge", float64(status.OSDsUp), "", nil)
	b.Metric("system.ceph.osds.in", "gauge", float64(status.OSDsIn), "", nil)
	b.Metric("system.ceph.pgs.total", "gauge", float64(status.PGsTotal), "", nil)
	b.Metric("system.ceph.bytes.used", "gauge", float64(status.BytesUsed), "bytes", nil)
	b.Metric("system.ceph.bytes.total", "gauge", float64(status.BytesTotal), "bytes", nil)
	b.Metric("system.ceph.bytes.available", "gauge", float64(status.BytesAvailable), "bytes", nil)
	if len(status.QuorumMonitors) > 0 {
		b.Metric("system.ceph.mons.quorum", "gauge", float64(len(status.QuorumMonitors)), "", nil)
		for _, monitor := range status.QuorumMonitors {
			b.State("system.ceph.mon.in_quorum", true, map[string]string{"monitor": monitor})
		}
	}
	for state, count := range status.PGsByState {
		b.Metric("system.ceph.pgs.by_state", "gauge", float64(count), "", map[string]string{"state": state})
	}
}

func collectDF(ctx context.Context, b *monitoring.Builder, scope monitoring.Scope, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "df", "--format", "json").CombinedOutput()
	cancel()
	if err != nil {
		if isCephUnavailable(out, err) {
			return
		}
		b.EventDetails("system.ceph.df.failed", map[string]string{"operation": "df"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	pools, err := parsePools(out)
	if err != nil {
		b.EventDetails("system.ceph.df.failed", map[string]string{"operation": "parse_df"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return
	}
	b.Metric("system.ceph.pools", "gauge", float64(len(pools)), "", nil)
	for _, pool := range pools {
		dims := map[string]string{"pool": pool.Name}
		pb := monitoring.NewBuilder(cephPoolScope(scope, pool.Name))
		pb.State("system.ceph.pool.present", true, dims)
		pb.Metric("system.ceph.pool.bytes.used", "gauge", float64(pool.BytesUsed), "bytes", dims)
		pb.Metric("system.ceph.pool.bytes.available", "gauge", float64(pool.BytesAvailable), "bytes", dims)
		pb.Metric("system.ceph.pool.objects", "gauge", float64(pool.Objects), "count", dims)
		b.Merge(pb.Batch())
	}
}

func cephClusterScope(scope monitoring.Scope, fsid string) monitoring.Scope {
	scope.EntityType = "ceph-cluster"
	host := entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions)
	key := entity.Key(fsid, host)
	scope.EntityID = entity.ID("ceph-cluster", key)
	scope.Label = "Ceph " + key
	return scope
}

func cephPoolScope(scope monitoring.Scope, pool string) monitoring.Scope {
	scope.EntityType = "ceph-pool"
	cluster := strings.TrimPrefix(scope.EntityID, "ceph-cluster:")
	scope.EntityID = entity.ID("ceph-pool", entity.Key(cluster, entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions)), pool)
	scope.Label = pool
	return scope
}

func isCephUnavailable(out []byte, err error) bool {
	text := strings.ToLower(strings.TrimSpace(string(out) + " " + fmt.Sprint(err)))
	if text == "" {
		return false
	}
	for _, marker := range []string{
		"ceph.conf",
		"cluster not found",
		"no such file or directory",
		"no monitors specified",
		"not configured",
		"objectnotfound",
		"rados object not found",
		"unable to get monitor info",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

type Status struct {
	FSID           string
	Health         string
	QuorumMonitors []string
	OSDsTotal      int
	OSDsUp         int
	OSDsIn         int
	PGsTotal       int
	PGsByState     map[string]int
	BytesUsed      uint64
	BytesTotal     uint64
	BytesAvailable uint64
}

func parseStatus(out []byte) (Status, error) {
	var raw struct {
		FSID   string `json:"fsid"`
		Health struct {
			Status string `json:"status"`
		} `json:"health"`
		QuorumNames []string `json:"quorum_names"`
		OSDMap      struct {
			OSDMap struct {
				NumOSDs   int `json:"num_osds"`
				NumUpOSDs int `json:"num_up_osds"`
				NumInOSDs int `json:"num_in_osds"`
			} `json:"osdmap"`
		} `json:"osdmap"`
		PGMap struct {
			NumPGs     int    `json:"num_pgs"`
			BytesUsed  uint64 `json:"bytes_used"`
			BytesTotal uint64 `json:"bytes_total"`
			BytesAvail uint64 `json:"bytes_avail"`
			PGsByState []struct {
				StateName string `json:"state_name"`
				Count     int    `json:"count"`
			} `json:"pgs_by_state"`
		} `json:"pgmap"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Status{}, err
	}
	status := Status{
		FSID:           raw.FSID,
		Health:         raw.Health.Status,
		QuorumMonitors: raw.QuorumNames,
		OSDsTotal:      raw.OSDMap.OSDMap.NumOSDs,
		OSDsUp:         raw.OSDMap.OSDMap.NumUpOSDs,
		OSDsIn:         raw.OSDMap.OSDMap.NumInOSDs,
		PGsTotal:       raw.PGMap.NumPGs,
		PGsByState:     map[string]int{},
		BytesUsed:      raw.PGMap.BytesUsed,
		BytesTotal:     raw.PGMap.BytesTotal,
		BytesAvailable: raw.PGMap.BytesAvail,
	}
	for _, row := range raw.PGMap.PGsByState {
		if row.StateName != "" {
			status.PGsByState[row.StateName] += row.Count
		}
	}
	return status, nil
}

type Pool struct {
	Name           string
	BytesUsed      uint64
	BytesAvailable uint64
	Objects        uint64
}

func parsePools(out []byte) ([]Pool, error) {
	var raw struct {
		Pools []struct {
			Name  string `json:"name"`
			Stats struct {
				BytesUsed uint64 `json:"bytes_used"`
				MaxAvail  uint64 `json:"max_avail"`
				Objects   uint64 `json:"objects"`
			} `json:"stats"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	pools := make([]Pool, 0, len(raw.Pools))
	for _, pool := range raw.Pools {
		if pool.Name == "" {
			continue
		}
		pools = append(pools, Pool{
			Name:           pool.Name,
			BytesUsed:      pool.Stats.BytesUsed,
			BytesAvailable: pool.Stats.MaxAvail,
			Objects:        pool.Stats.Objects,
		})
	}
	return pools, nil
}
