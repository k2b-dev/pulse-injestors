package pulse

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// Exercise production splitting and encoding, not a separately rebuilt payload.
func TestPulseContractWire(t *testing.T) {
	dir := os.Getenv("PULSE_CONTRACT_OUTPUT")
	if dir == "" {
		t.Skip("set PULSE_CONTRACT_OUTPUT via scripts/check-pulse-contract.ts")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no collector fixtures")
	}
	wire := filepath.Join(dir, "wire")
	if err := os.Mkdir(wire, 0700); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var batch Batch
			if err := json.Unmarshal(data, &batch); err != nil {
				t.Fatal(err)
			}
			part := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/pulse/ingest" || r.Header.Get("Authorization") != "Bearer contract-token" || r.Header.Get("Idempotency-Key") == "" {
					t.Error("invalid ingest request")
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				if err := os.WriteFile(filepath.Join(wire, fmt.Sprintf("%s-%03d.json", filepath.Base(file), part)), body, 0600); err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				part++
				var accepted Batch
				if err := json.Unmarshal(body, &accepted); err != nil {
					t.Error(err)
					w.WriteHeader(400)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]int{"metrics": len(accepted.Metrics), "events": len(accepted.Events), "states": len(accepted.States)})
			}))
			defer server.Close()
			c := Client{URL: server.URL + "/api/pulse/ingest", Token: "contract-token", HTTPClient: server.Client()}
			if err := c.PostBatch(context.Background(), batch); err != nil {
				t.Fatal(err)
			}
		})
	}
}
