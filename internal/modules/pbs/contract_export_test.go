package pbs

import (
	"context"
	"encoding/json"
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
		Resource:  pulse.ResourceRef{Type: "proxmox-backup-server", ID: "pbs"},
		Timestamp: time.Unix(1700001000, 0).UTC(),
	})
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
		if err := os.WriteFile(filepath.Join(directory, "pbs.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
