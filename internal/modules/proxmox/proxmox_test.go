package proxmox

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
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
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	batch, err := Collector{
		BaseURL:    srv.URL,
		APIToken:   "root@pam!pulse=secret",
		HTTPClient: srv.Client(),
		Timeout:    time.Second,
	}.Collect(context.Background(), monitoring.Scope{
		EntityID:   "pve",
		EntityType: "proxmox",
		Timestamp:  time.Unix(0, 0).UTC(),
	})
	if err != nil {
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

func countState(states []pulse.State, key string) int {
	count := 0
	for _, state := range states {
		if state.Key == key {
			count++
		}
	}
	return count
}
