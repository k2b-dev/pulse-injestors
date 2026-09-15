package script

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestScriptCollectorParsesJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "script.sh")
	body := "#!/bin/sh\nprintf '%s\\n' '{\"dimensions\":{\"role\":\"test\"},\"metrics\":[{\"name\":\"custom.x\",\"type\":\"gauge\",\"value\":1}]}'\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	batch, err := Collector{Scripts: []Script{{
		Name:           "custom",
		Command:        []string{"/bin/sh", path},
		Timeout:        30 * time.Second,
		MaxOutputBytes: 1024,
	}}}.Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "node"}, Timestamp: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) != 1 {
		t.Fatalf("metrics = %d; events = %#v", len(batch.Metrics), batch.Events)
	}
	if batch.Metrics[0].Dimensions["script"] != "custom" || batch.Metrics[0].Dimensions["role"] != "test" {
		t.Fatalf("dimensions = %#v", batch.Metrics[0].Dimensions)
	}
}

func TestScriptRejectsLegacyAndUnknownFields(t *testing.T) {
	for _, body := range []string{
		`{"metrics":[{"name":"custom.x","value":1,"entityId":"host:x"}]}`,
		`{"metrics":[{"name":"custom.x","value":1,"resource":{"type":"host","id":"x","extra":true}}]}`,
		`{"unknown":true}`,
		`{"metrics":[]} {"states":[]}`,
	} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.json")
			if err := os.WriteFile(path, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			_, err := runScript(context.Background(), monitoring.Scope{}, Script{Name: "test", Command: []string{"cat", path}, Timeout: 30 * time.Second})
			if err == nil {
				t.Fatal("invalid script output accepted")
			}
			if !strings.Contains(err.Error(), "invalid json") && !strings.Contains(err.Error(), "trailing JSON") {
				t.Fatalf("unexpected failure: %v", err)
			}
		})
	}
}
