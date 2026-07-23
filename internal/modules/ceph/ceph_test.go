package ceph

import (
	"testing"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestParseStatus(t *testing.T) {
	status, err := parseStatus([]byte(`{
  "fsid": "abcd-1234",
  "health": {"status": "HEALTH_WARN"},
  "quorum_names": ["mon-a", "mon-b"],
  "osdmap": {"osdmap": {"num_osds": 3, "num_up_osds": 2, "num_in_osds": 2}},
  "pgmap": {
    "num_pgs": 128,
    "bytes_used": 1000,
    "bytes_total": 4000,
    "bytes_avail": 3000,
    "pgs_by_state": [
      {"state_name": "active+clean", "count": 120},
      {"state_name": "active+degraded", "count": 8}
    ]
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if status.Health != "HEALTH_WARN" || status.OSDsTotal != 3 || status.OSDsUp != 2 || status.OSDsIn != 2 {
		t.Fatalf("status = %#v", status)
	}
	if status.FSID != "abcd-1234" {
		t.Fatalf("fsid = %q", status.FSID)
	}
	if status.BytesUsed != 1000 || status.BytesAvailable != 3000 || status.PGsByState["active+degraded"] != 8 {
		t.Fatalf("pgmap = %#v", status)
	}
	if len(status.QuorumMonitors) != 2 || status.QuorumMonitors[0] != "mon-a" {
		t.Fatalf("quorum = %#v", status.QuorumMonitors)
	}
}

func TestParsePools(t *testing.T) {
	pools, err := parsePools([]byte(`{
  "pools": [
    {"name": "rbd", "stats": {"bytes_used": 1024, "max_avail": 2048, "objects": 12}},
    {"name": "cephfs_data", "stats": {"bytes_used": 4096, "max_avail": 8192, "objects": 99}},
    {"name": "", "stats": {"bytes_used": 1, "max_avail": 2, "objects": 3}}
  ]
}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 2 {
		t.Fatalf("pools = %d", len(pools))
	}
	if pools[0].Name != "rbd" || pools[0].BytesUsed != 1024 || pools[0].BytesAvailable != 2048 || pools[0].Objects != 12 {
		t.Fatalf("first pool = %#v", pools[0])
	}
	if pools[1].Name != "cephfs_data" || pools[1].Objects != 99 {
		t.Fatalf("second pool = %#v", pools[1])
	}
}

func TestCephUnavailableMarkers(t *testing.T) {
	if !isCephUnavailable([]byte("ObjectNotFound('RADOS object not found')"), nil) {
		t.Fatal("expected rados object not found to be treated as unavailable")
	}
	if isCephUnavailable([]byte("permission denied"), nil) {
		t.Fatal("permission errors should stay visible")
	}
}

func TestCephScopesUseResourceEntities(t *testing.T) {
	scope := monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{
			"host": "server-01",
		},
	}

	cluster := cephClusterScope(scope, "abcd-1234")
	if cluster.EntityType != "ceph-cluster" || cluster.EntityID != "ceph-cluster:abcd-1234" {
		t.Fatalf("cluster scope = %#v", cluster)
	}
	pool := cephPoolScope(cluster, "rbd")
	if pool.EntityType != "ceph-pool" || pool.EntityID != "ceph-pool:abcd-1234:rbd" {
		t.Fatalf("pool scope = %#v", pool)
	}
	fallback := cephClusterScope(scope, "")
	if fallback.EntityID != "ceph-cluster:server-01" {
		t.Fatalf("fallback cluster scope = %#v", fallback)
	}
}
