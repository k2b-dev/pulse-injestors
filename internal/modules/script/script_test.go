package script

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
)

func TestScriptCollectorParsesJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.sh")
	body := "#!/bin/sh\nprintf '%s\\n' '{\"dimensions\":{\"role\":\"test\"},\"metrics\":[{\"name\":\"custom.x\",\"type\":\"gauge\",\"value\":1}]}'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	batch, err := Collector{Scripts: []Script{{
		Name:           "custom",
		Command:        []string{path},
		Timeout:        5 * time.Second,
		MaxOutputBytes: 1024,
	}}}.Collect(context.Background(), monitoring.Scope{EntityID: "node", EntityType: "host", Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) != 1 {
		t.Fatalf("metrics = %d", len(batch.Metrics))
	}
	if batch.Metrics[0].Dimensions["script"] != "custom" || batch.Metrics[0].Dimensions["role"] != "test" {
		t.Fatalf("dimensions = %#v", batch.Metrics[0].Dimensions)
	}
}
