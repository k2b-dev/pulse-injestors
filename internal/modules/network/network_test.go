package network

import (
	"testing"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
)

func TestParseNetDev(t *testing.T) {
	rows, err := parseNetDev([]byte(`Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 100 2 3 4 0 0 0 0 200 5 6 7 0 0 0 0
  eth0: 300 8 9 10 0 0 0 0 400 11 12 13 0 0 0 0
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("len=%d", len(rows))
	}
	if rows[1].Interface != "eth0" || rows[1].RxBytes != 300 || rows[1].TxDropped != 13 {
		t.Fatalf("row=%#v", rows[1])
	}
}

func TestInterfaceScopeUsesResourceEntity(t *testing.T) {
	scope := interfaceScope(monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{
			"host": "server-01",
		},
	}, "eth0")

	if scope.EntityType != "network-interface" || scope.EntityID != "network-interface:server-01:eth0" {
		t.Fatalf("scope = %#v", scope)
	}
}
