package proxmox

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

func TestPveshArgs(t *testing.T) {
	args, err := pveshArgs("/cluster/tasks?limit=50")
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, " ")
	want := "get /cluster/tasks --limit 50 --output-format json"
	if got != want {
		t.Fatalf("args = %q", got)
	}
}

func TestCollectorEmitsVersionClusterAndResources(t *testing.T) {
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
		EntityID:   "pve",
		EntityType: "proxmox-node",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if countState(batch.States, "proxmox.available") != 1 || countState(batch.States, "proxmox.cluster.quorate") != 1 {
		t.Fatalf("states = %#v", batch.States)
	}
	if countMetric(batch.Metrics, "proxmox.nodes.total") != 1 {
		t.Fatalf("missing node metric: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "proxmox.resource.memory.usage") != 2 {
		t.Fatalf("missing memory usage metrics: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "proxmox.resource.memory.usage", "proxmox-vm", "proxmox-vm:lab:100") != 1 {
		t.Fatalf("missing vm-scoped memory usage metric: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "proxmox.resource.memory.usage", "proxmox-node", "proxmox-node:lab:pve1") != 1 {
		t.Fatalf("missing node-scoped resource memory usage metric: %#v", batch.Metrics)
	}
	if hasMetricDimension(batch.Metrics, "proxmox.resource.memory.usage", "proxmox-node", "proxmox-node:lab:pve1", "vmid") {
		t.Fatalf("node resource has vmid dimension: %#v", batch.Metrics)
	}
	if countStateEntity(batch.States, "proxmox.resource.present", "proxmox-container", "proxmox-container:lab:101") != 1 {
		t.Fatalf("missing container-scoped present state: %#v", batch.States)
	}
	if countStateEntity(batch.States, "proxmox.node.online", "proxmox-node", "proxmox-node:lab:pve1") != 1 {
		t.Fatalf("missing node-scoped online state: %#v", batch.States)
	}
	if countMetric(batch.Metrics, "proxmox.resources.by_type") != 3 {
		t.Fatalf("missing type counts: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "system.ceph.osds.total") != 1 {
		t.Fatalf("missing ceph osd metric: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "system.ceph.osds.total", "ceph-cluster", "ceph-cluster:ceph-fsid-1") != 1 {
		t.Fatalf("missing ceph-cluster scoped osd metric: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "system.ceph.pool.bytes.used") != 1 {
		t.Fatalf("missing ceph pool metric: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "system.ceph.pool.bytes.used", "ceph-pool", "ceph-pool:ceph-fsid-1:rbd") != 1 {
		t.Fatalf("missing ceph-pool scoped metric: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "proxmox.tasks.by_type_status") != 3 {
		t.Fatalf("missing task status metrics: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "proxmox.backup.last_success.age") != 1 {
		t.Fatalf("missing backup freshness metric: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "proxmox.backup.guests.not_backed_up") != 1 {
		t.Fatalf("missing backup coverage metric: %#v", batch.Metrics)
	}
	if countEvent(batch.Events, "proxmox.backup.failed") != 1 {
		t.Fatalf("missing backup failed event: %#v", batch.Events)
	}
	for _, event := range batch.Events {
		if event.Kind == "proxmox.backup.failed" {
			if event.CorrelationID != "UPID:pve1:2" || event.ActorID != "root@pam" {
				t.Fatalf("task identity = %#v", event)
			}
			if _, ok := event.Dimensions["id"]; ok {
				t.Fatalf("task id must not be a dimension: %#v", event.Dimensions)
			}
		}
	}
	if countState(batch.States, "proxmox.backup.job.enabled") != 2 {
		t.Fatalf("missing backup job states: %#v", batch.States)
	}
	if countStateEntity(batch.States, "proxmox.backup.job.enabled", "proxmox-backup-job", "proxmox-backup-job:lab:backup-daily") != 1 {
		t.Fatalf("missing backup-job scoped enabled state: %#v", batch.States)
	}
	if countStateEntity(batch.States, "proxmox.backup.guest.covered", "proxmox-container", "proxmox-container:lab:102") != 1 {
		t.Fatalf("missing guest-scoped backup coverage state: %#v", batch.States)
	}
}

func TestCollectorFallsBackForStandaloneNode(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"get /version --output-format json":                           `{"version":"9.0"}`,
		"get /nodes --output-format json":                             `[{"node":"pve1","status":"online","cpu":0.2,"maxcpu":4,"mem":100,"maxmem":200,"disk":300,"maxdisk":600,"uptime":60}]`,
		"get /cluster/resources --output-format json":                 `[]`,
		"get /cluster/tasks --output-format json":                     `[]`,
		"get /cluster/backup --output-format json":                    `[]`,
		"get /cluster/backup-info/not-backed-up --output-format json": `[]`,
	}}

	batch, err := Collector{
		PveshPath: "pvesh",
		Runner:    runner,
		Timeout:   time.Second,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pve1",
		EntityType: "proxmox-node",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if countEvent(batch.Events, "proxmox.cluster.status.failed") != 1 {
		t.Fatalf("missing cluster fallback event: %#v", batch.Events)
	}
	if countState(batch.States, "proxmox.node.online") != 1 {
		t.Fatalf("missing node state: %#v", batch.States)
	}
	if countMetric(batch.Metrics, "proxmox.node.memory.usage") != 1 {
		t.Fatalf("missing node memory metric: %#v", batch.Metrics)
	}
	for _, state := range batch.States {
		if state.EntityType == "proxmox-cluster" {
			t.Fatalf("standalone node created a cluster resource: %#v", state.Resource)
		}
	}
}

func TestCollectorGracefullyReportsMissingCommand(t *testing.T) {
	batch, err := Collector{
		PveshPath: "pulse-test-missing-pvesh",
		Timeout:   time.Second,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pve",
		EntityType: "proxmox-node",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !hasStateValue(batch.States, "proxmox.available", false) {
		t.Fatalf("missing unavailable state: %#v", batch.States)
	}
	if countEvent(batch.Events, "proxmox.config.failed") != 0 {
		t.Fatalf("unexpected config failure event: %#v", batch.Events)
	}
}

func TestCollectorGracefullyReportsMissingCephAPI(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"get /version --output-format json":           `{"version":"9.0"}`,
		"get /cluster/status --output-format json":    `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"pve1","online":1}]`,
		"get /cluster/resources --output-format json": `[]`,
	}}

	batch, err := Collector{
		PveshPath:     "pvesh",
		Runner:        runner,
		Timeout:       time.Second,
		EnableCephAPI: true,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pve",
		EntityType: "proxmox-node",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if !hasStateValue(batch.States, "proxmox.ceph.api.enabled", true) {
		t.Fatalf("missing ceph api enabled state: %#v", batch.States)
	}
	if !hasStateValue(batch.States, "system.ceph.available", false) {
		t.Fatalf("missing ceph unavailable state: %#v", batch.States)
	}
	if countEvent(batch.Events, "proxmox.ceph.status.failed") != 1 {
		t.Fatalf("missing ceph failure event: %#v", batch.Events)
	}
}

func TestCollectorQuietlyReportsUnavailableCephAPI(t *testing.T) {
	runner := &fakeRunner{
		responses: map[string]string{
			"get /version --output-format json":                           `{"version":"9.0"}`,
			"get /cluster/status --output-format json":                    `[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"pve1","online":1}]`,
			"get /cluster/resources --output-format json":                 `[]`,
			"get /cluster/tasks --output-format json":                     `[]`,
			"get /cluster/backup --output-format json":                    `[]`,
			"get /cluster/backup-info/not-backed-up --output-format json": `[]`,
		},
		errors: map[string]error{
			"get /cluster/ceph/status --output-format json": fmt.Errorf("exit status 1: ceph is not configured"),
		},
	}

	batch, err := Collector{
		PveshPath:     "pvesh",
		Runner:        runner,
		Timeout:       time.Second,
		EnableCephAPI: true,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pve",
		EntityType: "proxmox-node",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if !hasStateValue(batch.States, "system.ceph.available", false) {
		t.Fatalf("missing ceph unavailable state: %#v", batch.States)
	}
	if countEvent(batch.Events, "proxmox.ceph.status.failed") != 0 {
		t.Fatalf("unexpected ceph failure event: %#v", batch.Events)
	}
}

func TestParseCephPools(t *testing.T) {
	pools := parseCephPools([]map[string]any{
		{"pool_name": "rbd", "bytes_used": float64(10), "max_avail": float64(20), "objects": float64(3)},
		{"name": "cephfs", "stored": float64(11), "available": float64(22), "objects": float64(4)},
		{"bytes_used": float64(1)},
	})
	if len(pools) != 2 {
		t.Fatalf("pools = %d", len(pools))
	}
	if pools[0].Name != "rbd" || pools[0].BytesUsed != 10 || pools[0].BytesAvailable != 20 || pools[0].Objects != 3 {
		t.Fatalf("first = %#v", pools[0])
	}
	if pools[1].Name != "cephfs" || pools[1].BytesUsed != 11 || pools[1].BytesAvailable != 22 || pools[1].Objects != 4 {
		t.Fatalf("second = %#v", pools[1])
	}
}

func TestParseBackupJobs(t *testing.T) {
	jobs := parseBackupJobs([]map[string]any{
		{"id": "daily", "enabled": float64(1), "schedule": "daily", "storage": "pbs", "mode": "snapshot"},
		{"id": "weekly", "enabled": "false"},
		{"enabled": float64(1)},
	})
	if len(jobs) != 2 {
		t.Fatalf("jobs = %d", len(jobs))
	}
	if jobs[0].ID != "daily" || !jobs[0].Enabled || jobs[0].Schedule != "daily" || jobs[0].Storage != "pbs" || jobs[0].Mode != "snapshot" {
		t.Fatalf("first = %#v", jobs[0])
	}
	if jobs[1].ID != "weekly" || jobs[1].Enabled {
		t.Fatalf("second = %#v", jobs[1])
	}
}

type fakeRunner struct {
	responses map[string]string
	errors    map[string]error
}

func (f *fakeRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	if err, ok := f.errors[key]; ok {
		return nil, err
	}
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

func hasMetricDimension(metrics []pulse.Metric, name, entityType, entityID, dimension string) bool {
	for _, metric := range metrics {
		if metric.Name != name || metric.EntityType != entityType || metric.EntityID != entityID {
			continue
		}
		if _, ok := metric.Dimensions[dimension]; ok {
			return true
		}
	}
	return false
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

func hasStateValue(states []pulse.State, key string, value any) bool {
	for _, state := range states {
		if state.Key == key && state.Value == value {
			return true
		}
	}
	return false
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
