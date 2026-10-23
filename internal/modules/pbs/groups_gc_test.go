package pbs

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type runnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f runnerFunc) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return f(ctx, name, args...)
}

func metricValue(t *testing.T, batch pulse.Batch, name string) float64 {
	t.Helper()
	for _, metric := range batch.Metrics {
		if metric.Name == name {
			return metric.Value
		}
	}
	t.Fatalf("missing metric %s", name)
	return 0
}

func TestGroupsBreadthFirstCountsAndPartialResults(t *testing.T) {
	for _, failAt := range []string{"", "a", "a/deep"} {
		t.Run("failure_"+failAt, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var visited []string
			runner := runnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				switch args[2] {
				case "/admin/datastore":
					return []byte(`[{"store":"main"}]`), nil
				case "/admin/datastore/main/namespace":
					return []byte(`[{"ns":"a/deep"},{"ns":"z"},{"ns":"a"},{"ns":""},{"ns":"a"}]`), nil
				case "/admin/datastore/main/groups":
					ns := ""
					if args[3] == "--ns" {
						ns = args[4]
					}
					visited = append(visited, ns)
					if failAt != "" && ns == failAt {
						if ns == "a/deep" {
							cancel()
							return nil, ctx.Err()
						}
						return nil, context.DeadlineExceeded
					}
					return []byte(`[{"backup-type":"host","backup-id":"client","last-backup":1700000900,"backup-count":7}]`), nil
				default:
					t.Fatalf("unexpected API path (snapshot details are forbidden): %s", args[2])
					return nil, nil
				}
			})
			batch, err := (&Collector{Runner: runner, Part: "groups"}).Collect(ctx, reviewScope())
			if err != nil || !reflect.DeepEqual(visited, []string{"", "a", "z", "a/deep"}) {
				t.Fatal(err, visited)
			}
			wantCount, wantEvents := 28.0, 0
			if failAt != "" {
				wantCount, wantEvents = 21, 1
			}
			if metricValue(t, batch, "pbs.datastore.snapshots.total") != wantCount || metricValue(t, batch, "pbs.datastore.snapshots.by_type") != wantCount || stateValue(t, batch, "pbs.groups.complete", "") != (failAt == "") || countEvent(batch.Events, "pbs.datastore.groups.failed") != wantEvents {
				t.Fatal(batch)
			}
			if countMetric(batch.Metrics, "pbs.datastore.verify.failed_snapshots") != 0 {
				t.Fatal("removed verification metric emitted")
			}
		})
	}
}

func TestGroupsBudgetStopsCallsAndPreservesResults(t *testing.T) {
	calls := 0
	runner := runnerFunc(func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		switch calls {
		case 1:
			return []byte(`[{"store":"main"},{"store":"second"}]`), nil
		case 2:
			return []byte(`[{"ns":"child"},{"ns":"later"}]`), nil
		case 3:
			return []byte(`[{"backup-type":"host","backup-id":"client","last-backup":1700000900,"backup-count":9}]`), nil
		case 4:
			<-ctx.Done()
			return nil, ctx.Err()
		default:
			t.Fatalf("called after budget exhausted: %v", args)
			return nil, nil
		}
	})
	batch, err := (&Collector{Runner: runner, Part: "groups", GroupsTimeout: 50 * time.Millisecond, ListTimeout: time.Hour}).Collect(context.Background(), reviewScope())
	if err != nil || calls != 4 || stateValue(t, batch, "pbs.groups.complete", "") != false || metricValue(t, batch, "pbs.datastore.snapshots.total") != 9 || countMetric(batch.Metrics, "pbs.group.last_snapshot.age") != 1 || countEvent(batch.Events, "pbs.datastore.groups.failed") != 1 {
		t.Fatal(err, calls, batch)
	}
}

func TestGroupsDiscoveryFailureIncomplete(t *testing.T) {
	for _, discovery := range []bool{false, true} {
		runner := &fakeRunner{responses: map[string]string{}}
		if discovery {
			runner.responses["api get /admin/datastore --output-format json"] = `[{"store":"main"}]`
		}
		batch, _ := (&Collector{Runner: runner, Part: "groups"}).Collect(context.Background(), reviewScope())
		if stateValue(t, batch, "pbs.groups.complete", "") != false || len(batch.Events) != 1 {
			t.Fatal(batch)
		}
	}
}

func TestGroupStaleThreshold(t *testing.T) {
	for _, threshold := range []time.Duration{0, 24 * time.Hour} {
		limit := threshold
		if limit == 0 {
			limit = 168 * time.Hour
		}
		for _, age := range []time.Duration{-time.Second, limit - time.Second, limit, limit + time.Second, 79 * 24 * time.Hour} {
			runner := &fakeRunner{responses: map[string]string{
				"api get /admin/datastore --output-format json":                              `[ {"store":"main"} ]`,
				"api get /admin/datastore/main/namespace --max-depth 7 --output-format json": `[]`,
				"api get /admin/datastore/main/groups --output-format json":                  fmt.Sprintf(`[{"backup-type":"host","backup-id":"client","backup-count":2,"last-backup":%d}]`, reviewScope().Timestamp.Add(-age).Unix()),
			}}
			batch, _ := (&Collector{Runner: runner, Part: "groups", StaleAfter: threshold}).Collect(context.Background(), reviewScope())
			if stateValue(t, batch, "pbs.group.stale", "") != (age > limit) {
				t.Fatal(threshold, age, batch.States)
			}
		}
	}
}

func TestGCResultFixtures(t *testing.T) {
	for _, tc := range []struct {
		name, list, fallback, want string
		failed                     bool
	}{
		{"list_ok", `{"store":"main","last-run-state":"OK","last-run-endtime":1700000000}`, "", "ok", false},
		{"fallback_ok", `{"store":"main"}`, `{"last-run-state":"OK","upid":"UPID:gc:1","last-run-endtime":1700000000}`, "ok", false},
		{"fallback_warning", `{"store":"main"}`, `{"last-run-state":"WARNINGS: 2","upid":"UPID:gc:1"}`, "warnings", false},
		{"fallback_failure", `{"store":"main"}`, `{"last-run-state":"permission denied","upid":"UPID:gc:1"}`, "error", false},
		{"list_running", `{"store":"main","last-run-upid":"UPID:gc:1"}`, "", "running", false},
		{"list_running_state", `{"store":"main","last-run-state":"running"}`, "", "running", false},
		{"running_flag", `{"store":"main","running":true,"last-run-state":"OK"}`, "", "running", false},
		{"fallback_running", `{"store":"main"}`, `{"running":true,"upid":"UPID:gc:1"}`, "running", false},
		{"never", `{"store":"main"}`, `{}`, "never", false},
		{"never_null_fields", `{"store":"main","last-run-state":null}`, `{"upid":null,"last-run-state":null}`, "never", false},
		{"unparseable_list_state", `{"store":"main","last-run-state":42}`, "", "unknown", true},
		{"unparseable_fallback_state", `{"store":"main"}`, `{"last-run-state":{}}`, "unknown", true},
		{"unparseable_json", `{"store":"main"}`, `{`, "unknown", true},
		{"missing_object", `{"store":"main"}`, `null`, "unknown", true},
		{"fallback_failed", `{"store":"main"}`, "", "unknown", true},
		{"upid_only", `{"store":"main"}`, `{"upid":"UPID:gc:1"}`, "unknown", false},
		{"completed_without_result", `{"store":"main","last-run-upid":"UPID:gc:1","last-run-endtime":1700000000}`, `{}`, "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := jobFixture()
			runner.responses["api get /admin/gc --output-format json"] = "[" + tc.list + "]"
			if tc.fallback != "" {
				runner.responses["api get /admin/datastore/main/gc --output-format json"] = tc.fallback
			}
			batch, _ := (&Collector{Runner: runner, Part: "jobs"}).Collect(context.Background(), reviewScope())
			if stateValue(t, batch, "pbs.datastore.gc.last_run.result", "") != tc.want || (countEvent(batch.Events, "pbs.datastore.gc.failed") != 0) != tc.failed {
				t.Fatal(batch)
			}
			if tc.fallback == "" && !tc.failed {
				for _, call := range runner.calls {
					if strings.Contains(call, "/main/gc") {
						t.Fatal("unnecessary GC fallback", call)
					}
				}
			}
		})
	}
}

func TestGroupsDoNotInventTotalsWhenEveryQueryFails(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"api get /admin/datastore --output-format json":                              `[{"store":"main"}]`,
		"api get /admin/datastore/main/namespace --max-depth 7 --output-format json": `[]`,
	}}
	batch, _ := (&Collector{Runner: runner, Part: "groups"}).Collect(context.Background(), reviewScope())
	if stateValue(t, batch, "pbs.groups.complete", "") != false || countMetric(batch.Metrics, "pbs.datastore.snapshots.total") != 0 {
		t.Fatal(batch)
	}
}

func TestVerifyFailureWithoutSnapshotDetails(t *testing.T) {
	runner := jobFixture()
	runner.responses["api get /admin/sync --output-format json"] = `[]`
	runner.responses["api get /admin/verify --output-format json"] = `[{"id":"verify-main","store":"main","last-run-state":"verification failed - please check the log for details","last-run-endtime":1700000000,"last-run-upid":"UPID:verify:1"}]`
	batch, _ := (&Collector{Runner: runner, Part: "jobs"}).Collect(context.Background(), reviewScope())
	if stateValue(t, batch, "pbs.job.last_run.result", "backup:verify:verify-main") != "error" || countEvent(batch.Events, "pbs.job.failed") != 1 {
		t.Fatal(batch)
	}
	for _, event := range batch.Events {
		if event.Kind == "pbs.job.failed" && (event.CorrelationID != "UPID:verify:1" || event.Dimensions["kind"] != "verify") {
			t.Fatal(event)
		}
	}
}

func TestDefaultGroupTimeouts(t *testing.T) {
	runner := &deadlineRunner{}
	collector := &Collector{Runner: runner, Part: "groups"}
	if collector.CollectTimeout() != 300*time.Second {
		t.Fatal(collector.CollectTimeout())
	}
	client, err := collector.client()
	if err != nil {
		t.Fatal(err)
	}
	var rows []any
	if err := client.get(context.Background(), "/admin/datastore/main/groups", &rows); err != nil {
		t.Fatal(err)
	}
	if runner.timeout > 120*time.Second || runner.timeout < 119*time.Second {
		t.Fatal(runner.timeout)
	}
}
