package proxmox

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
		"get /version --output-format json":        `{"version":"9.0","release":"9.0-1","repoid":"abc"}`,
		"get /cluster/status --output-format json": `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"pve1","online":1,"ip":"10.0.0.1"},{"type":"node","name":"pve2","online":0}]`,
		"get /cluster/resources --output-format json": `[
			{"id":"node/pve1","type":"node","node":"pve1","status":"online","cpu":0.25,"maxcpu":8,"mem":1024,"maxmem":4096,"disk":2048,"maxdisk":8192,"uptime":99},
			{"id":"qemu/100","type":"qemu","node":"pve1","name":"vm-100","status":"running","cpu":0.5,"maxcpu":2,"mem":512,"maxmem":1024,"disk":100,"maxdisk":200,"uptime":10},
			{"id":"lxc/101","type":"lxc","node":"pve2","name":"ct-101","status":"stopped"}
		]`,
		"get /cluster/tasks --output-format json": `[
			{"upid":"UPID:pve1:1","node":"pve1","type":"vzdump","id":"100","user":"root@pam","status":"OK","starttime":1700000000,"endtime":1700000300},
			{"upid":"UPID:pve1:2","node":"pve1","type":"vzdump","id":"101","user":"root@pam","status":"ERROR: backup failed","starttime":1700000400,"endtime":1700000500},
			{"upid":"UPID:pve1:3","node":"pve1","type":"qmstart","id":"100","user":"root@pam","status":"OK","starttime":1700000600,"endtime":1700000610}
		]`,
		"get /cluster/backup --output-format json": `[
			{"id":"backup-daily","enabled":1,"schedule":"daily","storage":"pbs","mode":"snapshot"},
			{"id":"backup-disabled","enabled":0,"schedule":"weekly","storage":"local"}
		]`,
		"get /cluster/backup-info/not-backed-up --output-format json": `[{"vmid":102,"name":"ct-102","type":"lxc","node":"pve1"}]`,
		"get /cluster/ceph/status --output-format json": `{
				"fsid":"ceph-fsid-1",
				"health":{"status":"HEALTH_OK"},
			"osdmap":{"osdmap":{"num_osds":3,"num_up_osds":3,"num_in_osds":2}},
			"pgmap":{"num_pgs":64,"bytes_used":1000,"bytes_total":4000,"bytes_avail":3000,"pgs_by_state":[{"state_name":"active+clean","count":64}]}
		}`,
		"get /nodes/pve1/ceph/pools --output-format json": `[{"pool_name":"rbd","bytes_used":10,"max_avail":20,"objects":3}]`,
	}}

	batch, err := Collector{
		PveshPath:     "pvesh",
		Runner:        runner,
		Timeout:       time.Second,
		EnableCephAPI: true,
	}.Collect(context.Background(), monitoring.Scope{
		Resource:  pulse.ResourceRef{Type: "proxmox-node", ID: "pve"},
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
		if err := os.WriteFile(filepath.Join(directory, "proxmox.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
