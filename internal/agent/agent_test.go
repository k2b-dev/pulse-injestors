package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type fakePulse struct {
	mu      sync.Mutex
	status  []int // consumed per request; 200 when empty
	batches []pulse.Batch
	// rejectContaining refuses every body containing this text with 400.
	rejectContaining string
	refusedCount     int
	rejectionMessage string
}

func (f *fakePulse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	status := 200
	if len(f.status) > 0 {
		status, f.status = f.status[0], f.status[1:]
	}
	if f.rejectContaining != "" && strings.Contains(string(data), f.rejectContaining) {
		status = 400
	}
	if status != 200 {
		f.refusedCount++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(status)
		message := f.rejectionMessage
		if message == "" {
			message = "refused by test"
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "TEST", "message": message})
		return
	}
	var batch pulse.Batch
	_ = json.Unmarshal(data, &batch)
	f.batches = append(f.batches, batch)
	_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":0}`))
}

func (f *fakePulse) received() []pulse.Batch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]pulse.Batch(nil), f.batches...)
}

func (f *fakePulse) refuse(statuses ...int) {
	f.mu.Lock()
	f.status = append(f.status, statuses...)
	f.mu.Unlock()
}

type staticCollector struct{ value *float64 }

func (staticCollector) Name() string { return "static" }
func (c staticCollector) Collect(_ context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	b.Metric("test.value", "gauge", *c.value, "", nil)
	b.State("test.state", "stable", nil)
	return b.Batch(), nil
}

func setup(t *testing.T, spoolDir string) (*fakePulse, Options, *float64) {
	t.Helper()
	fake := &fakePulse{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	value := 1.0
	cfg := config.Config{
		Pulse:  config.PulseConfig{IngestURL: srv.URL, IngestToken: "token", SpoolDir: spoolDir, SpoolMaxBytes: 1 << 20},
		HTTP:   config.HTTPConfig{TimeoutSeconds: 5, MaxRetries: 2, InitialBackoffMS: 1},
		Runner: config.RunnerConfig{IntervalSeconds: 1, CollectorTimeoutSeconds: 1},
	}
	runner := &monitoring.Runner{
		Resource:   pulse.ResourceRef{Type: "host", ID: "vm-1", Label: "vm-1"},
		Collectors: []monitoring.Collector{staticCollector{&value}},
		Interval:   time.Second,
		Timeout:    time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return fake, Options{Name: "pulse-test", Version: "v-test", Command: "once", Config: cfg, Runner: runner, Logger: logger}, &value
}

func names(batches []pulse.Batch) (metrics, states, events map[string]int) {
	metrics, states, events = map[string]int{}, map[string]int{}, map[string]int{}
	for _, b := range batches {
		for _, m := range b.Metrics {
			metrics[m.Name]++
		}
		for _, s := range b.States {
			states[s.Key]++
		}
		for _, e := range b.Events {
			events[e.Kind]++
		}
	}
	return
}

func TestOnceSendsHeartbeatAndOnlyChangedStates(t *testing.T) {
	fake, opt, _ := setup(t, filepath.Join(t.TempDir(), "spool"))
	for i := 0; i < 2; i++ {
		if err := Run(context.Background(), opt); err != nil {
			t.Fatal(err)
		}
	}
	batches := fake.received()
	if len(batches) != 2 {
		t.Fatalf("requests = %d", len(batches))
	}
	metrics, states, _ := names(batches)
	if metrics["ingestor.heartbeat"] != 2 || metrics["test.value"] != 2 || metrics["ingestor.spool.bytes"] != 2 {
		t.Fatalf("metrics %v", metrics)
	}
	if states["test.state"] != 1 || states["ingestor.version"] != 1 || states["ingestor.collector.ok"] != 1 {
		t.Fatalf("states were repeated: %v", states)
	}
	for _, m := range batches[0].Metrics {
		if m.Name == "ingestor.heartbeat" && (m.Resource == nil || m.Resource.Key() != "host:vm-1" || m.Dimensions["ingestor"] != "pulse-test") {
			t.Fatalf("heartbeat identity %#v", m)
		}
	}
}

func TestOnceKeepsDataWhilePulseRefusesAndDeliversLater(t *testing.T) {
	fake, opt, value := setup(t, filepath.Join(t.TempDir(), "spool"))
	fake.refuse(404)
	err := Run(context.Background(), opt)
	if err == nil || !strings.Contains(err.Error(), "stay queued") || !strings.Contains(err.Error(), "refused by test") {
		t.Fatalf("paused run: %v", err)
	}
	*value = 2
	if err := Run(context.Background(), opt); err != nil {
		t.Fatal(err)
	}
	var values []float64
	for _, b := range fake.received() {
		for _, m := range b.Metrics {
			if m.Name == "test.value" {
				values = append(values, m.Value)
			}
		}
	}
	if len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("delivered values %v", values)
	}
}

func TestInvalidItemIsIsolatedAndReported(t *testing.T) {
	fake, opt, _ := setup(t, filepath.Join(t.TempDir(), "spool"))
	fake.rejectContaining = `"name":"test.value"`
	if err := Run(context.Background(), opt); err != nil {
		t.Fatalf("rejection must not block: %v", err)
	}
	metrics, states, events := names(fake.received())
	if metrics["test.value"] != 0 || metrics["ingestor.heartbeat"] != 1 || states["test.state"] != 1 {
		t.Fatalf("healthy items were not delivered: %v %v", metrics, states)
	}
	if events["ingestor.delivery.rejected"] != 1 {
		t.Fatalf("events %v", events)
	}
	for _, b := range fake.received() {
		for _, e := range b.Events {
			if e.Kind == "ingestor.delivery.rejected" && e.Attributes["item"] != "metric test.value (gauge, unit )" {
				t.Fatalf("event attributes %v", e.Attributes)
			}
		}
	}
}

func TestOnceWithoutSpoolReportsLoss(t *testing.T) {
	fake, opt, _ := setup(t, "")
	opt.Config.Pulse.SpoolMaxBytes = 0
	fake.refuse(401)
	if err := Run(context.Background(), opt); err == nil || !strings.Contains(err.Error(), "lost") {
		t.Fatalf("err = %v", err)
	}
}

func TestServeKeepsCollectingWhileDeliveryIsBlocked(t *testing.T) {
	fake, opt, _ := setup(t, filepath.Join(t.TempDir(), "spool"))
	opt.Command = "run"
	fake.refuse(503, 503, 503)
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()
	if err := Run(ctx, opt); err != nil {
		t.Fatal(err)
	}
	metrics, _, _ := names(fake.received())
	if metrics["ingestor.heartbeat"] < 3 {
		t.Fatalf("heartbeats delivered: %d", metrics["ingestor.heartbeat"])
	}
}

func TestServeStopsSendingAMetricWithDefinitionConflict(t *testing.T) {
	fake, opt, _ := setup(t, filepath.Join(t.TempDir(), "spool"))
	opt.Command = "run"
	fake.rejectContaining = `"name":"test.value"`
	fake.rejectionMessage = "Metric type and unit must match the existing definition"
	ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	defer cancel()
	if err := Run(ctx, opt); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	refused := fake.refusedCount
	fake.mu.Unlock()
	metrics, _, events := names(fake.received())
	if metrics["ingestor.heartbeat"] < 3 || events["ingestor.delivery.rejected"] != 1 {
		t.Fatalf("metrics %v events %v", metrics, events)
	}
	// Isolation in the first cycle only; later cycles leave the metric out.
	if refused > 12 {
		t.Fatalf("refused requests %d", refused)
	}
}

func TestCollectResendsStatesAfterSpoolLoss(t *testing.T) {
	for _, reason := range []string{"eviction", "expiry", "rejection"} {
		t.Run(reason, func(t *testing.T) {
			fake, opt, _ := setup(t, "")
			a, err := newAgent(opt)
			if err != nil {
				t.Fatal(err)
			}
			defer a.close()
			if err := a.collect(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			switch reason {
			case "eviction":
				// A new part consumes the whole budget, evicting the state batch.
				batch := pulse.Batch{States: []pulse.State{{Key: "replacement", Value: true}}}
				body, _ := json.Marshal(batch)
				a.spool.MaxBytes = int64(len(body))
				if err := a.spool.Enqueue([]pulse.Batch{batch}, time.Now()); err != nil {
					t.Fatal(err)
				}
				a.spool.MaxBytes = opt.Config.Pulse.SpoolMaxBytes
			case "expiry":
				expiredAt := a.spool.State().Oldest.Add(pulse.SpoolMaxAge)
				a.spool.Now = func() time.Time { return expiredAt }
			case "rejection":
				fake.rejectContaining = `"key":"test.state"`
			}
			if result := a.spool.Drain(context.Background(), a.client); result.Outcome != pulse.Delivered {
				t.Fatalf("drain after loss: %+v", result)
			}
			a.spool.Now = nil
			fake.mu.Lock()
			fake.rejectContaining = ""
			fake.batches = nil
			fake.mu.Unlock()
			// Consuming the notice must reset the cache before this cycle filters.
			if err := a.collect(context.Background(), true); err != nil {
				t.Fatal(err)
			}
			if result := a.spool.Drain(context.Background(), a.client); result.Outcome != pulse.Delivered {
				t.Fatalf("drain next cycle: %+v", result)
			}
			_, states, events := names(fake.received())
			if states["test.state"] != 1 || events["ingestor.telemetry.dropped"]+events["ingestor.delivery.rejected"] != 1 {
				t.Fatalf("lost state not resent: states=%v events=%v", states, events)
			}
		})
	}
}

func TestLocalRunKeepsCollectingDueCollectors(t *testing.T) {
	_, opt, _ := setup(t, "")
	opt.Local, opt.Command = true, "run"
	// A slow collector is only due once during this short run.
	opt.Runner.Collectors = append(opt.Runner.Collectors, monitoring.WithCadence{
		Collector: localSlowCollector{}, Every: time.Hour,
	})
	now := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	opt.Runner.Now = func() time.Time {
		now = now.Add(time.Second)
		return now
	}
	file, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	stdout := os.Stdout
	os.Stdout = file
	t.Cleanup(func() { os.Stdout = stdout })
	ctx, cancel := context.WithTimeout(context.Background(), 2200*time.Millisecond)
	defer cancel()
	if err := Run(ctx, opt); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	var batches []pulse.Batch
	decoder := json.NewDecoder(file)
	for {
		var batch pulse.Batch
		if err := decoder.Decode(&batch); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		batches = append(batches, batch)
	}
	metrics, states, _ := names(batches)
	if len(batches) < 2 || metrics["test.value"] != len(batches) || states["slow.state"] != 1 {
		t.Fatalf("local batches=%d metrics=%v states=%v", len(batches), metrics, states)
	}
}

type localSlowCollector struct{}

func (localSlowCollector) Name() string { return "slow" }
func (localSlowCollector) Collect(_ context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	b.State("slow.state", true, nil)
	return b.Batch(), nil
}
