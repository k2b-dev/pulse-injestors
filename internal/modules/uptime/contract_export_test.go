package uptime

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

// TestPulseContractExport exercises mocked collection even without the external contract harness.
func TestPulseContractExport(t *testing.T) {
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
		Resource:   pulse.ResourceRef{Type: "uptime-probe", ID: "office", Label: "Office"},
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
	if len(batch.Metrics) == 0 || len(batch.States) == 0 {
		t.Fatal("fixture must cover metrics and states")
	}
	if batch.Metrics == nil {
		batch.Metrics = []pulse.Metric{}
	}
	if batch.Events == nil {
		batch.Events = []pulse.Event{}
	}
	if batch.States == nil {
		batch.States = []pulse.State{}
	}
	if directory := os.Getenv("PULSE_CONTRACT_OUTPUT"); directory != "" {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		body, err := json.Marshal(batch)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "uptime.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
