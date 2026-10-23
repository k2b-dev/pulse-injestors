package proxmox

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

func stateValue(t *testing.T, batch pulse.Batch, key, id string) any {
	t.Helper()
	for _, state := range batch.States {
		if state.Key == key && (id == "" || state.Resource.ID == id) {
			return state.Value
		}
	}
	t.Fatalf("missing %s on %s: %+v", key, id, batch.States)
	return nil
}

func healthFixture() *fakeRunner {
	return &fakeRunner{responses: map[string]string{
		"get /cluster/status --output-format json":            `[{"type":"node","name":"solo","online":1,"local":1}]`,
		"get /cluster/tasks --output-format json":             `[]`,
		"get /cluster/resources --output-format json":         `[]`,
		"get /cluster/ha/status/current --output-format json": `[]`,
	}}
}

func TestPartitionRequiresQuorum(t *testing.T) {
	reporters := 0
	for local := 1; local <= 5; local++ {
		rows := []map[string]any{{"type": "cluster", "name": "lab", "quorate": 0}}
		if local <= 3 {
			rows[0]["quorate"] = 1
		}
		for node := 1; node <= 5; node++ {
			online, isLocal := 0, 0
			if (local <= 3) == (node <= 3) {
				online = 1
			}
			if node == local {
				isLocal = 1
			}
			rows = append(rows, map[string]any{"type": "node", "name": fmt.Sprintf("n%d", node), "nodeid": node, "online": online, "local": isLocal})
		}
		data, _ := json.Marshal(rows)
		runner := healthFixture()
		runner.responses["get /cluster/status --output-format json"] = string(data)
		c := &Collector{Runner: runner, Part: "health"}
		for cycle := 0; cycle < 2; cycle++ {
			batch, _ := c.Collect(context.Background(), fixtureScope())
			id := fmt.Sprintf("lab:n%d", local)
			if got := stateValue(t, batch, "proxmox.node.quorate", id); got != (local <= 3) {
				t.Fatal(got)
			}
			elected := stateValue(t, batch, "proxmox.cluster.elected", id) == true
			if elected && cycle == 0 {
				reporters++
			}
			if elected != (local == 1) {
				t.Fatalf("node %d elected=%v", local, elected)
			}
			if countMetric(batch.Metrics, "proxmox.tasks.recent") != map[bool]int{true: 1, false: 0}[elected] {
				t.Fatal(batch)
			}
		}
	}
	if reporters != 1 {
		t.Fatalf("reporters=%d", reporters)
	}
}

func TestAvailabilityKeepsNodeIdentity(t *testing.T) {
	for _, clustered := range []bool{false, true} {
		runner := healthFixture()
		if clustered {
			runner.responses["get /cluster/status --output-format json"] = `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"solo","local":1,"online":1}]`
		}
		c := &Collector{Runner: runner, Part: "health"}
		good, _ := c.Collect(context.Background(), fixtureScope())
		id := "custom-host-id:solo"
		if clustered {
			id = "lab:solo"
		}
		if stateValue(t, good, "proxmox.available", id) != true {
			t.Fatal(good)
		}
		delete(runner.responses, "get /cluster/status --output-format json")
		bad, _ := c.Collect(context.Background(), fixtureScope())
		if stateValue(t, bad, "proxmox.available", id) != false {
			t.Fatal(bad)
		}
		c.Runner = nil
		c.PveshPath = "/pulse-fixture-missing-command"
		bad, _ = c.Collect(context.Background(), fixtureScope())
		if stateValue(t, bad, "proxmox.available", id) != false {
			t.Fatal(bad)
		}
	}
	cold := &Collector{PveshPath: "/pulse-fixture-missing-command"}
	batch, _ := cold.Collect(context.Background(), fixtureScope())
	if stateValue(t, batch, "proxmox.available", "custom-host-id:custom-host-id") != false {
		t.Fatal(batch)
	}
	runner := healthFixture()
	runner.responses["get /cluster/status --output-format json"] = `[]`
	batch, _ = (&Collector{Runner: runner, Part: "health"}).Collect(context.Background(), fixtureScope())
	if stateValue(t, batch, "proxmox.available", "custom-host-id:custom-host-id") != true {
		t.Fatal(batch)
	}
	// Phase collectors share the last discovered identity.
	parts := (Collector{Runner: runner}).Collectors()
	runner.responses["get /cluster/status --output-format json"] = `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"solo","local":1,"online":1}]`
	_, _ = parts[0].Collect(context.Background(), fixtureScope())
	delete(runner.responses, "get /cluster/status --output-format json")
	batch, _ = parts[3].Collect(context.Background(), fixtureScope())
	if stateValue(t, batch, "proxmox.available", "lab:solo") != false {
		t.Fatal(batch)
	}
}

func TestCephFailureKeepsFSID(t *testing.T) {
	runner := healthFixture()
	c := &Collector{Runner: runner, Part: "health", EnableCephAPI: true}
	batch, _ := c.Collect(context.Background(), fixtureScope())
	if stateValue(t, batch, "system.ceph.available", "custom-host-id:solo") != false {
		t.Fatal(batch)
	}
	runner.responses["get /cluster/ceph/status --output-format json"] = `{"fsid":"fixture-fsid"}`
	runner.responses["get /nodes/solo/ceph/pool --output-format json"] = `[]`
	batch, _ = c.Collect(context.Background(), fixtureScope())
	if stateValue(t, batch, "system.ceph.available", "fixture-fsid") != true {
		t.Fatal(batch)
	}
	delete(runner.responses, "get /cluster/ceph/status --output-format json")
	batch, _ = c.Collect(context.Background(), fixtureScope())
	if stateValue(t, batch, "system.ceph.available", "fixture-fsid") != false {
		t.Fatal(batch)
	}
	for _, state := range batch.States {
		if state.Key == "system.ceph.available" && state.Resource.Type != "ceph-cluster" {
			t.Fatal(state)
		}
	}
}

func TestTaskCapAfterDedupAndPruning(t *testing.T) {
	runner := healthFixture()
	var tasks []Task
	for i := 0; i < 12; i++ {
		tasks = append(tasks, Task{UPID: fmt.Sprintf("UPID:solo:%d", i), Type: "vzdump", Status: "ERROR", EndTime: 1700000000})
	}
	data, _ := json.Marshal(tasks)
	runner.responses["get /cluster/tasks --output-format json"] = string(data)
	c := &Collector{Runner: runner, Part: "health"}
	for _, want := range []int{10, 2, 0} {
		batch, _ := c.Collect(context.Background(), fixtureScope())
		for _, kind := range []string{"proxmox.task.failed", "proxmox.backup.failed"} {
			if countEvent(batch.Events, kind) != want {
				t.Fatalf("%s want %d: %+v", kind, want, batch.Events)
			}
		}
	}
	runner.responses["get /cluster/tasks --output-format json"] = `[]`
	_, _ = c.Collect(context.Background(), fixtureScope())
	if len(c.seenTasks) != 0 {
		t.Fatal(c.seenTasks)
	}
}

func TestHAStateShapesAndBoundedAPIText(t *testing.T) {
	runner := healthFixture()
	runner.responses["get /cluster/resources --output-format json"] = `[{"id":"qemu/100","type":"qemu"},{"id":"lxc/101","type":"lxc"}]`
	runner.responses["get /cluster/ha/status/current --output-format json"] = `[{"type":"service","sid":"vm:100","state":"stopped","status":"service vm:100 (pve1, started)"},{"type":"service","sid":"ct:101","status":"service ct:101 (pve1, started)"}]`
	batch, _ := (&Collector{Runner: runner, Part: "health"}).Collect(context.Background(), fixtureScope())
	if stateValue(t, batch, "proxmox.guest.ha.state", "custom-host-id:100") != "stopped" || stateValue(t, batch, "proxmox.guest.ha.state", "custom-host-id:101") != "started" {
		t.Fatal(batch)
	}
	var jobs []map[string]any
	for i := 0; i < 100; i++ {
		jobs = append(jobs, map[string]any{"id": fmt.Sprintf("backup-job-%03d", i), "all": 1, "schedule": strings.Repeat("界", 600)})
	}
	data, _ := json.Marshal(jobs)
	runner.responses["get /cluster/backup --output-format json"] = string(data)
	runner.responses["get /nodes/solo/storage --output-format json"] = `[]`
	batch, _ = (&Collector{Runner: runner, Part: "backups"}).Collect(context.Background(), fixtureScope())
	if utf8.RuneCountInString(stateValue(t, batch, "proxmox.backup.guest.jobs", "custom-host-id:100").(string)) != 500 {
		t.Fatal("job list not bounded")
	}
	for _, s := range batch.States {
		if v, ok := s.Value.(string); ok && utf8.RuneCountInString(v) > 500 {
			t.Fatal(s)
		}
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
}

type deadlineRunner struct{ timeout time.Duration }

func (r *deadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	deadline, _ := ctx.Deadline()
	r.timeout = time.Until(deadline)
	return []byte(`[]`), nil
}
func TestListTimeout(t *testing.T) {
	r := &deadlineRunner{}
	client, _ := (Collector{Runner: r, Timeout: 2 * time.Second, ListTimeout: 45 * time.Second}).client()
	for path, want := range map[string]time.Duration{"/cluster/tasks": 2 * time.Second, "/nodes/n1/storage/main/content?content=backup": 45 * time.Second} {
		var rows []any
		if err := client.get(context.Background(), path, &rows); err != nil {
			t.Fatal(err)
		}
		if r.timeout > want || r.timeout < want-time.Second {
			t.Fatalf("%s: %s", path, r.timeout)
		}
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

var _ monitoring.Collector = (*Collector)(nil)

func TestBackupCollectorBudgets(t *testing.T) {
	for _, timeout := range []time.Duration{0, 240 * time.Second} {
		parts := (Collector{BackupTimeout: timeout}).Collectors()
		for _, part := range parts {
			want := time.Duration(0)
			if part.Name() == "proxmox.backups" {
				want = timeout
				if want == 0 {
					want = 180 * time.Second
				}
			}
			if got := part.(monitoring.Timeouter).CollectTimeout(); got != want {
				t.Fatalf("%s: %s want %s", part.Name(), got, want)
			}
		}
	}
	r := &deadlineRunner{}
	client, err := (Collector{Runner: r}).client()
	if err != nil {
		t.Fatal(err)
	}
	var rows []any
	if err := client.get(context.Background(), "/nodes/n1/storage/pbs/content?content=backup", &rows); err != nil {
		t.Fatal(err)
	}
	if r.timeout < 119*time.Second || r.timeout > 120*time.Second {
		t.Fatalf("default list timeout: %s", r.timeout)
	}
}
