package backup

import (
	"context"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMarkerAndCommandTimes(t *testing.T) {
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	last := now.Add(-time.Hour)
	path := filepath.Join(t.TempDir(), "ok")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, last, last); err != nil {
		t.Fatal(err)
	}
	for _, marker := range []Marker{{Name: "db", MarkerFile: path}, {Name: "db", Command: []string{"fake", "rfc"}}, {Name: "db", Command: []string{"fake", "unix"}}} {
		c := Collector{Markers: []Marker{marker}, Run: func(ctx context.Context, _ string, args ...string) ([]byte, error) {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("no deadline")
			}
			if args[0] == "rfc" {
				return []byte(last.Format(time.RFC3339)), nil
			}
			return []byte("1767351600"), nil
		}}
		b, err := c.Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "vm"}, Timestamp: now})
		if err != nil {
			t.Fatal(err)
		}
		if len(b.Metrics) != 1 || b.Metrics[0].Value != 3600 || b.Metrics[0].Resource.Key() != "backup:vm:db" {
			t.Fatalf("batch=%+v", b)
		}
	}
}
func TestMissingInvalidAndOversizedMarkers(t *testing.T) {
	c := Collector{Markers: []Marker{{Name: "missing", MarkerFile: filepath.Join(t.TempDir(), "absent")}}}
	b, err := c.Collect(context.Background(), monitoring.Scope{})
	if err != nil || len(b.Metrics) != 0 || len(b.Events) != 0 || b.States[0].Value != false {
		t.Fatalf("missing: %+v %v", b, err)
	}
	for _, body := range []string{"secret-invalid-time", strings.Repeat("x", 4097)} {
		c = Collector{Markers: []Marker{{Name: "db", Command: []string{"fake"}}}, Run: func(context.Context, string, ...string) ([]byte, error) { return []byte(body), nil }}
		b, err := c.Collect(context.Background(), monitoring.Scope{})
		if err != nil || len(b.Events) != 1 || len(b.Metrics) != 0 || strings.Contains(b.Events[0].Attributes["error"].(string), body) {
			t.Fatalf("invalid: %+v %v", b, err)
		}
	}
}
