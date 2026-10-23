package clickhouse

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
)

type Collector struct {
	Config config.ClickHouseConfig
	Group  string
}

func (c Collector) Name() string {
	if c.Group == "" || c.Group == "health" {
		return "clickhouse"
	}
	return "clickhouse-" + c.Group
}

const HealthSQL = "SELECT 1 AS ok FORMAT JSON"
const VersionSQL = "SELECT version() AS version FORMAT JSON"
const CapacitySQL = `SELECT 'disk.free' AS signal, name AS disk, toFloat64(free_space) AS value FROM system.disks
UNION ALL SELECT 'disk.total', name, toFloat64(total_space) FROM system.disks
UNION ALL SELECT 'queries.running', '', toFloat64(value) FROM system.metrics WHERE metric = 'Query'
UNION ALL SELECT 'parts.max_per_partition', '', toFloat64(ifNull(max(n), 0)) FROM (SELECT count() AS n FROM system.parts WHERE active GROUP BY database, table, partition_id)
UNION ALL SELECT 'merges.running', '', toFloat64(count()) FROM system.merges
UNION ALL SELECT 'errors', '', toFloat64(sum(value)) FROM system.errors
UNION ALL SELECT 'uptime', '', toFloat64(uptime()) FORMAT JSON`

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceprobe.Timeout)
	defer cancel()
	b := monitoring.NewBuilder(scope)
	group := c.Group
	if group == "" {
		group = "health"
	}
	e := c.collect(ctx, b, group)
	return serviceprobe.Finish(b, "clickhouse", group, e)
}
func (c Collector) collect(ctx context.Context, b *monitoring.Builder, group string) error {
	h := c.Config
	user := h.User
	if user == "" {
		user = "default"
	}
	client, e := (serviceprobe.HTTP{URL: h.URL, CAFile: h.CAFile, ServerName: h.ServerName, Username: user, PasswordFile: h.PasswordFile}).Open()
	if e != nil {
		return e
	}
	defer client.Close()
	sql := HealthSQL
	if group == "capacity" {
		sql = CapacitySQL
	}
	if group == "version" {
		sql = VersionSQL
	}
	raw, status, e := client.Request(ctx, http.MethodPost, "/", sql, true)
	if e != nil {
		return e
	}
	if status != 200 {
		return serviceprobe.ErrResponse
	}
	var response struct{ Data []json.RawMessage }
	if e = serviceprobe.JSON(raw, &response); e != nil || len(response.Data) == 0 {
		return serviceprobe.ErrResponse
	}
	if group == "health" {
		var row struct{ OK *int }
		if len(response.Data) != 1 || serviceprobe.JSON(response.Data[0], &row) != nil || row.OK == nil || *row.OK != 1 {
			return serviceprobe.ErrResponse
		}
		return nil
	}
	if group == "version" {
		var row struct{ Version string }
		if len(response.Data) != 1 || serviceprobe.JSON(response.Data[0], &row) != nil || row.Version == "" {
			return serviceprobe.ErrResponse
		}
		b.State("clickhouse.version", client.Text(row.Version), nil)
		return nil
	}
	if len(response.Data) > 205 {
		return serviceprobe.ErrResponse
	}
	seen := map[string]bool{}
	disks := map[string]bool{}
	for _, raw := range response.Data {
		var row struct {
			Signal, Disk string
			Value        json.RawMessage
		}
		if serviceprobe.JSON(raw, &row) != nil {
			return serviceprobe.ErrResponse
		}
		// FORMAT JSON may quote 64-bit numeric values; accept either representation.
		number := string(row.Value)
		if len(number) > 0 && number[0] == '"' {
			if json.Unmarshal(row.Value, &number) != nil {
				return serviceprobe.ErrResponse
			}
		}
		value, e := serviceprobe.Number(number)
		if e != nil {
			return e
		}
		key := row.Signal + "\x00" + row.Disk
		if seen[key] {
			return serviceprobe.ErrResponse
		}
		seen[key] = true
		if row.Signal == "disk.free" || row.Signal == "disk.total" {
			disks[row.Disk] = true
		} else if row.Disk != "" {
			return serviceprobe.ErrResponse
		}
		dims := map[string]string{"disk": client.Text(row.Disk)}
		switch row.Signal {
		case "disk.free":
			if row.Disk == "" {
				return serviceprobe.ErrResponse
			}
			b.Metric("clickhouse.disk.free", "gauge", value, "bytes", dims)
		case "disk.total":
			if row.Disk == "" {
				return serviceprobe.ErrResponse
			}
			b.Metric("clickhouse.disk.total", "gauge", value, "bytes", dims)
		case "queries.running":
			b.Metric("clickhouse.queries.running", "gauge", value, "", nil)
		case "parts.max_per_partition":
			b.Metric("clickhouse.parts.max_per_partition", "gauge", value, "", nil)
		case "merges.running":
			b.Metric("clickhouse.merges.running", "gauge", value, "", nil)
		case "errors":
			b.Metric("clickhouse.errors", "counter", value, "", nil)
		case "uptime":
			b.Metric("clickhouse.uptime", "gauge", value, "seconds", nil)
		default:
			return serviceprobe.ErrResponse
		}
	}
	if len(disks) > 100 {
		return serviceprobe.ErrResponse
	}
	for disk := range disks {
		if !seen["disk.free\x00"+disk] || !seen["disk.total\x00"+disk] {
			return serviceprobe.ErrResponse
		}
	}
	for _, name := range []string{"queries.running", "parts.max_per_partition", "merges.running", "errors", "uptime"} {
		if !seen[name+"\x00"] {
			return serviceprobe.ErrResponse
		}
	}
	return nil
}
