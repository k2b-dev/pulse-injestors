package zfs

import (
	"testing"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestParsePoolList(t *testing.T) {
	pools := parsePoolList([]byte("tank\t1099511627776\t274877906944\t824633720832\t25\t3\tONLINE\nbackup\t1000\t500\t500\t50%\t-\tDEGRADED\n"))
	if len(pools) != 2 {
		t.Fatalf("pools = %d", len(pools))
	}
	if pools[0].Name != "tank" || pools[0].Size != 1099511627776 || pools[0].Capacity != 25 || pools[0].Fragmentation != 3 || pools[0].Health != "ONLINE" {
		t.Fatalf("first pool = %#v", pools[0])
	}
	if pools[1].Name != "backup" || pools[1].Fragmentation != -1 || pools[1].Health != "DEGRADED" {
		t.Fatalf("second pool = %#v", pools[1])
	}
}

func TestParseDatasetList(t *testing.T) {
	datasets := parseDatasetList([]byte("tank/app\tfilesystem\t1024\t2048\t512\t1.23x\t/tank/app\ntank/vol\tvolume\t4096\t8192\t4096\t-\t-\n"))
	if len(datasets) != 2 {
		t.Fatalf("datasets = %d", len(datasets))
	}
	if datasets[0].Name != "tank/app" || datasets[0].Type != "filesystem" || datasets[0].Used != 1024 || datasets[0].CompressRatio != 1.23 || datasets[0].Mountpoint != "/tank/app" {
		t.Fatalf("first dataset = %#v", datasets[0])
	}
	if datasets[1].Type != "volume" || datasets[1].CompressRatio != -1 {
		t.Fatalf("second dataset = %#v", datasets[1])
	}
}

func TestParseSnapshotCounts(t *testing.T) {
	counts := parseSnapshotCounts([]byte("tank/app@daily-1\ntank/app@daily-2\ntank/db@hourly\ninvalid\n"))
	if counts["tank/app"] != 2 || counts["tank/db"] != 1 {
		t.Fatalf("counts = %#v", counts)
	}
	if _, ok := counts["invalid"]; ok {
		t.Fatalf("invalid snapshot counted: %#v", counts)
	}
}

func TestParsePoolScans(t *testing.T) {
	scans := parsePoolScans([]byte(`
  pool: tank
 state: ONLINE
  scan: scrub repaired 0B in 00:01:04 with 0 errors on Tue Jun  9 20:00:00 2026
config:

  pool: backup
 state: ONLINE
  scan: none requested
`))
	if len(scans) != 2 {
		t.Fatalf("scans = %d", len(scans))
	}
	if scans[0].Pool != "tank" || scans[0].Status != "scrub" || scans[0].Errors != 0 || scans[0].CompletedAt != "Tue Jun  9 20:00:00 2026" {
		t.Fatalf("first scan = %#v", scans[0])
	}
	if scans[1].Pool != "backup" || scans[1].Status != "none" || scans[1].Errors != -1 {
		t.Fatalf("second scan = %#v", scans[1])
	}
}

func TestZFSScopeUsesResourceEntities(t *testing.T) {
	scope := monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{
			"host": "server-01",
		},
	}

	pool := poolScope(scope, "tank")
	if pool.EntityType != "zfs-pool" || pool.EntityID != "zfs-pool:server-01:tank" {
		t.Fatalf("pool scope = %#v", pool)
	}
	dataset := datasetScope(scope, "tank/app")
	if dataset.EntityType != "zfs-dataset" || dataset.EntityID != "zfs-dataset:server-01:tank_app" {
		t.Fatalf("dataset scope = %#v", dataset)
	}
}
