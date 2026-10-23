package nats

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

const varz = `{"version":"2.12.0","connections":4,"mem":1024,"slow_consumers":2,"in_msgs":7,"out_msgs":9,"in_bytes":100,"out_bytes":200}`

func TestGroups(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("unexpected credentials")
		}
		switch r.URL.RequestURI() {
		case "/healthz":
			io.WriteString(w, `{"status":"ok"}`)
		case "/varz":
			io.WriteString(w, varz)
		case "/routez":
			io.WriteString(w, `{"num_routes":2}`)
		case "/jsz":
			io.WriteString(w, `{"streams":3,"consumers":5,"storage":4096,"reserved_storage":8192,"api":{"errors":6},"meta_cluster":{"leader":"broker-a"}}`)
		default:
			t.Error(r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	for _, g := range []string{"health", "jetstream", "version"} {
		b := probetest.Collect(t, Collector{Config: config.NATSConfig{MonitorURL: s.URL}, Group: g})
		switch g {
		case "health":
			if len(b.Metrics) != 8 {
				t.Fatal(len(b.Metrics))
			}
			probetest.State(t, b, "nats.healthy", true)
			probetest.State(t, b, "nats.health.error", "")
			probetest.Metric(t, b, "nats.routes", 2, "")
			probetest.Metric(t, b, "nats.bytes.in", 100, "bytes")
		case "jetstream":
			if len(b.Metrics) != 5 {
				t.Fatal(len(b.Metrics))
			}
			probetest.Metric(t, b, "nats.jetstream.storage.used", 4096, "bytes")
			probetest.Metric(t, b, "nats.jetstream.storage.reserved", 8192, "bytes")
			probetest.State(t, b, "nats.jetstream.meta_leader", "broker-a")
		case "version":
			probetest.State(t, b, "nats.version", "2.12.0")
		}
	}
}
func TestOmittedZeroJetStreamFields(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"api":{"total":0,"errors":0}}`) }))
	defer s.Close()
	b := probetest.Collect(t, Collector{Config: config.NATSConfig{MonitorURL: s.URL}, Group: "jetstream"})
	probetest.Metric(t, b, "nats.jetstream.streams", 0, "")
}
func TestUnhealthyAndMalformed(t *testing.T) {
	for _, body := range []string{`{}`, `{"status":"error","error":"` + strings.Repeat("x", 400) + `"}`, strings.Repeat("x", serviceprobe.MaxBytes+1)} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503); io.WriteString(w, body) }))
		c := Collector{Config: config.NATSConfig{MonitorURL: s.URL}}
		probetest.Failure(t, c, "nats")
		b, _ := c.Collect(t.Context(), probetest.Scope())
		probetest.State(t, b, "nats.healthy", false)
		for _, state := range b.States {
			if state.Key == "nats.health.error" && len([]rune(state.Value.(string))) > 240 {
				t.Fatal("unbounded health error")
			}
		}
		s.Close()
	}
	for _, g := range []string{"jetstream", "version"} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{}`) }))
		probetest.Failure(t, Collector{Config: config.NATSConfig{MonitorURL: s.URL}, Group: g}, "nats")
		s.Close()
	}
}
func TestTLSUntrustedIsFailure(t *testing.T) {
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"status":"ok"}`) }))
	defer s.Close()
	probetest.Failure(t, Collector{Config: config.NATSConfig{MonitorURL: s.URL}}, "nats")
}
