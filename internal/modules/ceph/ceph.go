package ceph

import (
	"context"
	"encoding/json"
	"os/exec"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
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
	b.State("system.ceph.available", true, nil)
	collectStatus(ctx, b, path, timeout)
	collectDF(ctx, b, path, timeout)
	return b.Batch(), nil
}

func collectStatus(ctx context.Context, b *monitoring.Builder, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "status", "--format", "json").Output()
	cancel()
	if err != nil {
		b.Event("system.ceph.status.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	status, err := parseStatus(out)
	if err != nil {
		b.Event("system.ceph.status.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	if status.Health != "" {
		b.State("system.ceph.health.status", status.Health, nil)
		b.State("system.ceph.health.healthy", status.Health == "HEALTH_OK", nil)
	}
	b.Metric("system.ceph.osds.total", "gauge", float64(status.OSDsTotal), "count", nil)
	b.Metric("system.ceph.osds.up", "gauge", float64(status.OSDsUp), "count", nil)
	b.Metric("system.ceph.osds.in", "gauge", float64(status.OSDsIn), "count", nil)
	b.Metric("system.ceph.pgs.total", "gauge", float64(status.PGsTotal), "count", nil)
	b.Metric("system.ceph.bytes.used", "gauge", float64(status.BytesUsed), "bytes", nil)
	b.Metric("system.ceph.bytes.total", "gauge", float64(status.BytesTotal), "bytes", nil)
	b.Metric("system.ceph.bytes.available", "gauge", float64(status.BytesAvailable), "bytes", nil)
	if len(status.QuorumMonitors) > 0 {
		b.Metric("system.ceph.mons.quorum", "gauge", float64(len(status.QuorumMonitors)), "count", nil)
		for _, monitor := range status.QuorumMonitors {
			b.State("system.ceph.mon.in_quorum", true, map[string]string{"monitor": monitor})
		}
	}
	for state, count := range status.PGsByState {
		b.Metric("system.ceph.pgs.by_state", "gauge", float64(count), "count", map[string]string{"state": state})
	}
}

func collectDF(ctx context.Context, b *monitoring.Builder, path string, timeout time.Duration) {
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	out, err := exec.CommandContext(runCtx, path, "df", "--format", "json").Output()
	cancel()
	if err != nil {
		b.Event("system.ceph.df.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	pools, err := parsePools(out)
	if err != nil {
		b.Event("system.ceph.df.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	b.Metric("system.ceph.pools", "gauge", float64(len(pools)), "count", nil)
	for _, pool := range pools {
		dims := map[string]string{"pool": pool.Name}
		b.State("system.ceph.pool.present", true, dims)
		b.Metric("system.ceph.pool.bytes.used", "gauge", float64(pool.BytesUsed), "bytes", dims)
		b.Metric("system.ceph.pool.bytes.available", "gauge", float64(pool.BytesAvailable), "bytes", dims)
		b.Metric("system.ceph.pool.objects", "gauge", float64(pool.Objects), "count", dims)
	}
}

type Status struct {
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
