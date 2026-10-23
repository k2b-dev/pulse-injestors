// Package probetest provides local fixtures and assertions for service collectors.
package probetest

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

var Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

func Scope() monitoring.Scope {
	return monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "fixture", Label: "Fixture"}, Timestamp: Now}
}
func File(t *testing.T, value string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture")
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func CA(t *testing.T, server *httptest.Server) string {
	t.Helper()
	return File(t, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})))
}
func Collect(t *testing.T, c monitoring.Collector) pulse.Batch {
	t.Helper()
	batch, err := c.Collect(context.Background(), Scope())
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	return batch
}
func Metric(t *testing.T, b pulse.Batch, name string, value float64, unit string) {
	t.Helper()
	for _, m := range b.Metrics {
		if m.Name == name && m.Value == value && m.Unit == unit {
			return
		}
	}
	t.Fatalf("missing metric %s=%v (%s): %+v", name, value, unit, b.Metrics)
}
func State(t *testing.T, b pulse.Batch, name string, value any) {
	t.Helper()
	for _, s := range b.States {
		if s.Key == name && s.Value == value {
			return
		}
	}
	t.Fatalf("missing state %s=%v", name, value)
}
func NoSecret(t *testing.T, b pulse.Batch, extra string, secrets ...string) {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		if secret != "" && strings.Contains(string(raw)+extra, secret) {
			t.Fatal("secret escaped into telemetry or diagnostics")
		}
	}
}
func Failure(t *testing.T, c monitoring.Collector, service string, secrets ...string) {
	t.Helper()
	var log bytes.Buffer
	r := monitoring.Runner{Resource: Scope().Resource, Now: func() time.Time { return Now }, Collectors: []monitoring.Collector{c}, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	batch := r.Collect(context.Background(), true)
	State(t, batch, service+".available", false)
	State(t, batch, "ingestor.collector.ok", false)
	if len(batch.Events) != 1 || batch.Events[0].Kind != "ingestor.collector.failed" {
		t.Fatalf("failure events: %+v", batch.Events)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	NoSecret(t, batch, log.String(), secrets...)
}
