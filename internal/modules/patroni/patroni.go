package patroni

import (
	"context"
	"encoding/json"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
)

type Collector struct{ Config config.PatroniConfig }

func (Collector) Name() string { return "patroni" }

type member struct {
	Name, Role, State string
	Lag               json.RawMessage
}
type status struct {
	Role, State string
	Timeline    *uint64
	Patroni     struct{ Name, Scope string }
	Xlog        struct {
		Received *uint64 `json:"received_location"`
		Replayed *uint64 `json:"replayed_location"`
	}
}

func leader(role string) bool {
	return role == "leader" || role == "master" || role == "primary" || role == "standby_leader"
}
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceprobe.Timeout)
	defer cancel()
	b := monitoring.NewBuilder(scope)
	err := c.collect(ctx, scope, b)
	return serviceprobe.Finish(b, "patroni", "health", err)
}
func (c Collector) collect(ctx context.Context, scope monitoring.Scope, b *monitoring.Builder) error {
	h := c.Config
	client, e := (serviceprobe.HTTP{URL: h.URL, CAFile: h.CAFile, ServerName: h.ServerName, Insecure: h.InsecureSkipVerify, Username: h.Username, PasswordFile: h.PasswordFile}).Open()
	if e != nil {
		return e
	}
	defer client.Close()
	var s status
	if e = client.JSON(ctx, "/patroni", &s); e != nil {
		return e
	}
	if s.Role == "" || s.State == "" {
		return serviceprobe.ErrResponse
	}
	role := s.Role
	if leader(role) {
		role = "leader"
	}
	b.State("postgres.role", client.Text(role), nil)
	b.State("postgres.state", client.Text(s.State), nil)
	if s.Timeline != nil {
		b.Metric("postgres.timeline", "gauge", float64(*s.Timeline), "", nil)
	}
	// /patroni exposes local receive/replay positions, not the leader's WAL tip.
	// A concurrent replay snapshot can be ahead of the receive snapshot.
	if !leader(s.Role) {
		if s.Xlog.Received != nil && s.Xlog.Replayed != nil {
			var lag uint64
			if *s.Xlog.Received > *s.Xlog.Replayed {
				lag = *s.Xlog.Received - *s.Xlog.Replayed
			}
			b.Metric("postgres.replication.replay_backlog", "gauge", float64(lag), "bytes", nil)
		}
	}
	var cluster struct{ Members *[]member }
	if e = client.JSON(ctx, "/cluster", &cluster); e != nil {
		return e
	}
	if cluster.Members == nil || len(*cluster.Members) > 1000 {
		return serviceprobe.ErrResponse
	}
	if !leader(s.Role) {
		if s.Patroni.Name == "" {
			return serviceprobe.ErrResponse
		}
		for _, m := range *cluster.Members {
			if m.Name != s.Patroni.Name {
				continue
			}
			if m.State != "" {
				b.State("postgres.replication.state", client.Text(m.State), nil)
			}
			known := len(m.Lag) > 0 && string(m.Lag) != `"unknown"` && string(m.Lag) != "null"
			if known {
				var lag uint64
				if json.Unmarshal(m.Lag, &lag) != nil {
					return serviceprobe.ErrResponse
				}
				b.Metric("postgres.replication.lag", "gauge", float64(lag), "bytes", nil)
			}
			b.State("postgres.replication.lag_known", known, nil)
			return nil
		}
		b.State("postgres.replication.lag_known", false, nil)
		return serviceprobe.ErrResponse
	}
	name := h.Cluster
	if name == "" {
		name = s.Patroni.Scope
	}
	if name == "" || client.Text(name) != name {
		return serviceprobe.ErrResponse
	}
	cs := scope
	cs.Resource = pulse.ResourceRef{Type: "postgres-cluster", ID: client.Text(name), Label: client.Text(name)}
	cb := monitoring.NewBuilder(cs)
	running, sync := 0, 0
	leaderName := ""
	for _, m := range *cluster.Members {
		if m.State == "running" || m.State == "streaming" {
			running++
		}
		if m.Role == "sync_standby" || m.Role == "quorum_standby" {
			sync++
		}
		if leader(m.Role) {
			leaderName = m.Name
		}
	}
	cb.Metric("postgres.cluster.members", "gauge", float64(len(*cluster.Members)), "", nil)
	cb.Metric("postgres.cluster.members_running", "gauge", float64(running), "", nil)
	cb.Metric("postgres.cluster.sync_standbys", "gauge", float64(sync), "", nil)
	if leaderName != "" {
		cb.State("postgres.cluster.leader", client.Text(leaderName), nil)
	}
	b.Merge(cb.Batch())
	return nil
}
