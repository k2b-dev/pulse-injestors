package postgres

import (
	"context"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
)

type Collector struct {
	Config  config.PostgresConfig
	Version bool
	Run     localexec.Runner
}

func (c Collector) Name() string {
	if c.Version {
		return "postgres-version"
	}
	return "postgres"
}

// JSON keeps database names containing separators/newlines unambiguous. These
// catalog/statistics reads also work during recovery on PostgreSQL 18.
const CapacitySQL = `BEGIN READ ONLY; SET LOCAL statement_timeout = '10s';
SELECT json_build_object(
 'connections', (SELECT json_object_agg(state, n) FROM (SELECT CASE WHEN state IN ('active','idle','idle in transaction') THEN state ELSE 'other' END AS state, count(*) AS n FROM pg_stat_activity WHERE backend_type = 'client backend' GROUP BY 1) c),
 'max_connections', current_setting('max_connections')::bigint,
 'databases', (SELECT coalesce(json_agg(d), '[]'::json) FROM (SELECT datname AS name, pg_database_size(oid) AS size FROM pg_database WHERE NOT datistemplate ORDER BY datname LIMIT 100) d),
 'xid_age', (SELECT max(age(datfrozenxid)) FROM pg_database),
 'oldest_age', (SELECT coalesce(max(greatest(0,extract(epoch FROM clock_timestamp()-xact_start))),0) FROM pg_stat_activity WHERE backend_type = 'client backend' AND state <> 'idle' AND xact_start IS NOT NULL AND pid <> pg_backend_pid()),
 'deadlocks', (SELECT coalesce(sum(deadlocks),0) FROM pg_stat_database)); COMMIT;`
const VersionSQL = `BEGIN READ ONLY; SET LOCAL statement_timeout = '10s'; SELECT to_json(current_setting('server_version')); COMMIT;`

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceprobe.Timeout)
	defer cancel()
	b := monitoring.NewBuilder(scope)
	group := "capacity"
	if c.Version {
		group = "version"
	}
	e := c.collect(ctx, b)
	return serviceprobe.Finish(b, "postgres", group, e)
}
func (c Collector) collect(ctx context.Context, b *monitoring.Builder) error {
	command := c.Config.Command
	if len(command) == 0 {
		command = []string{"runuser", "-u", "postgres", "--", "psql"}
	}
	db := c.Config.Database
	if db == "" {
		db = "postgres"
	}
	sql := CapacitySQL
	if c.Version {
		sql = VersionSQL
	}
	// -w forbids interactive password prompts; no shell or password argument.
	args := append(append([]string{}, command[1:]...), "-X", "-At", "-q", "-w", "-v", "ON_ERROR_STOP=1", "--dbname", db, "-c", sql)
	run := c.Run
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return localexec.Output(ctx, serviceprobe.MaxBytes, name, args...)
		}
	}
	raw, e := run(ctx, command[0], args...)
	if e != nil {
		return serviceprobe.ErrResponse
	}
	if c.Version {
		var v string
		if e = serviceprobe.JSON(raw, &v); e != nil || v == "" {
			return serviceprobe.ErrResponse
		}
		b.State("postgres.version", serviceprobe.Text(v), nil)
		return nil
	}
	var data struct {
		Connections map[string]float64 `json:"connections"`
		Max         *float64           `json:"max_connections"`
		Databases   *[]struct {
			Name string
			Size float64
		} `json:"databases"`
		XID       *float64 `json:"xid_age"`
		Oldest    *float64 `json:"oldest_age"`
		Deadlocks *float64 `json:"deadlocks"`
	}
	if e = serviceprobe.JSON(raw, &data); e != nil {
		return e
	}
	if data.Connections == nil || data.Databases == nil || data.Max == nil || data.XID == nil || data.Oldest == nil || data.Deadlocks == nil || len(*data.Databases) > 100 {
		return serviceprobe.ErrResponse
	}
	for _, state := range []string{"active", "idle", "idle in transaction", "other"} {
		v := data.Connections[state]
		if v < 0 {
			return serviceprobe.ErrResponse
		}
		b.Metric("postgres.connections", "gauge", v, "", map[string]string{"state": state})
	}
	for _, v := range []*float64{data.Max, data.XID, data.Oldest, data.Deadlocks} {
		if *v < 0 {
			return serviceprobe.ErrResponse
		}
	}
	b.Metric("postgres.connections.max", "gauge", *data.Max, "", nil)
	for _, db := range *data.Databases {
		if db.Name == "" || db.Size < 0 {
			return serviceprobe.ErrResponse
		}
		b.Metric("postgres.database.size", "gauge", db.Size, "bytes", map[string]string{"database": serviceprobe.Text(db.Name)})
	}
	b.Metric("postgres.xid.age.max", "gauge", *data.XID, "", nil)
	b.Metric("postgres.transaction.oldest_age", "gauge", *data.Oldest, "seconds", nil)
	b.Metric("postgres.deadlocks", "counter", *data.Deadlocks, "", nil)
	return nil
}
