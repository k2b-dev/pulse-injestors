package pbs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

func TestDebugAPIArgs(t *testing.T) {
	args, err := debugAPIArgs("/nodes/localhost/tasks?limit=50")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	want := "api get /nodes/localhost/tasks --limit 50 --output-format json"
	if got != want {
		t.Fatalf("args = %q", got)
	}
}

func TestCollectorEmitsDatastoresJobsAndTasks(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"api get /version --output-format json":                `{"version":"4.2","release":"4.2-1","repoid":"abc"}`,
		"api get /status/datastore-usage --output-format json": `[{"store":"main","used":1000,"total":4000,"avail":3000,"estimated-full-date":2000000000}]`,
		"api get /admin/datastore/main/snapshots --output-format json": `[
			{"backup-type":"vm","backup-id":"100","backup-time":1700000000},
			{"backup-type":"ct","backup-id":"101","backup-time":1700000500}
		]`,
		"api get /admin/gc --output-format json":     `[{"store":"main","schedule":"daily","last-run-state":"OK","last-run-endtime":1700000600,"next-run":2000000000}]`,
		"api get /admin/prune --output-format json":  `[{"id":"prune-main","store":"main","schedule":"daily","last-run-state":"OK","last-run-endtime":1700000700}]`,
		"api get /admin/sync --output-format json":   `[{"id":"sync-main","store":"main","disable":true,"last-run-state":"ERROR: sync failed","last-run-endtime":1700000800}]`,
		"api get /admin/verify --output-format json": `[{"id":"verify-main","store":"main","schedule":"weekly","last-run-state":"OK","last-run-endtime":1700000900}]`,
		"api get /nodes/localhost/tasks --limit 50 --output-format json": `[
			{"upid":"UPID:pbs:1","node":"pbs","worker_type":"backup","worker_id":"vm/100","user":"root@pam","status":"OK","starttime":1700000000,"endtime":1700000300},
			{"upid":"UPID:pbs:2","node":"pbs","worker_type":"syncjob","worker_id":"sync-main","user":"root@pam","status":"ERROR: failed","starttime":1700000400,"endtime":1700000500}
		]`,
	}}

	batch, err := Collector{
		CommandPath: "proxmox-backup-debug",
		Runner:      runner,
		Timeout:     time.Second,
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
	if countMetric(batch.Metrics, "pbs.datastore.bytes.usage") != 1 {
		t.Fatalf("missing datastore usage: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "pbs.datastore.bytes.usage", "pbs-datastore", "pbs-datastore:pbs:main") != 1 {
		t.Fatalf("missing datastore-scoped usage metric: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "pbs.datastore.snapshots.by_type") != 2 {
		t.Fatalf("missing snapshot type metrics: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "pbs.datastore.snapshots.total", "pbs-datastore", "pbs-datastore:pbs:main") != 1 {
		t.Fatalf("missing datastore-scoped snapshot metric: %#v", batch.Metrics)
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
	if countStateEntity(batch.States, "pbs.job.enabled", "pbs-job", "pbs-job:pbs:sync:sync-main") != 1 {
		t.Fatalf("missing job-scoped enabled state: %#v", batch.States)
	}
	if countMetricEntity(batch.Metrics, "pbs.job.last_run.age", "pbs-job", "pbs-job:pbs:verify:verify-main") != 1 {
		t.Fatalf("missing job-scoped last-run metric: %#v", batch.Metrics)
	}
	if countEventEntity(batch.Events, "pbs.job.failed", "pbs-job", "pbs-job:pbs:sync:sync-main") != 1 {
		t.Fatalf("missing job-scoped failure event: %#v", batch.Events)
	}
	if countStateEntity(batch.States, "pbs.datastore.present", "pbs-datastore", "pbs-datastore:pbs:main") != 1 {
		t.Fatalf("missing datastore-scoped present state: %#v", batch.States)
	}
}

func TestCollectorGracefullyReportsMissingCommand(t *testing.T) {
	batch, err := Collector{
		CommandPath: "pulse-test-missing-proxmox-backup-debug",
		Timeout:     time.Second,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pbs",
		EntityType: "proxmox-backup-server",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if countState(batch.States, "pbs.available") != 1 {
		t.Fatalf("missing pbs availability state: %#v", batch.States)
	}
	if countEvent(batch.Events, "pbs.config.failed") != 0 {
		t.Fatalf("unexpected config failure event: %#v", batch.Events)
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

type fakeRunner struct {
	responses map[string]string
}

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	response, ok := f.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected command: %s", key)
	}
	return []byte(response), nil
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

func countMetricEntity(metrics []pulse.Metric, name, entityType, entityID string) int {
	count := 0
	for _, metric := range metrics {
		if metric.Name == name && metric.EntityType == entityType && metric.EntityID == entityID {
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

func countEventEntity(events []pulse.Event, kind, entityType, entityID string) int {
	count := 0
	for _, event := range events {
		if event.Kind == kind && event.EntityType == entityType && event.EntityID == entityID {
			count++
		}
	}
	return count
}

func countStateEntity(states []pulse.State, key, entityType, entityID string) int {
	count := 0
	for _, state := range states {
		if state.Key == key && state.EntityType == entityType && state.EntityID == entityID {
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
