package clickhouse

import (
	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe/probetest"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const capacity = `{"data":[{"signal":"disk.free","disk":"default","value":"1024"},{"signal":"disk.total","disk":"default","value":4096},{"signal":"queries.running","disk":"","value":1},{"signal":"parts.max_per_partition","disk":"","value":7},{"signal":"merges.running","disk":"","value":2},{"signal":"errors","disk":"","value":3},{"signal":"uptime","disk":"","value":3600}]}`

func TestQueriesHeadersAndTLS(t *testing.T) {
	secret := "fixture-clickhouse-secret"
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("X-ClickHouse-User") != "monitor" || r.Header.Get("X-ClickHouse-Key") != secret || r.Header.Get("Authorization") != "" {
			t.Error("incorrect auth or method")
		}
		if r.URL.RequestURI() != "/" {
			t.Error("incorrect query settings")
		}
		raw, _ := io.ReadAll(r.Body)
		switch string(raw) {
		case HealthSQL:
			io.WriteString(w, `{"data":[{"ok":1}]}`)
		case VersionSQL:
			io.WriteString(w, `{"data":[{"version":"26.9"}]}`)
		case CapacitySQL:
			io.WriteString(w, capacity)
		default:
			t.Error("unexpected SQL")
		}
	}))
	defer s.Close()
	cfg := config.ClickHouseConfig{URL: s.URL, User: "monitor", PasswordFile: probetest.File(t, secret), CAFile: probetest.CA(t, s), ServerName: "example.com"}
	for _, g := range []string{"health", "capacity", "version"} {
		b := probetest.Collect(t, Collector{Config: cfg, Group: g})
		probetest.NoSecret(t, b, "", secret)
		switch g {
		case "health":
			probetest.State(t, b, "clickhouse.available", true)
			for _, state := range b.States {
				if state.Key == "clickhouse.up" {
					t.Fatal("redundant state")
				}
			}
		case "capacity":
			if len(b.Metrics) != 7 {
				t.Fatal(len(b.Metrics))
			}
			probetest.Metric(t, b, "clickhouse.disk.free", 1024, "bytes")
			probetest.Metric(t, b, "clickhouse.uptime", 3600, "seconds")
		case "version":
			probetest.State(t, b, "clickhouse.version", "26.9")
		}
	}
	cfg.ServerName = "wrong.invalid"
	probetest.Failure(t, Collector{Config: cfg}, "clickhouse", secret)
	cfg.ServerName = ""
	cfg.CAFile = ""
	probetest.Failure(t, Collector{Config: cfg}, "clickhouse", secret)
}
func TestFailuresAndRedaction(t *testing.T) {
	secret := "fixture-clickhouse-secret"
	for _, tc := range []struct {
		group, body string
		status      int
	}{
		{"health", secret, 403}, {"health", `{}`, 200}, {"health", `{"data":[{"ok":0}]}`, 200}, {"health", strings.Repeat("x", serviceprobe.MaxBytes+1), 200},
		{"capacity", `{"data":[{"signal":"errors","value":1}]}`, 200}, {"capacity", strings.Replace(capacity, `"value":3600`, `"value":"NaN"`, 1), 200},
		{"version", `{"data":[{}]}`, 200},
		{"capacity", strings.Replace(capacity, `{"signal":"disk.total","disk":"default","value":4096},`, "", 1), 200},
		{"capacity", strings.Replace(capacity, `"signal":"errors","disk":""`, `"signal":"errors","disk":"unexpected"`, 1), 200},
	} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); io.WriteString(w, tc.body) }))
		probetest.Failure(t, Collector{Config: config.ClickHouseConfig{URL: s.URL, PasswordFile: probetest.File(t, secret)}, Group: tc.group}, "clickhouse", secret)
		s.Close()
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.ReplaceAll(capacity, "default", secret))
	}))
	defer s.Close()
	probetest.NoSecret(t, probetest.Collect(t, Collector{Config: config.ClickHouseConfig{URL: s.URL, PasswordFile: probetest.File(t, secret)}, Group: "capacity"}), "", secret)
}
