package uptime

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
	"github.com/valentinkolb/pulse-injestors/internal/validation"
)

func TestCollectorEmitsStableEndpointResourcesAndProtocolSignals(t *testing.T) {
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer tcpListener.Close()
	go func() {
		conn, acceptErr := tcpListener.Accept()
		if acceptErr == nil {
			_ = conn.Close()
		}
	}()

	httpServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer httpServer.Close()

	scope := monitoring.Scope{
		EntityID:   "uptime-probe:office",
		EntityType: "uptime-probe",
		Label:      "Office",
		Dimensions: map[string]string{"probe": "office", "site": "berlin"},
		Timestamp:  time.Date(2026, 7, 23, 18, 0, 0, 0, time.UTC),
	}
	collector := Collector{
		Concurrency: 4,
		Timeout:     time.Second,
		HTTPClient:  httpServer.Client(),
		Ping:        func(context.Context, string) error { return nil },
		Targets: []Target{
			{ID: "router", Label: "Router", Kind: KindICMP, Address: "192.0.2.1"},
			{ID: "resolver", Label: "Resolver", Kind: KindDNS, Address: "localhost"},
			{ID: "database", Label: "Database", Kind: KindTCP, Address: tcpListener.Addr().String()},
			{ID: "pulse", Label: "Pulse", Kind: KindHTTP, Address: httpServer.URL, ExpectedStatus: http.StatusNoContent},
		},
	}

	batch, err := collector.Collect(context.Background(), scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"uptime-endpoint:office:router",
		"uptime-endpoint:office:resolver",
		"uptime-endpoint:office:database",
		"uptime-endpoint:office:pulse",
	} {
		if !hasResource(batch, id) {
			t.Errorf("missing resource %q", id)
		}
	}
	if metricValue(batch, "uptime.check.availability", "pulse") != 1 {
		t.Fatalf("HTTP availability metric missing: %#v", batch.Metrics)
	}
	if metricValue(batch, "uptime.dns.address_count", "resolver") < 1 {
		t.Fatalf("DNS address count missing: %#v", batch.Metrics)
	}
	if metricValue(batch, "uptime.tls.certificate.expires_in", "pulse") <= 0 {
		t.Fatalf("TLS expiry metric missing: %#v", batch.Metrics)
	}
	if stateValue(batch, "uptime.http.status_code", "pulse") != http.StatusNoContent {
		t.Fatalf("HTTP status state missing: %#v", batch.States)
	}
	if stateValue(batch, "uptime.check.error", "pulse") != "" {
		t.Fatalf("unexpected HTTP error: %#v", batch.States)
	}
}

func TestCollectorModelsTargetFailureWithoutReturningCollectorError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	batch, err := (Collector{
		Targets: []Target{{
			ID:             "api",
			Label:          "API",
			Kind:           KindHTTP,
			Address:        server.URL,
			ExpectedStatus: http.StatusOK,
		}},
	}).Collect(context.Background(), monitoring.Scope{
		EntityID:   "uptime-probe:office",
		EntityType: "uptime-probe",
		Dimensions: map[string]string{"probe": "office"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if metricValue(batch, "uptime.check.availability", "api") != 0 {
		t.Fatalf("availability = %#v", batch.Metrics)
	}
	if stateValue(batch, "uptime.check.success", "api") != false {
		t.Fatalf("success state = %#v", batch.States)
	}
	if stateValue(batch, "uptime.check.error", "api") != "unexpected HTTP status" {
		t.Fatalf("error state = %#v", batch.States)
	}
	if len(batch.Events) != 0 {
		t.Fatalf("target failures must not emit events: %#v", batch.Events)
	}
}

func TestCollectorDoesNotReportUnavailablePingCommandAsDowntimeMetric(t *testing.T) {
	batch, err := (Collector{
		Ping: func(context.Context, string) error { return exec.ErrNotFound },
		Targets: []Target{{
			ID: "router", Label: "Router", Kind: KindICMP, Address: "192.0.2.1",
		}},
	}).Collect(context.Background(), monitoring.Scope{
		EntityID:   "uptime-probe:office",
		EntityType: "uptime-probe",
		Dimensions: map[string]string{"probe": "office"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) != 0 {
		t.Fatalf("unexpected metrics: %#v", batch.Metrics)
	}
	if stateValue(batch, "uptime.check.measured", "router") != false {
		t.Fatalf("measured state = %#v", batch.States)
	}
	if stateValue(batch, "uptime.check.error", "router") != "ping command unavailable" {
		t.Fatalf("error state = %#v", batch.States)
	}
}

func TestCollectorDoesNotReportPingExecutionErrorAsDowntimeMetric(t *testing.T) {
	batch, err := (Collector{
		Ping: func(ctx context.Context, _ string) error {
			return exec.CommandContext(ctx, "sh", "-c", "exit 2").Run()
		},
		Targets: []Target{{
			ID: "router", Label: "Router", Kind: KindICMP, Address: "192.0.2.1",
		}},
	}).Collect(context.Background(), monitoring.Scope{
		EntityID:   "uptime-probe:office",
		EntityType: "uptime-probe",
		Dimensions: map[string]string{"probe": "office"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) != 0 {
		t.Fatalf("unexpected metrics: %#v", batch.Metrics)
	}
	if stateValue(batch, "uptime.check.measured", "router") != false {
		t.Fatalf("measured state = %#v", batch.States)
	}
	if stateValue(batch, "uptime.check.error", "router") != "ping command failed" {
		t.Fatalf("error state = %#v", batch.States)
	}
}

func TestCollectorClearsHTTPResponseStatesAfterRequestFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := server.URL
	server.Close()

	batch, err := (Collector{
		Targets: []Target{{
			ID: "api", Label: "API", Kind: KindHTTP, Address: address,
		}},
		Timeout: time.Second,
	}).Collect(context.Background(), monitoring.Scope{
		EntityID:   "uptime-probe:office",
		EntityType: "uptime-probe",
		Dimensions: map[string]string{"probe": "office"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stateValue(batch, "uptime.http.status_code", "api") != 0 {
		t.Fatalf("status state = %#v", batch.States)
	}
	for _, key := range []string{
		"uptime.http.final_url",
		"uptime.http.protocol",
		"uptime.tls.server_name",
		"uptime.tls.issuer",
		"uptime.tls.expires_at",
	} {
		if stateValue(batch, key, "api") != "" {
			t.Fatalf("%s state = %#v", key, batch.States)
		}
	}
}

func TestDefaultTargetsCoverAllCheckTypes(t *testing.T) {
	targets := DefaultTargets(3 * time.Second)
	if len(targets) != 8 {
		t.Fatalf("target count = %d", len(targets))
	}
	kinds := map[string]bool{}
	for _, target := range targets {
		kinds[target.Kind] = true
		if target.Timeout != 3*time.Second {
			t.Fatalf("target timeout = %s", target.Timeout)
		}
	}
	for _, kind := range []string{KindICMP, KindDNS, KindTCP, KindHTTP} {
		if !kinds[kind] {
			t.Errorf("missing default kind %q", kind)
		}
	}
}

func hasResource(batch pulse.Batch, entityID string) bool {
	for _, metric := range batch.Metrics {
		if metric.EntityID == entityID && metric.Resource != nil && metric.Resource.Label != "" {
			return true
		}
	}
	for _, state := range batch.States {
		if state.EntityID == entityID && state.Resource != nil && state.Resource.Label != "" {
			return true
		}
	}
	return false
}

func metricValue(batch pulse.Batch, name, endpoint string) float64 {
	for _, metric := range batch.Metrics {
		if metric.Name == name && metric.Dimensions["endpoint"] == endpoint {
			return metric.Value
		}
	}
	return -1
}

func stateValue(batch pulse.Batch, key, endpoint string) any {
	for _, state := range batch.States {
		if state.Key == key && state.Dimensions["endpoint"] == endpoint {
			return state.Value
		}
	}
	return nil
}
