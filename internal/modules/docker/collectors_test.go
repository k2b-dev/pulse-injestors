package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

const fakeStats = `{"cpu_stats":{"cpu_usage":{"total_usage":200},"system_cpu_usage":1000,"online_cpus":2},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":500},"memory_stats":{"usage":1024,"limit":4096,"stats":{"inactive_file":128}},"pids_stats":{"current":4},"networks":{"eth0":{"rx_bytes":100,"tx_bytes":200,"rx_errors":1},"eth1":{"rx_bytes":30,"tx_bytes":40}},"blkio_stats":{"io_service_bytes_recursive":[{"op":"Read","value":2048},{"op":"Write","value":4096}]}}`

// Every API request uses this local Unix listener; no Docker daemon is needed.
type fakeDocker struct {
	socket     string
	mu         sync.Mutex
	calls      map[string]int
	containers []containerSummary
	hook       func(http.ResponseWriter, *http.Request) bool
}

func newFakeDocker(t *testing.T, count int, hook func(http.ResponseWriter, *http.Request) bool) *fakeDocker {
	t.Helper()
	f := &fakeDocker{socket: filepath.Join(t.TempDir(), "docker.sock"), calls: map[string]int{}, hook: hook}
	for i := 0; i < count; i++ {
		f.containers = append(f.containers, containerSummary{ID: fmt.Sprintf("c%d", i), Names: []string{fmt.Sprintf("/app-%d", i)}, Image: fmt.Sprintf("registry.invalid/app%d:1", i), ImageID: fmt.Sprintf("sha256:image%d", i), State: "running", Labels: map[string]string{"com.docker.compose.project": "app", "com.docker.compose.service": fmt.Sprintf("service%d", i), "com.docker.compose.container-number": "1"}})
	}
	listener, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(f.serveHTTP))
	server.Listener.Close()
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return f
}

func (f *fakeDocker) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.calls[r.URL.Path]++
	call := f.calls[r.URL.Path]
	f.mu.Unlock()
	if f.hook != nil && f.hook(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/containers/json":
		if r.URL.Query().Get("all") != "1" {
			http.Error(w, "missing all=1", 400)
			return
		}
		containers := append([]containerSummary(nil), f.containers...)
		for i := range containers {
			containers[i].Status = fmt.Sprintf("Up %d minutes", call)
		}
		json.NewEncoder(w).Encode(containers)
	case strings.HasSuffix(r.URL.Path, "/stats"):
		if r.URL.Query().Get("stream") != "false" {
			http.Error(w, "stream must be false", 400)
			return
		}
		fmt.Fprint(w, fakeStats)
	case strings.HasPrefix(r.URL.Path, "/containers/"):
		fmt.Fprint(w, `{"RestartCount":2,"State":{"Health":{"Status":"healthy"}},"Mounts":[{"Type":"volume","Source":"/must/not/stat"}],"NetworkSettings":{"Networks":{"default":{"IPAddress":"192.0.2.1"}}}}`)
	case strings.HasPrefix(r.URL.Path, "/images/"):
		fmt.Fprint(w, `{"Id":"sha256:runtime-id","Created":"2023-11-13T00:00:00Z","RepoTags":["unused:tag"],"RepoDigests":["registry.invalid/app@sha256:digest"],"Size":1024,"VirtualSize":2048,"Architecture":"arm64","Os":"linux"}`)
	case r.URL.Path == "/version":
		fmt.Fprint(w, `{"Version":"27.0","OperatingSystem":"linux","Architecture":"arm64"}`)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeDocker) count(path string) int { f.mu.Lock(); defer f.mu.Unlock(); return f.calls[path] }
func testScope() monitoring.Scope {
	return monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "fixture", Label: "Fixture"}, Timestamp: time.Unix(1700001000, 0).UTC()}
}
func (f *fakeDocker) collector() Collector {
	return Collector{SocketPath: f.socket, StableHostID: "fixture", Concurrency: 4}
}

func TestStatusUsesStableLifecycleAndCompactHealth(t *testing.T) {
	for _, health := range []string{"none", "starting", "healthy", "unhealthy"} {
		t.Run(health, func(t *testing.T) {
			f := newFakeDocker(t, 1, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != "/containers/c0/json" {
					return false
				}
				if health == "none" {
					fmt.Fprint(w, `{"RestartCount":3,"State":{}}`)
				} else {
					fmt.Fprintf(w, `{"RestartCount":3,"State":{"Health":{"Status":%q}}}`, health)
				}
				return true
			})
			// Include stopped containers in inspect so restart counts/health stay visible.
			f.containers[0].State = "exited"
			batch, err := f.collector().Collect(context.Background(), testScope())
			if err != nil {
				t.Fatal(err)
			}
			if err := validation.Batch(batch); err != nil {
				t.Fatal(err)
			}
			if len(batch.States) != 6 || len(batch.Metrics) != 3 {
				t.Fatalf("unexpected status surface: states=%d metrics=%d", len(batch.States), len(batch.Metrics))
			}
			if got := findState(batch.States, "docker.container.status"); got == nil || got.Value != "exited" {
				t.Fatalf("lifecycle = %#v", got)
			}
			if got := findState(batch.States, "docker.container.health"); got == nil || got.Value != health {
				t.Fatalf("health = %#v", got)
			}
			for _, metric := range batch.Metrics {
				if metric.Name == "docker.container.restarts" && (metric.Value != 3 || metric.Type != "counter" || metric.Unit != "") {
					t.Fatalf("restart = %#v", metric)
				}
			}
			if f.count("/containers/c0/stats") != 0 || f.count("/version") != 0 {
				t.Fatal("status phase performed slow inventory/stats")
			}
		})
	}
}

func TestStatsAggregateInterfacesWithoutExtraContainerDetails(t *testing.T) {
	f := newFakeDocker(t, 2, nil)
	f.containers[1].State = "exited"
	batch, err := (StatsCollector{Collector: f.collector()}).Collect(context.Background(), testScope())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		value     float64
		typ, unit string
	}{
		"docker.container.cpu.usage":     {40, "gauge", "percent"},
		"docker.container.memory.used":   {896, "gauge", "bytes"},
		"docker.container.memory.limit":  {4096, "gauge", "bytes"},
		"docker.container.network.rx":    {130, "counter", "bytes"},
		"docker.container.network.tx":    {240, "counter", "bytes"},
		"docker.container.blockio.read":  {2048, "counter", "bytes"},
		"docker.container.blockio.write": {4096, "counter", "bytes"},
	}
	if len(batch.Metrics) != len(want) || len(batch.States) != 0 || len(batch.Events) != 0 {
		t.Fatalf("stats surface = %#v", batch)
	}
	for _, metric := range batch.Metrics {
		expected, ok := want[metric.Name]
		if !ok || metric.Value != expected.value || metric.Type != expected.typ || metric.Unit != expected.unit {
			t.Fatalf("stats metric = %#v", metric)
		}
		if metric.Resource.Type != "docker-container" || metric.Dimensions["interface"] != "" {
			t.Fatalf("unexpected stats identity/dimensions: %#v", metric)
		}
	}
	if f.count("/containers/c0/json") != 0 || f.count("/containers/c1/stats") != 0 {
		t.Fatal("stats phase inspected metadata or collected a stopped container")
	}
}

func TestStatsCPUResetDoesNotUnderflow(t *testing.T) {
	for _, reset := range []string{"cpu", "system"} {
		var stats statsResponse
		if err := json.Unmarshal([]byte(fakeStats), &stats); err != nil {
			t.Fatal(err)
		}
		if reset == "cpu" {
			stats.PreCPUStats.CPUUsage.TotalUsage = 300
		} else {
			stats.PreCPUStats.SystemUsage = 2000
		}
		b := monitoring.NewBuilder(testScope())
		emitStats(b, nil, stats)
		for _, metric := range b.Batch().Metrics {
			if metric.Name == "docker.container.cpu.usage" {
				t.Fatalf("CPU emitted after %s counter reset: %#v", reset, metric)
			}
		}
	}
}

func TestComposeCountsAreStatesOnly(t *testing.T) {
	scope := testScope()
	batch := pulse.Batch{}
	labels := map[string]string{"com.docker.compose.project": "app", "com.docker.compose.service": "api"}
	emitContainerSummary(&batch, scope, "fixture", []containerSummary{{State: "running", Labels: labels}, {State: "exited", Labels: labels}})
	if len(batch.Metrics) != 0 || len(batch.States) != 2 {
		t.Fatalf("compose surface = %#v", batch)
	}
	for _, state := range batch.States {
		if state.Value != "1/2" {
			t.Fatalf("count = %#v", state)
		}
		if state.Resource.Type != "docker-compose-project" && state.Resource.Type != "docker-compose-service" {
			t.Fatalf("resource = %#v", state.Resource)
		}
	}
}

func TestDockerFailuresStayVisibleAndScoped(t *testing.T) {
	f := newFakeDocker(t, 2, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/containers/c0/json" || r.URL.Path == "/containers/c0/stats" || r.URL.Path == "/images/sha256:image0/json" {
			http.Error(w, "fixture failure", http.StatusInternalServerError)
			return true
		}
		return false
	})
	for _, collector := range []monitoring.Collector{f.collector(), StatsCollector{Collector: f.collector()}, InventoryCollector{Collector: f.collector()}} {
		batch, err := collector.Collect(context.Background(), testScope())
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Events) != 1 || batch.Events[0].Attributes["error"] == nil {
			t.Fatalf("missing scoped failure for %s: %#v", collector.Name(), batch.Events)
		}
		if len(batch.Metrics) == 0 {
			t.Fatalf("%s lost unaffected container/image metrics", collector.Name())
		}
	}
	missing := Collector{SocketPath: filepath.Join(t.TempDir(), "missing.sock")}
	batch, err := missing.Collect(context.Background(), testScope())
	if err != nil || len(batch.Events) != 1 || findState(batch.States, "docker.available").Value != false {
		t.Fatalf("unavailable = %#v, %v", batch, err)
	}
}

func TestStatsBoundConcurrencyAndTotalBudget(t *testing.T) {
	var active, maxActive atomic.Int32
	entered := make(chan struct{}, 10)
	f := newFakeDocker(t, 10, func(w http.ResponseWriter, r *http.Request) bool {
		if !strings.HasSuffix(r.URL.Path, "/stats") {
			return false
		}
		current := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); current > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		entered <- struct{}{}
		<-r.Context().Done()
		return true
	})
	base := f.collector()
	base.Concurrency = 2
	base.StatsTimeout = 150 * time.Millisecond
	base.ContainerTimeout = time.Second
	collector := StatsCollector{Collector: base}
	if (StatsCollector{}).CollectTimeout() != 120*time.Second || collector.CollectTimeout() != base.StatsTimeout {
		t.Fatal("stats timeout default/override lost")
	}
	done := make(chan error, 1)
	go func() { _, err := collector.Collect(context.Background(), testScope()); done <- err }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("workers did not enter")
		}
	}
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("timeout = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stats exceeded its phase budget")
	}
	if maxActive.Load() != 2 {
		t.Fatalf("concurrency = %d", maxActive.Load())
	}
	total := 0
	for i := 0; i < 10; i++ {
		total += f.count(fmt.Sprintf("/containers/c%d/stats", i))
	}
	if total != 2 {
		t.Fatalf("dispatched %d requests after timeout", total)
	}
}

func TestRunnerUsesDockerPhaseCadencesAndStateCache(t *testing.T) {
	f := newFakeDocker(t, 1, nil)
	base := f.collector()
	scope := testScope()
	now := scope.Timestamp
	runner := monitoring.Runner{Resource: scope.Resource, Collectors: []monitoring.Collector{base, StatsCollector{Collector: base}, InventoryCollector{Collector: base}}, Interval: time.Minute, Timeout: time.Second, Now: func() time.Time { return now }}
	cache := monitoring.StateCache{}
	for _, elapsed := range []time.Duration{0, time.Minute, 5 * time.Minute, 6 * time.Hour, 24 * time.Hour} {
		now = scope.Timestamp.Add(elapsed)
		batch := runner.Collect(context.Background(), false)
		states, commit := cache.Filter(batch.States, now)
		commit()
		lifecycle := findState(states, "docker.container.status")
		if elapsed == time.Minute || elapsed == 5*time.Minute {
			if lifecycle != nil {
				t.Fatal("volatile elapsed status bypassed state cache")
			}
		} else if lifecycle == nil {
			t.Fatal("initial/change/6h lifecycle refresh missing")
		}
		if len(batch.Events) != 0 {
			t.Fatalf("collection failure: %#v", batch.Events)
		}
	}
	if got := f.count("/version"); got != 2 {
		t.Fatalf("daily version requests = %d", got)
	}
	if got := f.count("/images/sha256:image0/json"); got != 2 {
		t.Fatalf("daily in-use image requests = %d", got)
	}
	if got := f.count("/containers/c0/json"); got != 5 {
		t.Fatalf("lifecycle requests = %d", got)
	}
	if got := f.count("/containers/c0/stats"); got != 4 {
		t.Fatalf("five-minute stats requests = %d", got)
	}
}

func TestSeventyContainerCadenceVolume(t *testing.T) {
	f := newFakeDocker(t, 70, nil)
	base := f.collector()
	scope := testScope()
	now := scope.Timestamp
	runner := monitoring.Runner{Resource: scope.Resource, Collectors: []monitoring.Collector{base, StatsCollector{Collector: base}, InventoryCollector{Collector: base}}, Interval: time.Minute, Timeout: 10 * time.Second, Now: func() time.Time { return now }}
	cache := monitoring.StateCache{}
	values := 0
	const minutes = 60
	for minute := 0; minute < minutes; minute++ {
		now = scope.Timestamp.Add(time.Duration(minute) * time.Minute)
		batch := runner.Collect(context.Background(), false)
		if err := validation.Batch(batch); err != nil {
			t.Fatal(err)
		}
		if len(batch.Events) != 0 {
			t.Fatalf("fixture collection failure: %#v", batch.Events)
		}
		states, commit := cache.Filter(batch.States, now)
		commit()
		values += len(batch.Metrics) + len(states) + len(batch.Events)
		for _, state := range batch.States {
			if state.Resource.Type == "docker-image" && state.Resource.Label == "unused:tag" {
				t.Fatal("unused image reference emitted")
			}
		}
	}
	average := float64(values) / minutes
	t.Logf("70 containers, 70 services: %d values / %d min = %.2f values/min (includes cold start)", values, minutes, average)
	if average >= 400 {
		t.Fatalf("Docker volume %.2f values/min exceeds 400", average)
	}
	if f.count("/containers/c0/stats") != 12 || f.count("/version") != 1 {
		t.Fatal("volume regression did not exercise scheduled cadences")
	}
}

func TestInventoryDeduplicatesReplacedImageReferences(t *testing.T) {
	f := newFakeDocker(t, 2, nil)
	f.containers[0].State = "exited"
	f.containers[0].Image = f.containers[1].Image
	batch, err := (InventoryCollector{Collector: f.collector()}).Collect(context.Background(), testScope())
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if f.count("/images/sha256:image0/json") != 0 || f.count("/images/sha256:image1/json") != 1 {
		t.Fatal("logical image did not prefer running replacement")
	}
	for _, state := range batch.States {
		if state.Resource.Type == "docker-daemon" && state.Key != "docker.version" {
			t.Fatalf("extra daemon inventory state: %#v", state)
		}
		if state.Resource.Type == "docker-image" && state.Resource.Label != f.containers[1].Image {
			t.Fatalf("unexpected image reference: %#v", state)
		}
	}
}

func TestMemoryEntirelyReclaimableCache(t *testing.T) {
	if got := dockerMemoryUsage(1024, map[string]uint64{"inactive_file": 1024}); got != 0 {
		t.Fatalf("used memory = %d, want 0", got)
	}
}

func TestInventoryTopLevelFailureRetriesNextTick(t *testing.T) {
	for _, path := range []string{"/version", "/containers/json"} {
		t.Run(path, func(t *testing.T) {
			var attempts atomic.Int32
			f := newFakeDocker(t, 1, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path == path && attempts.Add(1) == 1 {
					http.Error(w, "transient failure", 500)
					return true
				}
				return false
			})
			now := testScope().Timestamp
			runner := monitoring.Runner{Resource: testScope().Resource, Interval: time.Minute, Timeout: time.Second, Now: func() time.Time { return now }, Collectors: []monitoring.Collector{InventoryCollector{Collector: f.collector()}}}
			batch := runner.Collect(context.Background(), false)
			if findState(batch.States, "ingestor.collector.ok").Value != false {
				t.Fatal("inventory failure reported success")
			}
			now = now.Add(time.Minute)
			batch = runner.Collect(context.Background(), false)
			if findState(batch.States, "ingestor.collector.ok").Value != true {
				t.Fatal("inventory did not recover")
			}
			now = now.Add(time.Minute)
			runner.Collect(context.Background(), false)
			if attempts.Load() != 2 {
				t.Fatalf("attempts=%d", attempts.Load())
			}
		})
	}
}
