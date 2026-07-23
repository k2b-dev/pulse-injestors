package pulse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	client := Client{
		URL:            srv.URL,
		Token:          "secret",
		HTTPClient:     srv.Client(),
		MaxRetries:     0,
		InitialBackoff: time.Millisecond,
	}
	err := client.PostBatch(context.Background(), Batch{
		Metrics: []Metric{},
		Events:  []Event{},
		States:  []State{},
	})
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
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := Client{
		URL:            srv.URL,
		Token:          "secret",
		HTTPClient:     srv.Client(),
		MaxRetries:     1,
		InitialBackoff: time.Millisecond,
	}
	if err := client.PostBatch(context.Background(), Batch{}); err != nil {
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
		w.WriteHeader(http.StatusNoContent)
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
		w.WriteHeader(http.StatusNoContent)
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
