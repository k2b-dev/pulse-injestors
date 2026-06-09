package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/alecthomas/kong"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
	"github.com/valentinkolb/pulse-injestors/internal/validation"
)

type cli struct {
	Bind     string `name:"bind" help:"Listen address." default:"127.0.0.1:18080" env:"PULSE_MOCK_BIND"`
	Token    string `name:"token" help:"Expected bearer token." default:"test-token" env:"PULSE_MOCK_TOKEN"`
	DumpFile string `name:"dump-file" help:"Write the most recent accepted batch JSON to this path." env:"PULSE_MOCK_DUMP_FILE"`
	Semantic bool   `name:"semantic" help:"Apply semantic batch validation." default:"true" env:"PULSE_MOCK_SEMANTIC"`
}

func main() {
	var c cli
	kong.Parse(&c,
		kong.Name("pulse-mock-server"),
		kong.Description("Minimal local Pulse ingest mock for smoke tests."),
		kong.UsageOnError(),
	)
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+c.Token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			http.Error(w, "content-type must be application/json", http.StatusUnsupportedMediaType)
			return
		}
		defer r.Body.Close()
		var batch pulse.Batch
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20)).Decode(&batch); err != nil {
			http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if len(batch.Metrics) == 0 && len(batch.Events) == 0 && len(batch.States) == 0 {
			http.Error(w, "empty batch", http.StatusBadRequest)
			return
		}
		if c.Semantic {
			if err := validation.Batch(batch); err != nil {
				http.Error(w, "semantic validation failed: "+err.Error(), http.StatusBadRequest)
				return
			}
		}
		if c.DumpFile != "" {
			data, err := json.MarshalIndent(batch, "", "  ")
			if err != nil {
				http.Error(w, "encode dump: "+err.Error(), http.StatusInternalServerError)
				return
			}
			if err := os.WriteFile(c.DumpFile, data, 0o600); err != nil {
				http.Error(w, "write dump: "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
		log.Info("received pulse batch", "metrics", len(batch.Metrics), "events", len(batch.Events), "states", len(batch.States))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, `{"ok":true,"metrics":%d,"events":%d,"states":%d}`+"\n", len(batch.Metrics), len(batch.Events), len(batch.States))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	log.Info("pulse mock listening", "bind", c.Bind)
	if err := http.ListenAndServe(c.Bind, mux); err != nil {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}
