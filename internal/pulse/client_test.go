package pulse

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientPostsBatchWithBearerToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
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
