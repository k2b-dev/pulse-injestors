package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientPostsBatchWithBearerToken(t *testing.T) {
	var gotAuth string
	var gotIdempotencyKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotIdempotencyKey = r.Header.Get("Idempotency-Key")
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("content-type = %q", r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":0}`))
	}))
	defer srv.Close()

	client := Client{
		URL:            srv.URL,
		Token:          "secret",
		HTTPClient:     srv.Client(),
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	}
	err := client.PostBatch(context.Background(), oneState())
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotIdempotencyKey == "" {
		t.Fatal("missing idempotency key")
	}
}

func TestClientRetriesServerErrors(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporary", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":0}`))
	}))
	defer srv.Close()

	client := Client{
		URL:            srv.URL,
		Token:          "secret",
		HTTPClient:     srv.Client(),
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
	}
	if err := client.PostBatch(context.Background(), oneState()); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d", attempts)
	}
}

func TestClientSplitsLargeBatches(t *testing.T) {
	var sizes []Batch
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var batch Batch
		if err := json.NewDecoder(r.Body).Decode(&batch); err != nil {
			t.Fatal(err)
		}
		if len(batch.Metrics) > maxItemsPerIngestCollection || len(batch.States) > maxItemsPerIngestCollection || len(batch.Events) > maxItemsPerIngestCollection {
			t.Fatalf("oversized batch: metrics=%d states=%d events=%d", len(batch.Metrics), len(batch.States), len(batch.Events))
		}
		if batchItems(batch) > maxItemsPerIngestPayload {
			t.Fatalf("too many total items: %d", batchItems(batch))
		}
		sizes = append(sizes, batch)
		_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":0}`))
	}))
	defer srv.Close()

	client := Client{
		URL:            srv.URL,
		Token:          "secret",
		HTTPClient:     srv.Client(),
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	}
	if err := client.PostBatch(context.Background(), Batch{
		Metrics: make([]Metric, 613),
		States:  make([]State, 1617),
	}); err != nil {
		t.Fatal(err)
	}
	if len(sizes[0].Metrics) != 500 {
		t.Fatalf("first part metrics = %d", len(sizes[0].Metrics))
	}
	var metrics, states int
	for _, part := range sizes {
		metrics += len(part.Metrics)
		states += len(part.States)
	}
	if metrics != 613 || states != 1617 {
		t.Fatalf("posted totals = metrics:%d states:%d", metrics, states)
	}
}

func TestClientUsesStableIdempotencyKeyAcrossRetries(t *testing.T) {
	var keys []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys = append(keys, r.Header.Get("Idempotency-Key"))
		if len(keys) == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":0}`))
	}))
	defer srv.Close()

	client := Client{URL: srv.URL, Token: "secret", HTTPClient: srv.Client(), MaxRetries: 1, InitialBackoff: time.Millisecond}
	if err := client.PostBatch(context.Background(), Batch{States: []State{{Key: "system.online", Value: true}}}); err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0] == "" || keys[0] != keys[1] {
		t.Fatalf("idempotency keys = %#v", keys)
	}
}

func TestSplitBatchHonorsByteLimit(t *testing.T) {
	parts, err := splitBatch(Batch{Events: []Event{
		{Kind: "test.event", Payload: map[string]any{"value": "aaaaaaaaaaaaaaaa"}},
		{Kind: "test.event", Payload: map[string]any{"value": "bbbbbbbbbbbbbbbb"}},
	}}, 500, 1500, 140)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("parts = %d", len(parts))
	}
}

func TestClientPausesRedirectWithoutFollowing(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path == "/login" {
			w.WriteHeader(200)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	}))
	defer srv.Close()
	client := Client{URL: srv.URL, HTTPClient: srv.Client()}
	if result := client.Send(context.Background(), []byte(`{}`)); result.Outcome != Paused {
		t.Fatalf("redirect = %+v", result)
	}
	if calls != 1 {
		t.Fatalf("followed redirect: %d requests", calls)
	}
}

func oneState() Batch {
	return Batch{States: []State{{Key: "system.online", Value: true, Timestamp: time.Unix(1_800_000_000, 0).UTC()}}}
}

func TestSendClassifiesPulseResponses(t *testing.T) {
	cases := []struct {
		status  int
		body    string
		retry   string
		outcome Outcome
		code    string
		after   time.Duration
	}{
		{200, `{"metrics":1,"events":0,"states":0}`, "", Delivered, "", 0},
		{200, `{"ok":true}`, "", Retry, "", 0},
		{400, `{"message":"Metric type and unit must match the existing definition","code":"BAD_INPUT"}`, "", Rejected, "BAD_INPUT", 0},
		{413, `not json`, "", Rejected, "", 0},
		{401, `{"message":"Authentication required"}`, "", Paused, "", 0},
		{403, `{"message":"forbidden"}`, "", Paused, "", 0},
		{404, `{"message":"Ingest source not found","code":"NOT_FOUND"}`, "", Paused, "NOT_FOUND", 0},
		{409, `{"code":"PULSE_BASE_STORAGE_FULL","message":"full"}`, "", Paused, "PULSE_BASE_STORAGE_FULL", 0},
		{409, `{"message":"Idempotency key already used"}`, "", Rejected, "", 0},
		{409, `{"message":"IDEMPOTENCY KEY conflict"}`, "", Rejected, "", 0},
		{300, ``, "", Paused, "", 0},
		{304, ``, "", Paused, "", 0},
		{307, ``, "", Paused, "", 0},
		{308, ``, "", Paused, "", 0},
		{408, ``, "", Retry, "", 0},
		{429, `{"code":"PULSE_BASE_THROTTLED","message":"Retry after the indicated delay."}`, "28", Retry, "PULSE_BASE_THROTTLED", 28 * time.Second},
		{503, `{"code":"PULSE_STORE_DISK_LOW","message":"low"}`, "300", Retry, "PULSE_STORE_DISK_LOW", 300 * time.Second},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tc.retry != "" {
				w.Header().Set("Retry-After", tc.retry)
			}
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		stats := &Stats{}
		result := Client{URL: srv.URL, Token: "t", HTTPClient: srv.Client(), Stats: stats}.Send(context.Background(), []byte(`{}`))
		srv.Close()
		if result.Outcome != tc.outcome || result.Code != tc.code || result.RetryAfter != tc.after || result.Status != tc.status {
			t.Errorf("%d %s: got %+v", tc.status, tc.body, result)
		}
		if tc.outcome != Delivered && !strings.Contains(result.Error(), fmt.Sprint(tc.status)) {
			t.Errorf("%d: error %q lacks status", tc.status, result.Error())
		}
		if got := stats.Snapshot(); (tc.outcome == Delivered) != (got.Delivered == 1 && got.Failures == 0) {
			t.Errorf("%d: stats %+v", tc.status, got)
		}
	}
}

func TestSendReportsPulseMessageWithoutToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"code":"BAD_INPUT","message":"states[0] (key \"x\") has timestamp 2030-01-01T00:00:00.000Z\nmore"}`))
	}))
	defer srv.Close()
	result := Client{URL: srv.URL, Token: "very-secret-token", HTTPClient: srv.Client()}.Send(context.Background(), []byte(`{}`))
	msg := result.Error()
	if !strings.Contains(msg, "BAD_INPUT") || !strings.Contains(msg, "2030-01-01") || strings.Contains(msg, "\n") || strings.Contains(msg, "very-secret-token") {
		t.Fatalf("error = %q", msg)
	}
}

func TestSendEstimatesClockSkewFromDateHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().Add(-3*time.Minute).UTC().Format(http.TimeFormat))
		_, _ = w.Write([]byte(`{"metrics":0,"events":0,"states":1}`))
	}))
	defer srv.Close()
	stats := &Stats{}
	Client{URL: srv.URL, HTTPClient: srv.Client(), Stats: stats}.Send(context.Background(), []byte(`{}`))
	got := stats.Snapshot()
	if !got.SkewKnown || got.Skew > -170*time.Second || got.Skew < -190*time.Second {
		t.Fatalf("skew = %+v", got)
	}
}

func TestSplitBatchKeepsEveryItemWithinExactByteLimit(t *testing.T) {
	var batch Batch
	for i := 0; i < 300; i++ {
		batch.Metrics = append(batch.Metrics, Metric{Name: "m", Type: "gauge", Value: float64(i), Timestamp: time.Unix(int64(i), 0).UTC(), Dimensions: map[string]string{"i": strings.Repeat("x", i%40)}})
		batch.Events = append(batch.Events, Event{Kind: "e", Timestamp: time.Unix(int64(i), 0).UTC(), Attributes: map[string]any{"<html>": "&"}})
	}
	for _, limit := range []int{300, 1000, 7000, 50000} {
		parts, err := splitBatch(batch, 120, 200, limit)
		if err != nil {
			t.Fatal(err)
		}
		var metrics, events int
		for _, part := range parts {
			body, _ := json.Marshal(part)
			if len(body) > limit || len(part.Metrics) > 120 || batchItems(part) > 200 || batchItems(part) == 0 {
				t.Fatalf("limit %d: part %d bytes, %d metrics, %d items", limit, len(body), len(part.Metrics), batchItems(part))
			}
			metrics += len(part.Metrics)
			events += len(part.Events)
		}
		if metrics != 300 || events != 300 {
			t.Fatalf("limit %d: lost items %d/%d", limit, metrics, events)
		}
	}
	if _, err := splitBatch(Batch{Events: []Event{{Kind: strings.Repeat("k", 500)}}}, 500, 1500, 200); err == nil {
		t.Fatal("oversized single item accepted")
	}
	if parts, _ := SplitBatch(Batch{}); len(parts) != 0 {
		t.Fatal("empty batch produced a part")
	}
}

func TestRetryAfterDatesAndBounds(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("30", now); got != 30*time.Second {
		t.Fatal(got)
	}
	if got := parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); got != 90*time.Second {
		t.Fatal(got)
	}
	if got := parseRetryAfter("999999999", now); got != 24*time.Hour {
		t.Fatal(got)
	}
	if got := parseRetryAfter("soon", now); got != 0 {
		t.Fatal(got)
	}
}
