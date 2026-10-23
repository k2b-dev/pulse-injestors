package nats

import (
	"context"
	"net/http"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
)

type Collector struct {
	Config config.NATSConfig
	Group  string
}

func (c Collector) Name() string {
	if c.Group == "" || c.Group == "health" {
		return "nats"
	}
	return "nats-" + c.Group
}
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceprobe.Timeout)
	defer cancel()
	b := monitoring.NewBuilder(scope)
	group := c.Group
	if group == "" {
		group = "health"
	}
	e := c.collect(ctx, b, group)
	return serviceprobe.Finish(b, "nats", group, e)
}
func (c Collector) collect(ctx context.Context, b *monitoring.Builder, group string) error {
	client, e := (serviceprobe.HTTP{URL: c.Config.MonitorURL}).Open()
	if e != nil {
		return e
	}
	defer client.Close()
	if group == "jetstream" {
		var j struct {
			Streams, Consumers float64
			Store              float64 `json:"storage"`
			ReservedStore      float64 `json:"reserved_storage"`
			API                *struct{ Errors float64 }
			Meta               *struct{ Leader string } `json:"meta_cluster"`
		}
		if e = client.JSON(ctx, "/jsz", &j); e != nil {
			return e
		}
		if j.API == nil {
			return serviceprobe.ErrResponse
		}
		for _, v := range []float64{j.Streams, j.Consumers, j.Store, j.ReservedStore, j.API.Errors} {
			if v < 0 {
				return serviceprobe.ErrResponse
			}
		}
		b.Metric("nats.jetstream.streams", "gauge", j.Streams, "", nil)
		b.Metric("nats.jetstream.consumers", "gauge", j.Consumers, "", nil)
		b.Metric("nats.jetstream.storage.used", "gauge", j.Store, "bytes", nil)
		b.Metric("nats.jetstream.storage.reserved", "gauge", j.ReservedStore, "bytes", nil)
		b.Metric("nats.jetstream.api.errors", "counter", j.API.Errors, "", nil)
		if j.Meta != nil {
			b.State("nats.jetstream.meta_leader", client.Text(j.Meta.Leader), nil)
		}
		return nil
	}
	if group == "health" {
		raw, status, e := client.Request(ctx, http.MethodGet, "/healthz", "", false)
		if e != nil {
			b.State("nats.healthy", false, nil)
			return e
		}
		var h struct{ Status, Error string }
		if e = serviceprobe.JSON(raw, &h); e != nil || h.Status == "" {
			b.State("nats.healthy", false, nil)
			return serviceprobe.ErrResponse
		}
		healthy := status == 200 && h.Status == "ok"
		b.State("nats.healthy", healthy, nil)
		message := ""
		if !healthy {
			message = h.Error
			if message == "" {
				message = "NATS health check failed"
			}
		}
		b.State("nats.health.error", client.Text(message), nil)
		if !healthy {
			return serviceprobe.ErrResponse
		}
	}
	var v struct {
		Version          string
		Connections, Mem *float64
		Slow             *float64 `json:"slow_consumers"`
		InMsgs           *float64 `json:"in_msgs"`
		OutMsgs          *float64 `json:"out_msgs"`
		InBytes          *float64 `json:"in_bytes"`
		OutBytes         *float64 `json:"out_bytes"`
	}
	if e = client.JSON(ctx, "/varz", &v); e != nil {
		return e
	}
	if group == "version" {
		if v.Version == "" {
			return serviceprobe.ErrResponse
		}
		b.State("nats.version", client.Text(v.Version), nil)
		return nil
	}
	for _, n := range []*float64{v.Connections, v.Mem, v.Slow, v.InMsgs, v.OutMsgs, v.InBytes, v.OutBytes} {
		if n == nil || *n < 0 {
			return serviceprobe.ErrResponse
		}
	}
	b.Metric("nats.connections", "gauge", *v.Connections, "", nil)
	b.Metric("nats.slow_consumers", "counter", *v.Slow, "", nil)
	b.Metric("nats.messages.in", "counter", *v.InMsgs, "", nil)
	b.Metric("nats.messages.out", "counter", *v.OutMsgs, "", nil)
	b.Metric("nats.bytes.in", "counter", *v.InBytes, "bytes", nil)
	b.Metric("nats.bytes.out", "counter", *v.OutBytes, "bytes", nil)
	b.Metric("nats.memory", "gauge", *v.Mem, "bytes", nil)
	var routes struct {
		Num *float64 `json:"num_routes"`
	}
	if e = client.JSON(ctx, "/routez", &routes); e != nil {
		return e
	}
	if routes.Num == nil || *routes.Num < 0 {
		return serviceprobe.ErrResponse
	}
	b.Metric("nats.routes", "gauge", *routes.Num, "", nil)
	return nil
}
