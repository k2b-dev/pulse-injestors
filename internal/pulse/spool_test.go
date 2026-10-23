package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixedNow = time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)

// fakePulse records request bodies and answers with the status the test sets.
type fakePulse struct {
	mu     sync.Mutex
	status int
	body   string
	bodies []string
	keys   []string
}

func (f *fakePulse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bodies = append(f.bodies, string(data))
	f.keys = append(f.keys, r.Header.Get("Idempotency-Key"))
	status := f.status
	if status == 0 {
		status = 200
	}
	w.WriteHeader(status)
	if status == 200 {
		_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":1}`))
		return
	}
	_, _ = w.Write([]byte(f.body))
}

func (f *fakePulse) set(status int, body string) {
	f.mu.Lock()
	f.status, f.body = status, body
	f.mu.Unlock()
}

func (f *fakePulse) requests() ([]string, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.bodies...), append([]string(nil), f.keys...)
}

func newFake(t *testing.T) (*fakePulse, Client) {
	t.Helper()
	fake := &fakePulse{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	return fake, Client{URL: srv.URL, Token: "token", HTTPClient: srv.Client(), InitialBackoff: time.Millisecond}
}

func stateBatch(key string) Batch {
	return Batch{Metrics: []Metric{}, Events: []Event{}, States: []State{{Key: key, Value: 1, Timestamp: fixedNow}}}
}

func openSpool(t *testing.T, dir string, maxBytes int64, now time.Time) (*Spool, *Stats) {
	t.Helper()
	stats := &Stats{}
	spool := &Spool{Dir: dir, MaxBytes: maxBytes, Binding: "binding", Stats: stats, Now: func() time.Time { return now }}
	if err := spool.Open(now); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(spool.Close)
	return spool, stats
}

func TestSpoolSurvivesRestartAndReplaysIdenticalBodyAndKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	fake, client := newFake(t)
	spool, _ := openSpool(t, dir, 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("a"), stateBatch("b")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	fake.set(503, `{"code":"PULSE_STORE_UNAVAILABLE","message":"down"}`)
	if result := spool.Drain(context.Background(), client); result.Outcome != Retry {
		t.Fatalf("drain = %+v", result)
	}
	spool.Close()

	reopened, _ := openSpool(t, dir, 1<<20, fixedNow)
	if state := reopened.State(); state.Parts != 2 {
		t.Fatalf("after restart: %+v", state)
	}
	fake.set(200, "")
	if result := reopened.Drain(context.Background(), client); result.Outcome != Delivered {
		t.Fatalf("drain = %+v", result)
	}
	bodies, keys := fake.requests()
	// First attempt (503), then both parts in order.
	if len(bodies) != 3 || bodies[0] != bodies[1] || keys[0] != keys[1] || keys[1] == keys[2] {
		t.Fatalf("bodies=%d keys=%v", len(bodies), keys)
	}
	if keys[1] != IdempotencyKey([]byte(bodies[1])) || !strings.Contains(bodies[2], `"key":"b"`) {
		t.Fatalf("unexpected replay: %v", keys)
	}
	if reopened.State().Parts != 0 {
		t.Fatal("delivered parts remain")
	}
}

func TestSpoolRefusesChangedBindingAndSecondProcess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	openSpool(t, dir, 1024, fixedNow)
	second := &Spool{Dir: dir, MaxBytes: 1024, Binding: "binding"}
	if err := second.Open(fixedNow); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("second open: %v", err)
	}
	other := filepath.Join(t.TempDir(), "spool")
	first := &Spool{Dir: other, MaxBytes: 1024, Binding: "one"}
	if err := first.Open(fixedNow); err != nil {
		t.Fatal(err)
	}
	first.Close()
	changed := &Spool{Dir: other, MaxBytes: 1024, Binding: "two"}
	if err := changed.Open(fixedNow); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed binding: %v", err)
	}
	if err := os.Chmod(other, 0755); err != nil {
		t.Fatal(err)
	}
	if err := (&Spool{Dir: other, MaxBytes: 1024, Binding: "one"}).Open(fixedNow); err == nil {
		t.Fatal("public spool accepted")
	}
}

func TestSpoolDropsOldestWhenBudgetIsExhausted(t *testing.T) {
	body, _ := json.Marshal(stateBatch("k0"))
	spool, stats := openSpool(t, filepath.Join(t.TempDir(), "spool"), int64(len(body))*3, fixedNow)
	for _, key := range []string{"k0", "k1", "k2", "k3"} {
		if err := spool.Enqueue([]Batch{stateBatch(key)}, fixedNow); err != nil {
			t.Fatal(err)
		}
	}
	fake, client := newFake(t)
	spool.Drain(context.Background(), client)
	bodies, _ := fake.requests()
	if len(bodies) != 3 || !strings.Contains(bodies[0], `"k1"`) {
		t.Fatalf("delivered %v", bodies)
	}
	notices := stats.TakeNotices()
	if len(notices) != 1 || notices[0].Kind != "dropped" || notices[0].Reason != "budget" || notices[0].Parts != 1 {
		t.Fatalf("notices %+v", notices)
	}
	huge := Batch{States: []State{{Key: strings.Repeat("x", 500), Value: 1}}}
	if err := spool.Enqueue([]Batch{huge}, fixedNow); err == nil {
		t.Fatal("collection larger than the budget was queued")
	}
}

func TestSpoolExpiresPartsBeforePulseForgetsTheKey(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	spool, stats := openSpool(t, dir, 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("old")}, fixedNow.Add(-SpoolMaxAge)); err != nil {
		t.Fatal(err)
	}
	if err := spool.Enqueue([]Batch{stateBatch("new")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	spool.Close()
	reopened, _ := openSpool(t, dir, 1<<20, fixedNow)
	if state := reopened.State(); state.Parts != 1 {
		t.Fatalf("state %+v", state)
	}
	notices := stats.TakeNotices()
	if len(notices) != 1 || notices[0].Reason != "expired" {
		t.Fatalf("notices %+v", notices)
	}
}

func TestSpoolIsolatesTheInvalidItemByHalving(t *testing.T) {
	spool, stats := openSpool(t, filepath.Join(t.TempDir(), "spool"), 1<<20, fixedNow)
	batch := Batch{}
	for i := 0; i < 37; i++ {
		batch.Metrics = append(batch.Metrics, Metric{Name: fmt.Sprintf("m.%d", i), Type: "gauge", Value: 1, Timestamp: fixedNow})
	}
	batch.States = []State{{Key: "s", Value: true, Timestamp: fixedNow}}
	if err := spool.Enqueue([]Batch{batch, stateBatch("after")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	delivered := map[string]int{}
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got Batch
		_ = json.NewDecoder(r.Body).Decode(&got)
		mu.Lock()
		defer mu.Unlock()
		requests++
		for _, m := range got.Metrics {
			if m.Name == "m.23" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"code":"BAD_INPUT","message":"Metric type and unit must match the existing definition"}`))
				return
			}
		}
		for _, m := range got.Metrics {
			delivered[m.Name]++
		}
		for _, s := range got.States {
			delivered[s.Key]++
		}
		_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":0}`))
	}))
	defer srv.Close()
	if result := spool.Drain(context.Background(), Client{URL: srv.URL, HTTPClient: srv.Client()}); result.Outcome != Delivered {
		t.Fatalf("drain = %+v", result)
	}
	if len(delivered) != 38 || delivered["m.23"] != 0 || delivered["after"] != 1 || delivered["s"] != 1 {
		t.Fatalf("delivered %v", delivered)
	}
	for name, count := range delivered {
		if count != 1 {
			t.Fatalf("%s delivered %d times", name, count)
		}
	}
	if requests > 20 {
		t.Fatalf("isolation took %d requests", requests)
	}
	notices := stats.TakeNotices()
	if len(notices) != 1 || notices[0].Item != "metric m.23 (gauge, unit )" {
		t.Fatalf("notices %+v", notices)
	}
}

func TestSpoolQuarantinesRejectedPartAndKeepsDelivering(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	spool, stats := openSpool(t, dir, 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("bad"), stateBatch("good")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	fake, client := newFake(t)
	fake.set(400, `{"code":"BAD_INPUT","message":"Metric type and unit must match the existing definition"}`)
	calls := 0
	client.HTTPClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 2 {
			fake.set(200, "")
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	if result := spool.Drain(context.Background(), client); result.Outcome != Delivered {
		t.Fatalf("drain = %+v", result)
	}
	files, _ := filepath.Glob(filepath.Join(dir, quarantineDir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("quarantine files %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if !strings.Contains(string(data), "BAD_INPUT") || !strings.Contains(string(data), `"key":"bad"`) {
		t.Fatalf("quarantine content %s", data)
	}
	var record struct {
		RejectedAt time.Time `json:"rejectedAt"`
	}
	if err := json.Unmarshal(data, &record); err != nil || !record.RejectedAt.Equal(fixedNow) {
		t.Fatalf("quarantine clock: %+v, %v", record, err)
	}
	notices := stats.TakeNotices()
	if len(notices) != 1 || notices[0].Kind != "rejected" || notices[0].Reason != "BAD_INPUT" || notices[0].States != 1 {
		t.Fatalf("notices %+v", notices)
	}
	if spool.State().Parts != 0 {
		t.Fatal("parts remain")
	}
}

func TestSpoolLimitsQuarantineFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	spool, _ := openSpool(t, dir, 1<<20, fixedNow)
	for i := 0; i < maxQuarantineFiles+5; i++ {
		if err := spool.Enqueue([]Batch{stateBatch("bad")}, fixedNow); err != nil {
			t.Fatal(err)
		}
	}
	fake, client := newFake(t)
	fake.set(422, `{"message":"invalid"}`)
	spool.Drain(context.Background(), client)
	files, _ := filepath.Glob(filepath.Join(dir, quarantineDir, "*.json"))
	if len(files) != maxQuarantineFiles {
		t.Fatalf("quarantine holds %d files", len(files))
	}
}

func TestSpoolKeepsDataWhileSourceIsPaused(t *testing.T) {
	spool, _ := openSpool(t, filepath.Join(t.TempDir(), "spool"), 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("a")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	fake, client := newFake(t)
	// Pulse answers 404 for a disabled source.
	fake.set(404, `{"message":"Ingest source not found","code":"NOT_FOUND"}`)
	if result := spool.Drain(context.Background(), client); result.Outcome != Paused || result.Code != "NOT_FOUND" {
		t.Fatalf("drain = %+v", result)
	}
	if spool.State().Parts != 1 {
		t.Fatal("paused part was dropped")
	}
	fake.set(200, "")
	if result := spool.Drain(context.Background(), client); result.Outcome != Delivered || spool.State().Parts != 0 {
		t.Fatalf("drain = %+v", result)
	}
}

func TestSpoolTreatsAmbiguousAcknowledgementAsRetry(t *testing.T) {
	spool, _ := openSpool(t, "", 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("a")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"accepted":true}`))
	}))
	defer srv.Close()
	client := Client{URL: srv.URL, HTTPClient: srv.Client()}
	if result := spool.Drain(context.Background(), client); result.Outcome != Retry || spool.State().Parts != 1 {
		t.Fatalf("drain = %+v parts=%d", result, spool.State().Parts)
	}
}

func TestSpoolDrainStopsOnCancellation(t *testing.T) {
	spool, _ := openSpool(t, "", 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("a")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	_, client := newFake(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := spool.Drain(ctx, client); result.Outcome != Retry || spool.State().Parts != 1 {
		t.Fatalf("drain = %+v", result)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSpoolRefusesLegacyOutboxFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "legacy.json")
	if err := os.WriteFile(path, []byte(`{"states":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	spool := &Spool{Dir: dir, MaxBytes: 1024}
	defer spool.Close()
	if err := spool.Open(fixedNow); err == nil || !strings.Contains(err.Error(), dir) || !strings.Contains(err.Error(), "deliver or delete") {
		t.Fatalf("legacy open = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("legacy file changed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := spool.Open(fixedNow); err != nil {
		t.Fatalf("failed open leaked lock: %v", err)
	}
}

func TestSpoolSplitBudgetWaitsWithoutQuarantining(t *testing.T) {
	now := fixedNow
	spool, stats := openSpool(t, filepath.Join(t.TempDir(), "spool"), 1<<20, now)
	spool.Now = func() time.Time { return now }
	for i := 0; i < 256; i++ {
		if wait := spool.allowSplit(now); wait != 0 {
			t.Fatalf("split %d refused: %s", i, wait)
		}
	}
	now = now.Add(15 * time.Minute)
	batch := stateBatch("healthy")
	batch.States = append(batch.States, stateBatch("bad").States...)
	if err := spool.Enqueue([]Batch{batch, stateBatch("later")}, now); err != nil {
		t.Fatal(err)
	}
	fake, client := newFake(t)
	fake.set(400, `{"message":"invalid"}`)
	if result := spool.Drain(context.Background(), client); result.Outcome != Retry || result.RetryAfter != 45*time.Minute {
		t.Fatalf("budget exhausted: %+v", result)
	}
	bodies, _ := fake.requests()
	if len(bodies) != 1 || spool.State().Parts != 2 || len(stats.TakeNotices()) != 0 {
		t.Fatalf("FIFO lost: requests=%d state=%+v", len(bodies), spool.State())
	}
	files, _ := filepath.Glob(filepath.Join(spool.Dir, quarantineDir, "*.json"))
	if len(files) != 0 {
		t.Fatalf("part quarantined: %v", files)
	}
	now = now.Add(45 * time.Minute)
	fake.set(200, "")
	if wait := spool.allowSplit(now); wait != 0 {
		t.Fatalf("budget did not reset: %s", wait)
	}
	if result := spool.Drain(context.Background(), client); result.Outcome != Delivered {
		t.Fatalf("retry: %+v", result)
	}
	bodies, _ = fake.requests()
	if len(bodies) != 3 || bodies[0] != bodies[1] || !strings.Contains(bodies[2], `"later"`) {
		t.Fatalf("queued body/order changed: %v", bodies)
	}
}

func TestSpoolOnlyBlocksDefinitionConflicts(t *testing.T) {
	for _, tc := range []struct {
		message string
		blocked bool
	}{
		{"Metric type and unit must match the existing definition", true},
		{"Metric TYPE AND UNIT must agree within a batch", true},
		{"Timestamp is too far in the future", false},
		{"Invalid dimensions", false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			spool, stats := openSpool(t, "", 1<<20, fixedNow)
			// Single-item rejections bypass even an exhausted split budget.
			spool.splitWindow, spool.splits = fixedNow, maxSplitsPerHour
			batch := Batch{Metrics: []Metric{{Name: "test.metric", Type: "gauge", Value: 1, Timestamp: fixedNow}}}
			if err := spool.Enqueue([]Batch{batch}, fixedNow); err != nil {
				t.Fatal(err)
			}
			fake, client := newFake(t)
			body, _ := json.Marshal(map[string]string{"message": tc.message})
			fake.set(400, string(body))
			if result := spool.Drain(context.Background(), client); result.Outcome != Delivered || spool.State().Parts != 0 {
				t.Fatalf("single item not quarantined: %+v", result)
			}
			if stats.MetricBlocked("test.metric", fixedNow) != tc.blocked {
				t.Fatalf("blocked != %v", tc.blocked)
			}
			if notices := stats.TakeNotices(); len(notices) != 1 || notices[0].Kind != "rejected" || !notices[0].OccurredAt.Equal(fixedNow) {
				t.Fatalf("notices = %+v", notices)
			}
		})
	}
}

func TestSpoolExpiresUsingInjectedClock(t *testing.T) {
	spool, stats := openSpool(t, "", 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("old")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	spool.Now = func() time.Time { return fixedNow.Add(SpoolMaxAge) }
	fake, client := newFake(t)
	if result := spool.Drain(context.Background(), client); result.Outcome != Delivered {
		t.Fatalf("drain = %+v", result)
	}
	bodies, _ := fake.requests()
	if notices := stats.TakeNotices(); len(bodies) != 0 || len(notices) != 1 || notices[0].Reason != "expired" || !notices[0].OccurredAt.Equal(spool.Now()) {
		t.Fatalf("expired: requests=%d notices=%+v", len(bodies), notices)
	}
}

func TestSpoolSplitWriteFailureKeepsPartQueued(t *testing.T) {
	spool, stats := openSpool(t, "", 1<<20, fixedNow)
	batch := stateBatch("healthy")
	batch.States = append(batch.States, stateBatch("bad").States...)
	if err := spool.Enqueue([]Batch{batch}, fixedNow); err != nil {
		t.Fatal(err)
	}
	part, body, err := spool.peek(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// The replacement cannot be written to a missing directory.
	spool.Dir = filepath.Join(t.TempDir(), "missing")
	result := spool.reject(*part, body, Result{Outcome: Rejected, Status: 400})
	spool.Dir = ""
	if result.Outcome != Retry || result.Err == nil || spool.State().Parts != 1 || len(stats.TakeNotices()) != 0 {
		t.Fatalf("split write failure lost the part: %+v", result)
	}
	_, queued, err := spool.peek(fixedNow)
	if err != nil || string(queued) != string(body) {
		t.Fatalf("queued body changed: %s, %v", queued, err)
	}
}

func TestSpoolQuarantinesIdempotencyConflict(t *testing.T) {
	spool, stats := openSpool(t, "", 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("conflict")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	fake, client := newFake(t)
	fake.set(409, `{"message":"Idempotency key already used"}`)
	if result := spool.Drain(context.Background(), client); result.Outcome != Delivered || spool.State().Parts != 0 {
		t.Fatalf("key conflict blocked FIFO: %+v", result)
	}
	if notices := stats.TakeNotices(); len(notices) != 1 || notices[0].Kind != "rejected" || notices[0].Status != 409 {
		t.Fatalf("notices = %+v", notices)
	}
}

func TestSpoolRequestUsesRemainingLifetimeFromInjectedClock(t *testing.T) {
	spool, _ := openSpool(t, "", 1<<20, fixedNow)
	if err := spool.Enqueue([]Batch{stateBatch("near-expiry")}, fixedNow); err != nil {
		t.Fatal(err)
	}
	spool.Now = func() time.Time { return fixedNow.Add(SpoolMaxAge - 5*time.Second) }
	called := false
	client := Client{URL: "http://pulse.test/api/pulse/ingest", HTTPClient: &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			called = true
			deadline, ok := r.Context().Deadline()
			remaining := time.Until(deadline)
			if !ok || remaining <= 0 || remaining > 5*time.Second {
				t.Errorf("request lifetime = %s, deadline set = %v", remaining, ok)
			}
			return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
		}),
	}}
	if result := spool.Drain(context.Background(), client); result.Outcome != Retry || !called {
		t.Fatalf("drain = %+v, request called = %v", result, called)
	}
}

func TestSpoolFailedSplitCleansUpHalvesBeforeRestart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	spool, _ := openSpool(t, dir, 1<<20, fixedNow)
	batch := stateBatch("first")
	batch.States = append(batch.States, stateBatch("second").States...)
	if err := spool.Enqueue([]Batch{batch}, fixedNow); err != nil {
		t.Fatal(err)
	}
	part, body, err := spool.peek(fixedNow)
	if err != nil {
		t.Fatal(err)
	}
	// Prevent removal of the original after both halves have been written.
	path := filepath.Join(dir, part.name)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(path, "blocker")
	if err := os.WriteFile(blocker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		result := spool.reject(*part, body, Result{Outcome: Rejected, Status: 400})
		if result.Outcome != Retry || result.Err == nil {
			t.Fatalf("failed split = %+v", result)
		}
		files, err := filepath.Glob(filepath.Join(dir, "*"+partSuffix))
		if err != nil || len(files) != 1 || files[0] != path {
			t.Fatalf("untracked halves remain: %v, %v", files, err)
		}
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	spool.Close()
	reopened, _ := openSpool(t, dir, 1<<20, fixedNow)
	fake, client := newFake(t)
	if result := reopened.Drain(context.Background(), client); result.Outcome != Delivered {
		t.Fatalf("restarted drain = %+v", result)
	}
	if bodies, _ := fake.requests(); len(bodies) != 1 || bodies[0] != string(body) {
		t.Fatalf("restart replayed orphan halves: %v", bodies)
	}
}
