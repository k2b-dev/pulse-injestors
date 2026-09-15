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
	"syscall"
	"testing"
	"time"
)

func spoolClient(t *testing.T, handler http.HandlerFunc) Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		handler(recorder, r)
		for key, values := range recorder.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(recorder.Code)
		if recorder.Code == 200 && recorder.Body.Len() == 0 {
			_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":0}`))
			return
		}
		_, _ = w.Write(recorder.Body.Bytes())
	}))
	t.Cleanup(srv.Close)
	return Client{URL: srv.URL, Token: "source-secret", HTTPClient: srv.Client(), SpoolDir: filepath.Join(t.TempDir(), "queue"), SpoolMaxBytes: 1024 * 1024}
}
func queuedPaths(t *testing.T, c Client) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(c.SpoolDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestSpoolSurvivesRestartWithIdenticalBodyAndKey(t *testing.T) {
	var bodies, keys []string
	failing := true
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if failing {
			w.WriteHeader(503)
		}
	})
	batch := Batch{States: []State{{Key: "alive", Value: true, Timestamp: time.Now().UTC()}}}
	if err := c.PostBatch(context.Background(), batch); err == nil {
		t.Fatal("missing send error")
	}
	if len(queuedPaths(t, c)) != 1 {
		t.Fatal("not persisted")
	}
	failing = false
	restarted := Client{URL: c.URL, Token: c.Token, HTTPClient: c.HTTPClient, SpoolDir: c.SpoolDir, SpoolMaxBytes: c.SpoolMaxBytes}
	if err := restarted.PostBatch(context.Background(), Batch{}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 || bodies[0] != bodies[1] || keys[0] != keys[1] {
		t.Fatalf("replay changed: %v %v", bodies, keys)
	}
	if len(queuedPaths(t, c)) != 0 {
		t.Fatal("delivered entries remain")
	}
}

func TestSpoolRejectsSourceChangeAndBudgetOverflow(t *testing.T) {
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	if err := c.PostBatch(context.Background(), Batch{}); err == nil {
		t.Fatal("missing send error")
	}
	c.Token = "different"
	if err := c.PostBatch(context.Background(), Batch{}); err == nil || !strings.Contains(err.Error(), "token changed") {
		t.Fatalf("source binding: %v", err)
	}
	c.Token = "source-secret"
	c.SpoolMaxBytes = 1
	if err := c.PostBatch(context.Background(), Batch{}); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("budget: %v", err)
	}
	if len(queuedPaths(t, c)) != 1 {
		t.Fatal("overflow changed persisted data")
	}
}

func TestExpiredSpoolNotReplayedAndFailureVisible(t *testing.T) {
	calls := 0
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) })
	_ = c.PostBatch(context.Background(), Batch{})
	path := queuedPaths(t, c)[0]
	data, _ := os.ReadFile(path)
	var entry queuedBatch
	_ = json.Unmarshal(data, &entry)
	entry.Created = time.Now().Add(-24 * time.Hour)
	data, _ = json.Marshal(entry)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	err := c.PostBatch(context.Background(), Batch{})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expiry: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expired entry replayed: %d", calls)
	}
}

func TestPermanentRejectDoesNotPoisonLaterDelivery(t *testing.T) {
	calls := 0
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "sensitive response payload", 400)
		}
	})
	err := c.PostBatch(context.Background(), Batch{})
	if err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("reject: %v", err)
	}
	err = c.PostBatch(context.Background(), Batch{})
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("retained rejection invisible: %v", err)
	}
	if calls != 2 || len(queuedPaths(t, c)) != 1 {
		t.Fatal("quarantine blocked or disappeared")
	}
}

func TestRetryAfterHonorsCancellation(t *testing.T) {
	calls := 0
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	})
	c.SpoolDir = ""
	c.MaxRetries = 2
	c.InitialBackoff = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := c.PostBatch(ctx, Batch{}); err == nil {
		t.Fatal("missing cancellation")
	}
	if calls != 1 {
		t.Fatal("retried before server delay")
	}
}

func TestRetryAfterDatesAndBounds(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if got := parseRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now); got != time.Minute {
		t.Fatal(got)
	}
	if got := parseRetryAfter("9223372036854775807", now); got != 24*time.Hour {
		t.Fatal(got)
	}
	if got := parseRetryAfter("-1", now); got != 0 {
		t.Fatal(got)
	}
}

func TestSpoolPartialBatchDoesNotReplayAcknowledgedPart(t *testing.T) {
	calls := 0
	failing := true
	var keys []string
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if failing && calls == 2 {
			w.WriteHeader(503)
		}
	})
	batch := Batch{States: make([]State, 501)}
	for i := range batch.States {
		batch.States[i] = State{Key: fmt.Sprintf("state-%d", i), Value: i}
	}
	if err := c.PostBatch(context.Background(), batch); err == nil {
		t.Fatal("missing partial failure")
	}
	failing = false
	if err := c.PostBatch(context.Background(), Batch{}); err != nil {
		t.Fatal(err)
	}
	if calls != 4 || keys[1] != keys[2] || keys[0] == keys[2] {
		t.Fatalf("partial replay: %v", keys)
	}
}

func TestSpoolRefusesConcurrentSender(t *testing.T) {
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected delivery") })
	if err := os.MkdirAll(c.SpoolDir, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(c.SpoolDir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := c.PostBatch(context.Background(), Batch{}); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("concurrent sender: %v", err)
	}
}

func TestSpoolQuarantinesOnlyRejectedPart(t *testing.T) {
	calls := 0
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(400)
		}
	})
	batch := Batch{States: make([]State, 1001)}
	for i := range batch.States {
		batch.States[i] = State{Key: fmt.Sprintf("state-%d", i), Value: i}
	}
	if err := c.PostBatch(context.Background(), batch); err == nil {
		t.Fatal("missing rejection")
	}
	if calls != 3 {
		t.Fatalf("valid later chunks not delivered: %d", calls)
	}
	data, _ := os.ReadFile(queuedPaths(t, c)[0])
	var entry queuedBatch
	_ = json.Unmarshal(data, &entry)
	if len(entry.Rejected) != 1 || len(entry.Parts) != 0 {
		t.Fatalf("incorrect quarantine: %#v", entry)
	}
}

func TestSpoolReplaysDisabledSourceConflict(t *testing.T) {
	disabled := true
	calls := 0
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if disabled {
			w.WriteHeader(409)
		}
	})
	if err := c.PostBatch(context.Background(), Batch{}); err == nil {
		t.Fatal("missing disabled error")
	}
	disabled = false
	if err := c.PostBatch(context.Background(), Batch{}); err != nil {
		t.Fatal(err)
	}
	if calls != 3 || len(queuedPaths(t, c)) != 0 {
		t.Fatal("disabled source did not recover")
	}
}

func TestClientRetainsAmbiguousAcknowledgement(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>login</html>")) }))
	defer srv.Close()
	c := Client{URL: srv.URL, HTTPClient: srv.Client(), SpoolDir: filepath.Join(t.TempDir(), "queue"), SpoolMaxBytes: 1024}
	if err := c.PostBatch(context.Background(), Batch{}); err == nil {
		t.Fatal("HTML accepted")
	}
	if len(queuedPaths(t, c)) != 1 {
		t.Fatal("ambiguous entry lost")
	}
	data, _ := os.ReadFile(queuedPaths(t, c)[0])
	var entry queuedBatch
	_ = json.Unmarshal(data, &entry)
	if len(entry.Rejected) != 0 || len(entry.Parts) != 1 {
		t.Fatal("ambiguous response permanently rejected")
	}
}

func TestSpoolFullRefusesNewCollectionWithoutEvictingOld(t *testing.T) {
	c := spoolClient(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	_ = c.PostBatch(context.Background(), Batch{})
	path := queuedPaths(t, c)[0]
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c.SpoolMaxBytes = int64(len(before) + 1)
	if err := c.PostBatch(context.Background(), Batch{}); err == nil || !strings.Contains(err.Error(), "new collection was not queued") {
		t.Fatalf("overflow: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) || len(queuedPaths(t, c)) != 1 {
		t.Fatal("overflow evicted old data")
	}
}
