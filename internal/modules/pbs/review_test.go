package pbs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

func reviewScope() monitoring.Scope {
	return monitoring.Scope{Resource: pulse.ResourceRef{Type: "proxmox-backup-server", ID: "backup"}, Timestamp: time.Unix(1700001000, 0).UTC()}
}
func stateValue(t *testing.T, batch pulse.Batch, key, id string) any {
	t.Helper()
	for _, s := range batch.States {
		if s.Key == key && (id == "" || s.Resource.ID == id) {
			return s.Value
		}
	}
	t.Fatalf("missing %s on %s", key, id)
	return nil
}

func TestNestedNamespacesAndGroupLastBackup(t *testing.T) {
	for _, rootListed := range []bool{false, true} {
		namespaces := `[{"ns":"k8s-1/volumes"},{"ns":"k8s-1"}]`
		if rootListed {
			namespaces = `[{"ns":""},{"ns":"k8s-1/volumes"},{"ns":"k8s-1"}]`
		}
		runner := &fakeRunner{responses: map[string]string{
			"api get /admin/datastore --output-format json":                              `[{"store":"main"}]`,
			"api get /admin/datastore/main/namespace --max-depth 7 --output-format json": namespaces,
		}}
		for _, ns := range []string{"", "k8s-1", "k8s-1/volumes"} {
			flags := ""
			if ns != "" {
				flags = " --ns " + ns
			}
			runner.responses["api get /admin/datastore/main/groups"+flags+" --output-format json"] = `[{"backup-type":"vm","backup-id":"101","backup-count": 1, "last-backup": 1700000900}]`
		}
		batch, _ := (&Collector{Runner: runner, Part: "groups"}).Collect(context.Background(), reviewScope())
		if len(batch.Events) != 0 || countMetric(batch.Metrics, "pbs.group.last_snapshot.age") != 3 {
			t.Fatal(batch)
		}
		for _, m := range batch.Metrics {
			if m.Name == "pbs.group.last_snapshot.age" {
				ns := m.Dimensions["namespace"]
				id := "backup:main:vm%2F101"
				label := "vm/101 on main"
				if ns != "" {
					id = "backup:main:" + strings.ReplaceAll(ns, "/", "%2F") + ":vm%2F101"
					label = "vm/101 in " + ns + " on main"
				}
				if m.Value != 100 || m.Resource.ID != id || m.Resource.Label != label {
					t.Fatal(m)
				}
				if stateValue(t, batch, "pbs.group.last_snapshot.time", id) != "2023-11-14T22:28:20Z" {
					t.Fatal(batch)
				}
			}
			if m.Name == "pbs.datastore.snapshots.total" && m.Value != 3 {
				t.Fatal(m)
			}
		}
		if err := validation.Batch(batch); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGroupCapAcrossNamespacesStillCountsSnapshots(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"api get /admin/datastore --output-format json":                              `[{"store":"main"}]`,
		"api get /admin/datastore/main/namespace --max-depth 7 --output-format json": `[{"ns":"k8s-1"},{"ns":"k8s-1/volumes"}]`,
	}}
	for _, ns := range []string{"", "k8s-1", "k8s-1/volumes"} {
		var groups []string
		for i := 0; i < 1000; i++ {
			groups = append(groups, fmt.Sprintf(`{"backup-type":"vm","backup-id":"%d","backup-count": 1, "last-backup": 1700000000}`, i))
		}
		flags := ""
		if ns != "" {
			flags = " --ns " + ns
		}
		runner.responses["api get /admin/datastore/main/groups"+flags+" --output-format json"] = "[" + strings.Join(groups, ",") + "]"
	}
	batch, _ := (&Collector{Runner: runner, Part: "groups"}).Collect(context.Background(), reviewScope())
	if len(batch.Events) != 0 || countMetric(batch.Metrics, "pbs.group.last_snapshot.age") != 2000 || stateValue(t, batch, "pbs.groups.truncated", "") != true || stateValue(t, batch, "pbs.groups.complete", "") != true {
		t.Fatal("namespace cap failed", batch.Events)
	}
	for _, m := range batch.Metrics {
		if m.Name == "pbs.datastore.snapshots.total" && m.Value != 3000 {
			t.Fatal(m)
		}
	}
}

func TestDatastoreAvailabilityRecovers(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{}}
	c := &Collector{Runner: runner, Part: "datastores"}
	for _, success := range []bool{false, true, false, true} {
		if success {
			runner.responses["api get /status/datastore-usage --output-format json"] = `[]`
		} else {
			delete(runner.responses, "api get /status/datastore-usage --output-format json")
		}
		batch, _ := c.Collect(context.Background(), reviewScope())
		if stateValue(t, batch, "pbs.available", "backup") != success {
			t.Fatal(batch)
		}
	}
}

func jobFixture() *fakeRunner {
	return &fakeRunner{responses: map[string]string{
		"api get /admin/gc --output-format json":                         `[]`,
		"api get /admin/prune --output-format json":                      `[]`,
		"api get /admin/sync --output-format json":                       `[]`,
		"api get /admin/verify --output-format json":                     `[]`,
		"api get /tape/backup --output-format json":                      `[]`,
		"api get /nodes/localhost/tasks --limit 50 --output-format json": `[]`,
	}}
}

func TestJobAndTaskDedupAndPruning(t *testing.T) {
	runner := jobFixture()
	runner.responses["api get /admin/sync --output-format json"] = `[{"id":"copy","last-run-state":"ERROR","last-run-endtime":1700000000,"last-run-upid":"UPID:job:1"}]`
	var tasks []Task
	for i := 0; i < 12; i++ {
		tasks = append(tasks, Task{UPID: fmt.Sprintf("UPID:task:%d", i), Status: "ERROR", EndTime: 1700000000})
	}
	data, _ := json.Marshal(tasks)
	runner.responses["api get /nodes/localhost/tasks --limit 50 --output-format json"] = string(data)
	c := &Collector{Runner: runner, Part: "jobs"}
	for cycle, want := range []int{10, 2, 0} {
		batch, _ := c.Collect(context.Background(), reviewScope())
		jobs := 0
		if cycle == 0 {
			jobs = 1
		}
		if countEvent(batch.Events, "pbs.task.failed") != want || countEvent(batch.Events, "pbs.job.failed") != jobs {
			t.Fatal(batch.Events)
		}
		for _, e := range batch.Events {
			if e.Kind == "pbs.job.failed" && e.CorrelationID != "UPID:job:1" {
				t.Fatal(e)
			}
		}
	}
	runner.responses["api get /nodes/localhost/tasks --limit 50 --output-format json"] = `[]`
	runner.responses["api get /admin/sync --output-format json"] = strings.ReplaceAll(runner.responses["api get /admin/sync --output-format json"], "UPID:job:1", "UPID:job:2")
	batch, _ := c.Collect(context.Background(), reviewScope())
	if len(c.seenTasks) != 0 || len(c.seenJobs) != 1 || countEvent(batch.Events, "pbs.job.failed") != 1 {
		t.Fatal(batch.Events, c.seenTasks, c.seenJobs)
	}
}

func TestJobResultsAndBoundedText(t *testing.T) {
	for raw, result := range map[string]string{"OK": "ok", "ok": "ok", "WARNINGS: 3": "warnings", "": "unknown", strings.Repeat("界", 600): "error"} {
		runner := jobFixture()
		rows := []map[string]any{{"id": "job", "store": "main", "last-run-state": raw, "last-run-endtime": 1700000000, "last-run-upid": "UPID:job:1", "schedule": strings.Repeat("界", 600)}}
		data, _ := json.Marshal(rows)
		runner.responses["api get /admin/gc --output-format json"] = string(data)
		runner.responses["api get /admin/sync --output-format json"] = string(data)
		batch, _ := (&Collector{Runner: runner, Part: "jobs"}).Collect(context.Background(), reviewScope())
		if stateValue(t, batch, "pbs.job.last_run.result", "") != result || stateValue(t, batch, "pbs.datastore.gc.last_run.result", "") != result {
			t.Fatal(batch.States)
		}
		want := 0
		if result == "error" {
			want = 1
		}
		if countEvent(batch.Events, "pbs.job.failed") != want {
			t.Fatal(batch.Events)
		}
		for _, s := range batch.States {
			if text, ok := s.Value.(string); ok && utf8.RuneCountInString(text) > 500 {
				t.Fatal(s)
			}
		}
		if result == "error" && utf8.RuneCountInString(stateValue(t, batch, "pbs.job.last_run.state", "").(string)) != 500 {
			t.Fatal("raw state not retained")
		}
		if err := validation.Batch(batch); err != nil {
			t.Fatal(err)
		}
	}
	b := monitoring.NewBuilder(reviewScope())
	emitError(b, "pbs.tasks.failed", "tasks", nil, fmt.Errorf("%s", strings.Repeat("界", 600)))
	if utf8.RuneCountInString(b.Batch().Events[0].Attributes["error"].(string)) != 500 {
		t.Fatal("error not bounded")
	}
}

type deadlineRunner struct{ timeout time.Duration }

func (r *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	deadline, _ := ctx.Deadline()
	r.timeout = time.Until(deadline)
	return []byte(`[]`), nil
}
func TestListTimeoutAndNamespaceArgs(t *testing.T) {
	r := &deadlineRunner{}
	client, _ := (Collector{Runner: r, Timeout: 2 * time.Second, ListTimeout: 45 * time.Second}).client()
	for path, want := range map[string]time.Duration{"/version": 2 * time.Second, "/admin/datastore/main/namespace?max-depth=7": 45 * time.Second, "/admin/datastore/main/groups?ns=k8s-1%2Fvolumes": 45 * time.Second} {
		var rows []any
		if err := client.get(context.Background(), path, &rows); err != nil {
			t.Fatal(err)
		}
		if r.timeout > want || r.timeout < want-time.Second {
			t.Fatalf("%s: %s", path, r.timeout)
		}
	}
	args, err := debugAPIArgs("/admin/datastore/main/groups?ns=k8s-1%2Fvolumes")
	if err != nil || strings.Join(args, " ") != "api get /admin/datastore/main/groups --ns k8s-1/volumes --output-format json" {
		t.Fatal(args, err)
	}
}

func TestExecRunnerSeparatesAndBoundsOutput(t *testing.T) {
	out, err := (execRunner{}).Run(context.Background(), "sh", "-c", `printf '[]'; printf warning >&2`)
	if err != nil || string(out) != "[]" {
		t.Fatalf("%q %v", out, err)
	}
	_, err = (execRunner{}).Run(context.Background(), "sh", "-c", `head -c 2000 /dev/zero | tr '\000' x >&2; exit 1`)
	if err == nil || len(err.Error()) > 1100 || !strings.Contains(err.Error(), strings.Repeat("x", 1024)) {
		t.Fatal(err)
	}
	_, err = (execRunner{}).Run(context.Background(), "sh", "-c", `head -c 67108865 /dev/zero`)
	if err == nil || !strings.Contains(err.Error(), "stdout exceeds 67108864 bytes") {
		t.Fatal(err)
	}
}
