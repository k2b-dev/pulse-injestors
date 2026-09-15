package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

// Exercise Docker's signal renderers with API-shaped fixtures, without a daemon.
func TestPulseContractExport(t *testing.T) {
	scope := monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "fixture", Label: "Fixture"}, Dimensions: map[string]string{"host": "fixture"}, Timestamp: time.Unix(1700001000, 0).UTC()}
	container := containerSummary{ID: "abcdef0123456789", Names: []string{"/app-1"}, State: "running", Labels: map[string]string{"com.docker.compose.project": "example", "com.docker.compose.service": "app", "com.docker.compose.container-number": "1"}}
	b := monitoring.NewBuilder(containerScope(scope, "fixture", container))
	var stats statsResponse
	if err := json.Unmarshal([]byte(`{"cpu_stats":{"cpu_usage":{"total_usage":200},"system_cpu_usage":1000,"online_cpus":2},"precpu_stats":{"cpu_usage":{"total_usage":100},"system_cpu_usage":500},"memory_stats":{"usage":1024,"limit":4096,"stats":{"inactive_file":128}},"pids_stats":{"current":4},"networks":{"eth0":{"rx_bytes":10000,"tx_bytes":20000,"rx_errors":1,"tx_errors":0}},"blkio_stats":{"io_service_bytes_recursive":[{"op":"Read","value":2048},{"op":"Write","value":4096}]}}`), &stats); err != nil {
		t.Fatal(err)
	}
	emitStats(b, containerDims(container), stats)
	var inspect inspectResponse
	if err := json.Unmarshal([]byte(`{"Id":"abcdef0123456789","RestartCount":2,"State":{"Running":true,"Status":"running","StartedAt":"2023-11-14T22:13:20Z","Health":{"Status":"healthy","FailingStreak":0}},"Config":{"Image":"example/app:1","Hostname":"app","Labels":{"com.docker.compose.project":"example","com.docker.compose.service":"app"}},"HostConfig":{"NetworkMode":"bridge","RestartPolicy":{"Name":"unless-stopped"}},"NetworkSettings":{"Networks":{"default":{"IPAddress":"192.0.2.1","Gateway":"192.0.2.254"}}}}`), &inspect); err != nil {
		t.Fatal(err)
	}
	emitInspect(b, scope, "fixture", container, containerDims(container), t.TempDir(), inspect)
	batch := b.Batch()
	emitContainerSummary(&batch, scope, "fixture", []containerSummary{container})
	emitComposeUsageSummary(&batch, scope, "fixture")
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
		if err := os.WriteFile(filepath.Join(directory, "docker.json"), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
