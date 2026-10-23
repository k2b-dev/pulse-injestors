package postgres

import (
	"context"
	"errors"
	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe/probetest"
	"strings"
	"testing"
)

const fixture = `{"connections":{"active":2,"idle":8,"idle in transaction":1},"max_connections":100,"databases":[{"name":"app|db","size":1024}],"xid_age":500,"oldest_age":30,"deadlocks":4}`

func TestQueryAndParsing(t *testing.T) {
	for _, version := range []bool{false, true} {
		c := Collector{Version: version, Run: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Error("unbounded command")
			}
			if name != "runuser" || strings.Join(args[:4], " ") != "-u postgres -- psql" {
				t.Error("default command")
			}
			joined := strings.Join(args, " ")
			for _, required := range []string{"-w", "-X", "-At", "-q", "ON_ERROR_STOP=1", "--dbname postgres", "BEGIN READ ONLY", "statement_timeout = '10s'"} {
				if !strings.Contains(joined, required) {
					t.Errorf("missing %s", required)
				}
			}
			for _, flag := range []string{"-X", "-At", "-q"} {
				count := 0
				for _, arg := range args {
					if arg == flag {
						count++
					}
				}
				if count != 1 {
					t.Errorf("%s occurs %d times", flag, count)
				}
			}
			if version {
				if args[len(args)-1] != VersionSQL {
					t.Error("version SQL")
				}
				return []byte(`"18.0"`), nil
			}
			if args[len(args)-1] != CapacitySQL {
				t.Error("capacity SQL")
			}
			for _, required := range []string{"WHERE NOT datistemplate", "LIMIT 100", "backend_type = 'client backend'", "pid <> pg_backend_pid()", "FROM pg_stat_activity WHERE backend_type = 'client backend' AND state <> 'idle'"} {
				if !strings.Contains(CapacitySQL, required) {
					t.Error(required)
				}
			}
			return []byte(fixture), nil
		}}
		b := probetest.Collect(t, c)
		if version {
			probetest.State(t, b, "postgres.version", "18.0")
			continue
		}
		if len(b.Metrics) != 9 {
			t.Fatalf("metrics=%d", len(b.Metrics))
		}
		probetest.Metric(t, b, "postgres.database.size", 1024, "bytes")
		probetest.Metric(t, b, "postgres.transaction.oldest_age", 30, "seconds")
		probetest.Metric(t, b, "postgres.deadlocks", 4, "")
		for _, m := range b.Metrics {
			if m.Name == "postgres.connections" && m.Dimensions["state"] == "other" && m.Value != 0 {
				t.Fatal("missing state not zero")
			}
		}
	}
}
func TestCustomCommand(t *testing.T) {
	c := Collector{Config: config.PostgresConfig{Command: []string{"psql-local", "--host=/run/postgresql"}, Database: "app"}, Run: func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "psql-local" || args[0] != "--host=/run/postgresql" || !strings.Contains(strings.Join(args, " "), "--dbname app") {
			t.Error("custom command lost")
		}
		return []byte(fixture), nil
	}}
	probetest.Collect(t, c)
}
func TestInvalidOutputAndPrivateStderr(t *testing.T) {
	secret := "fixture-pg-secret"
	for _, raw := range []string{secret, `{}`, strings.Replace(fixture, `"deadlocks":4`, `"deadlocks":-1`, 1), strings.Repeat("x", serviceprobe.MaxBytes+1)} {
		probetest.Failure(t, Collector{Run: func(context.Context, string, ...string) ([]byte, error) { return []byte(raw), nil }}, "postgres", secret)
	}
	probetest.Failure(t, Collector{Run: func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New(secret) }}, "postgres", secret)
}
