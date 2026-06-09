package proxmox

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
	got, err := normalizeBaseURL("https://pve.example:8006")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://pve.example:8006/api2/json" {
		t.Fatalf("url = %q", got)
	}
	got, err = normalizeBaseURL("https://pve.example:8006/api2/json/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://pve.example:8006/api2/json" {
		t.Fatalf("url = %q", got)
	}
}

func TestAuthHeader(t *testing.T) {
	if got := authHeader("root@pam!pulse=secret"); got != "PVEAPIToken=root@pam!pulse=secret" {
		t.Fatalf("auth = %q", got)
	}
	if got := authHeader("PVEAPIToken=root@pam!pulse=secret"); got != "PVEAPIToken=root@pam!pulse=secret" {
		t.Fatalf("auth = %q", got)
	}
}

func TestCollectorEmitsVersionClusterAndResources(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api2/json/version":
			_, _ = w.Write([]byte(`{"data":{"version":"9.0","release":"9.0-1","repoid":"abc"}}`))
		case "/api2/json/cluster/status":
			_, _ = w.Write([]byte(`{"data":[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"pve1","online":1,"ip":"10.0.0.1"},{"type":"node","name":"pve2","online":0}]}`))
		case "/api2/json/cluster/resources":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"node/pve1","type":"node","node":"pve1","status":"online","cpu":0.25,"maxcpu":8,"mem":1024,"maxmem":4096,"disk":2048,"maxdisk":8192,"uptime":99},
				{"id":"qemu/100","type":"qemu","node":"pve1","name":"vm-100","status":"running","cpu":0.5,"maxcpu":2,"mem":512,"maxmem":1024,"disk":100,"maxdisk":200,"uptime":10},
				{"id":"lxc/101","type":"lxc","node":"pve2","name":"ct-101","status":"stopped"}
			]}`))
		case "/api2/json/cluster/tasks":
			_, _ = w.Write([]byte(`{"data":[
				{"upid":"UPID:pve1:1","node":"pve1","type":"vzdump","id":"100","user":"root@pam","status":"OK","starttime":1700000000,"endtime":1700000300},
				{"upid":"UPID:pve1:2","node":"pve1","type":"vzdump","id":"101","user":"root@pam","status":"ERROR: backup failed","starttime":1700000400,"endtime":1700000500},
				{"upid":"UPID:pve1:3","node":"pve1","type":"qmstart","id":"100","user":"root@pam","status":"OK","starttime":1700000600,"endtime":1700000610}
			]}`))
		case "/api2/json/cluster/backup":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"backup-daily","enabled":1,"schedule":"daily","storage":"pbs","mode":"snapshot"},
				{"id":"backup-disabled","enabled":0,"schedule":"weekly","storage":"local"}
			]}`))
		case "/api2/json/cluster/backup-info/not-backed-up":
			_, _ = w.Write([]byte(`{"data":[{"vmid":102,"name":"ct-102","type":"lxc","node":"pve1"}]}`))
		case "/api2/json/cluster/ceph/status":
			_, _ = w.Write([]byte(`{"data":{
				"health":{"status":"HEALTH_OK"},
				"osdmap":{"osdmap":{"num_osds":3,"num_up_osds":3,"num_in_osds":2}},
				"pgmap":{"num_pgs":64,"bytes_used":1000,"bytes_total":4000,"bytes_avail":3000,"pgs_by_state":[{"state_name":"active+clean","count":64}]}
			}}`))
		case "/api2/json/nodes/pve1/ceph/pools":
			_, _ = w.Write([]byte(`{"data":[{"pool_name":"rbd","bytes_used":10,"max_avail":20,"objects":3}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	batch, err := Collector{
		BaseURL:       srv.URL,
		APIToken:      "root@pam!pulse=secret",
		HTTPClient:    srv.Client(),
		Timeout:       time.Second,
		EnableCephAPI: true,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pve",
		EntityType: "proxmox",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "PVEAPIToken=root@pam!pulse=secret" {
		t.Fatalf("auth = %q", gotAuth)
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
	if countMetric(batch.Metrics, "proxmox.resources.by_type") != 3 {
		t.Fatalf("missing type counts: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "proxmox.ceph.osds.total") != 1 {
		t.Fatalf("missing ceph osd metric: %#v", batch.Metrics)
	}
	if countMetric(batch.Metrics, "proxmox.ceph.pool.bytes.used") != 1 {
		t.Fatalf("missing ceph pool metric: %#v", batch.Metrics)
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
	if countState(batch.States, "proxmox.backup.job.enabled") != 2 {
		t.Fatalf("missing backup job states: %#v", batch.States)
	}
}

func TestCollectorGracefullyReportsMissingCephAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/version":
			_, _ = w.Write([]byte(`{"data":{"version":"9.0"}}`))
		case "/api2/json/cluster/status":
			_, _ = w.Write([]byte(`{"data":[{"type":"cluster","name":"lab","quorate":1},{"type":"node","name":"pve1","online":1}]}`))
		case "/api2/json/cluster/resources":
			_, _ = w.Write([]byte(`{"data":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	batch, err := Collector{
		BaseURL:       srv.URL,
		APIToken:      "root@pam!pulse=secret",
		HTTPClient:    srv.Client(),
		Timeout:       time.Second,
		EnableCephAPI: true,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pve",
		EntityType: "proxmox",
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
	if !hasStateValue(batch.States, "proxmox.ceph.available", false) {
		t.Fatalf("missing ceph unavailable state: %#v", batch.States)
	}
	if countEvent(batch.Events, "proxmox.ceph.status.failed") != 1 {
		t.Fatalf("missing ceph failure event: %#v", batch.Events)
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

func hasStateValue(states []pulse.State, key string, value any) bool {
	for _, state := range states {
		if state.Key == key && state.Value == value {
			return true
		}
	}
	return false
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
