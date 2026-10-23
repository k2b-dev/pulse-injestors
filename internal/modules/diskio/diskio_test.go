package diskio

import (
	"context"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiskstatsWholeDevicesAndUnits(t *testing.T) {
	root := t.TempDir()
	sys := t.TempDir()
	var lines []string
	for _, name := range []string{"sda", "sda1", "vda", "vda2", "xvdb", "nvme0n1", "nvme0n1p1", "hda", "mmcblk0", "mmcblk0p1", "md0", "dm-0", "dm-1", "loop0", "ram0", "zram0", "sr0", "fd0"} {
		lines = append(lines, "8 0 "+name+" 10 2 20 3000 40 5 60 7000 0 9000 10000")
	}
	if err := os.WriteFile(filepath.Join(root, "diskstats"), []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(sys, "class/block/dm-1/dm"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sys, "class/block/dm-1/dm/uuid"), []byte("part1-mpath-abc"), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := (Collector{ProcRoot: root, SysRoot: sys}).Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "host:a"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	if len(batch.Metrics) != 8*7 {
		t.Fatalf("metrics=%d", len(batch.Metrics))
	}
	expected := map[string]float64{"read_bytes": 10240, "write_bytes": 30720, "reads": 10, "writes": 40, "busy_time": 9, "read_time": 3, "write_time": 7}
	for _, m := range batch.Metrics {
		key := strings.TrimPrefix(m.Name, "system.disk.io.")
		if m.Type != "counter" || m.Value != expected[key] {
			t.Fatalf("metric=%+v", m)
		}
		if m.Resource.ID != "host%3Aa:%2Fdev%2F"+m.Resource.Label {
			t.Fatalf("resource=%v", m.Resource)
		}
	}
}
func TestDiskstatsMissingAndMalformed(t *testing.T) {
	c := Collector{ProcRoot: t.TempDir(), SysRoot: t.TempDir()}
	batch, err := c.Collect(context.Background(), monitoring.Scope{})
	if err != nil || len(batch.States) != 1 || batch.States[0].Value != false {
		t.Fatalf("%+v %v", batch, err)
	}
	if err := os.WriteFile(filepath.Join(c.ProcRoot, "diskstats"), []byte("8 0 sda broken\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Collect(context.Background(), monitoring.Scope{}); err == nil {
		t.Fatal("malformed row accepted")
	}
}
