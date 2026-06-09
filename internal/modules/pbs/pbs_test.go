package pbs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
	"github.com/valentinkolb/pulse-injestors/internal/validation"
)

func TestNormalizeBaseURL(t *testing.T) {
	got, err := normalizeBaseURL("https://pbs.example:8007")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://pbs.example:8007/api2/json" {
		t.Fatalf("url = %q", got)
	}
	got, err = normalizeBaseURL("https://pbs.example:8007/api2/json/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://pbs.example:8007/api2/json" {
		t.Fatalf("url = %q", got)
	}
}

func TestAuthHeader(t *testing.T) {
	if got := authHeader("root@pam!pulse:secret"); got != "PBSAPIToken=root@pam!pulse:secret" {
		t.Fatalf("auth = %q", got)
	}
	if got := authHeader("PBSAPIToken=root@pam!pulse:secret"); got != "PBSAPIToken=root@pam!pulse:secret" {
		t.Fatalf("auth = %q", got)
	}
}

func TestCollectorEmitsDatastoresJobsAndTasks(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api2/json/version":
			_, _ = w.Write([]byte(`{"data":{"version":"4.2","release":"4.2-1","repoid":"abc"}}`))
		case "/api2/json/status/datastore-usage":
			_, _ = w.Write([]byte(`{"data":[{"store":"main","used":1000,"total":4000,"avail":3000,"estimated-full-date":2000000000}]}`))
		case "/api2/json/admin/datastore/main/snapshots":
			_, _ = w.Write([]byte(`{"data":[
				{"backup-type":"vm","backup-id":"100","backup-time":1700000000},
				{"backup-type":"ct","backup-id":"101","backup-time":1700000500}
			]}`))
		case "/api2/json/admin/gc":
			_, _ = w.Write([]byte(`{"data":[{"store":"main","schedule":"daily","last-run-state":"OK","last-run-endtime":1700000600,"next-run":2000000000}]}`))
		case "/api2/json/admin/prune":
			_, _ = w.Write([]byte(`{"data":[{"id":"prune-main","store":"main","schedule":"daily","last-run-state":"OK","last-run-endtime":1700000700}]}`))
		case "/api2/json/admin/sync":
			_, _ = w.Write([]byte(`{"data":[{"id":"sync-main","store":"main","disable":true,"last-run-state":"ERROR: sync failed","last-run-endtime":1700000800}]}`))
		case "/api2/json/admin/verify":
			_, _ = w.Write([]byte(`{"data":[{"id":"verify-main","store":"main","schedule":"weekly","last-run-state":"OK","last-run-endtime":1700000900}]}`))
		case "/api2/json/nodes/localhost/tasks":
			if r.URL.Query().Get("limit") != "50" {
				t.Fatalf("limit = %q", r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"data":[
				{"upid":"UPID:pbs:1","node":"pbs","worker_type":"backup","worker_id":"vm/100","user":"root@pam","status":"OK","starttime":1700000000,"endtime":1700000300},
				{"upid":"UPID:pbs:2","node":"pbs","worker_type":"syncjob","worker_id":"sync-main","user":"root@pam","status":"ERROR: failed","starttime":1700000400,"endtime":1700000500}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	batch, err := Collector{
		BaseURL:    srv.URL,
		APIToken:   "root@pam!pulse:secret",
		HTTPClient: srv.Client(),
		Timeout:    time.Second,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pbs",
		EntityType: "proxmox-backup-server",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "PBSAPIToken=root@pam!pulse:secret" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if countMetric(batch.Metrics, "pbs.datastore.bytes.usage") != 1 {
		t.Fatalf("missing datastore usage: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "pbs.datastore.snapshots.by_type") != 2 {
		t.Fatalf("missing snapshot type metrics: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "pbs.jobs.total") != 4 {
		t.Fatalf("missing job totals: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "pbs.tasks.by_type_status") != 2 {
		t.Fatalf("missing task type status metrics: %#v", batch.Metrics)
	}
	if countEvent(batch.Events, "pbs.task.failed") != 1 {
		t.Fatalf("missing task failure event: %#v", batch.Events)
	}
	if countEvent(batch.Events, "pbs.job.failed") != 1 {
		t.Fatalf("missing job failure event: %#v", batch.Events)
	}
	if countState(batch.States, "pbs.job.enabled") != 4 {
		t.Fatalf("missing job states: %#v", batch.States)
	}
}

func TestParseJobs(t *testing.T) {
	jobs := parseJobs([]map[string]any{
		{"id": "prune-main", "store": "main", "disable": float64(1), "schedule": "daily", "last-run-state": "OK", "last-run-endtime": float64(1700000000), "next-run": float64(1700003600)},
		{"store": "gc-store", "last-run-state": "ERROR"},
		{"disable": false},
	})
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	if jobs[0].ID != "prune-main" || jobs[0].Store != "main" || !jobs[0].Disabled || jobs[0].Schedule != "daily" || jobs[0].LastRunEndTime != 1700000000 || jobs[0].NextRun != 1700003600 {
		t.Fatalf("first = %#v", jobs[0])
	}
	if jobs[1].ID != "gc-store" || jobs[1].Store != "gc-store" || jobs[1].LastRunState != "ERROR" {
		t.Fatalf("second = %#v", jobs[1])
	}
}

func countMetric(metrics []pulse.Metric, name string) int {
	count := 0
	for _, metric := range metrics {
		if metric.Name == name {
			count++
		}
	}
	return count
}

func countEvent(events []pulse.Event, kind string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind {
			count++
		}
	}
	return count
}

func countState(states []pulse.State, key string) int {
	count := 0
	for _, state := range states {
		if state.Key == key {
			count++
		}
	}
	return count
}
