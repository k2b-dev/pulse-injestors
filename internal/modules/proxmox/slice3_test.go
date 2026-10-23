package proxmox

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

func TestElection(t *testing.T) {
	for _, test := range []struct{ name, json, local, want string }{
		{"lowest ID beats name", `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"a","nodeid":2,"online":1},{"type":"node","name":"z","nodeid":1,"online":1}]`, "a", "z"},
		{"local online", `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"a","nodeid":1,"online":1},{"type":"node","name":"b","nodeid":2,"online":1}]`, "a", "a"},
		{"local offline", `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"a","nodeid":1,"online":0},{"type":"node","name":"b","nodeid":2,"online":1}]`, "a", "b"},
		{"missing ID", `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"a","online":1},{"type":"node","name":"b","nodeid":1,"online":1}]`, "b", "a"},
		{"all offline", `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"a","nodeid":1,"online":0}]`, "a", ""},
		{"standalone", `[{"type":"node","name":"solo","online":1,"local":1}]`, "solo", "solo"},
		{"standalone empty", `[]`, "solo", "solo"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var rows []ClusterStatus
			if err := json.Unmarshal([]byte(test.json), &rows); err != nil {
				t.Fatal(err)
			}
			if got := electedNode(rows, test.local); got != test.want {
				t.Fatalf("got %q want %q", got, test.want)
			}
		})
	}
}

func TestBackupSelection(t *testing.T) {
	guest := Resource{ID: "qemu/101", Type: "qemu", Node: "n2", Pool: "production"}
	for _, test := range []struct {
		name string
		job  BackupJob
		want bool
	}{
		{"all", BackupJob{Enabled: true, All: true}, true},
		{"disabled", BackupJob{All: true}, false},
		{"vmids", BackupJob{Enabled: true, VMIDs: "100,101;102"}, true},
		{"pool", BackupJob{Enabled: true, Pool: "production"}, true},
		{"excluded all", BackupJob{Enabled: true, All: true, Exclude: "101"}, false},
		{"excluded pool", BackupJob{Enabled: true, Pool: "production", Exclude: "101"}, false},
		{"different node", BackupJob{Enabled: true, All: true, Node: "n1"}, false},
		{"different pool", BackupJob{Enabled: true, Pool: "other"}, false},
		{"exact ID", BackupJob{Enabled: true, VMIDs: "1010"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.job.selects(guest); got != test.want {
				t.Fatalf("got %v", got)
			}
		})
	}
}

func fixtureScope() monitoring.Scope {
	return monitoring.Scope{Resource: pulse.ResourceRef{Type: "proxmox-node", ID: "custom-host-id"}, Timestamp: time.Unix(1700001000, 0).UTC()}
}

func TestFollowerAndTakeover(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"get /cluster/status --output-format json":   `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"n1","nodeid":1,"online":1},{"type":"node","name":"n2","nodeid":2,"online":1,"local":1}]`,
		"get /nodes/n2/storage --output-format json": `[{"storage":"shared","shared":1,"enabled":1,"active":1},{"storage":"local","shared":0,"enabled":1,"active":1,"total":100,"used":0,"avail":100}]`,
	}}
	c := &Collector{Runner: runner, Part: "resources"}
	batch, _ := c.Collect(context.Background(), fixtureScope())
	if len(batch.Events) > 0 || len(batch.Metrics) != 3 {
		t.Fatalf("follower made cluster calls or missed local storage: %#v", batch)
	}
	for _, m := range batch.Metrics {
		if m.Resource.ID != "lab:n2:local" {
			t.Fatalf("unexpected resource: %#v", m.Resource)
		}
	}
	runner.responses["get /cluster/status --output-format json"] = strings.Replace(runner.responses["get /cluster/status --output-format json"], `"nodeid":1,"online":1`, `"nodeid":1,"online":0`, 1)
	runner.responses["get /cluster/resources --output-format json"] = `[{"id":"qemu/101","type":"qemu","name":"app","node":"n2","status":"running","cpu":0,"maxmem":1024,"diskread":100,"diskwrite":200,"netin":300,"netout":400},{"id":"qemu/999","type":"qemu","template":1}]`
	batch, _ = c.Collect(context.Background(), fixtureScope())
	if len(batch.Events) > 0 || countMetric(batch.Metrics, "proxmox.guest.cpu.usage") != 1 || len(batch.Metrics) != 14 {
		t.Fatalf("takeover failed: %#v", batch)
	}
	for _, m := range batch.Metrics {
		if strings.HasPrefix(m.Name, "proxmox.guest.") && (m.Resource.ID != "lab:101" || m.Dimensions["node"] != "") {
			t.Fatalf("unstable guest identity: %#v", m)
		}
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
}

func TestBackupsPerStorageAndMissingSnapshot(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"get /cluster/status --output-format json":                                    `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"n1","nodeid":1,"online":1,"local":1}]`,
		"get /cluster/resources --output-format json":                                 `[{"id":"qemu/101","type":"qemu","name":"app","node":"n1","pool":"prod"},{"id":"lxc/102","type":"lxc","name":"excluded","node":"n1"}]`,
		"get /cluster/backup --output-format json":                                    `[{"id":"hourly","enabled":1,"all":1,"exclude":"102"},{"id":"pool","enabled":1,"pool":"prod"},{"id":"disabled","enabled":0,"all":1}]`,
		"get /nodes/n1/storage --output-format json":                                  `[{"storage":"primary","type":"pbs","active":1,"enabled":1},{"storage":"copy","type":"pbs","active":1,"enabled":1},{"storage":"off","type":"pbs","active":0,"enabled":1}]`,
		"get /nodes/n1/storage/primary/content --content backup --output-format json": `[{"vmid":101,"ctime":1700000000},{"vmid":101,"ctime":1700000900}]`,
		"get /nodes/n1/storage/copy/content --content backup --output-format json":    `[{"vmid":101,"ctime":1700000500}]`,
	}}
	batch, _ := (&Collector{Runner: runner, Part: "backups"}).Collect(context.Background(), fixtureScope())
	if len(batch.Events) > 0 {
		t.Fatal(batch.Events)
	}
	if countMetric(batch.Metrics, "proxmox.backup.last_success.age") != 2 {
		t.Fatal(batch.Metrics)
	}
	for _, m := range batch.Metrics {
		want := float64(100)
		if m.Dimensions["storage"] == "copy" {
			want = 500
		}
		if m.Value != want || m.Resource.ID != "lab:101" {
			t.Fatal(m)
		}
	}
	for _, s := range batch.States {
		if s.Key == "proxmox.backup.guest.jobs" && s.Resource.ID == "lab:101" && s.Value != "hourly,pool" {
			t.Fatal(s)
		}
		if s.Key == "proxmox.backup.guest.covered" && s.Resource.ID == "lab:102" && s.Value != false {
			t.Fatal(s)
		}
		if s.Key == "proxmox.backup.last_success.time" && s.Resource.ID == "lab:102" && s.Value != "" {
			t.Fatal(s)
		}
	}
}

func TestTaskDedupAndClusterFailureClosed(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"get /cluster/status --output-format json":            `[{"type":"node","name":"solo","online":1,"local":1}]`,
		"get /cluster/resources --output-format json":         `[]`,
		"get /cluster/ha/status/current --output-format json": `[]`,
		"get /cluster/tasks --output-format json":             `[{"upid":"UPID:solo:1","type":"vzdump","status":"ERROR","endtime":1700000000}]`,
	}}
	c := &Collector{Runner: runner, Part: "health"}
	for i := 0; i < 2; i++ {
		batch, _ := c.Collect(context.Background(), fixtureScope())
		want := 1
		if i == 1 {
			want = 0
		}
		if countEvent(batch.Events, "proxmox.backup.failed") != want || countEvent(batch.Events, "proxmox.task.failed") != want {
			t.Fatal(batch.Events)
		}
	}
	delete(runner.responses, "get /cluster/status --output-format json")
	batch, _ := c.Collect(context.Background(), fixtureScope())
	if len(batch.Metrics) != 0 || countEvent(batch.Events, "proxmox.cluster.status.failed") != 1 {
		t.Fatal(batch)
	}
}

func TestModernCephStatus(t *testing.T) {
	var status CephStatus
	if err := json.Unmarshal([]byte(`{"osdmap":{"num_osds":12,"num_up_osds":11,"num_in_osds":10},"pgmap":{"num_pgs":20,"pgs_by_state":[{"state_name":"active+clean","count":18},{"state_name":"active+degraded","count":2}]}}`), &status); err != nil {
		t.Fatal(err)
	}
	s := status.summary()
	if s.OSDsTotal != 12 || s.OSDsUp != 11 || s.OSDsIn != 10 || s.PGsTotal-s.PGsByState["active+clean"] != 2 {
		t.Fatal(s)
	}
}
