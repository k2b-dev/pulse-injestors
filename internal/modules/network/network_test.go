package network

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
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
		Resource: pulse.ResourceRef{Type: "host", ID: "server-01"},
	}, "eth0")

	if scope.Resource.Type != "network-interface" || scope.Resource.Key() != "network-interface:server-01:eth0" {
		t.Fatalf("scope = %#v", scope)
	}
}

func TestInterfaceExclusionsAndResourceIDs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0755); err != nil {
		t.Fatal(err)
	}
	names := []string{"lo", "veth123", "docker0", "br-123", "virbr0", "cni0", "flannel.1", "cali123", "vxlan0", "tap0", "fwbr1", "fwpr1", "fwln1", "kube-ipvs0", "cilium0", "lxc0", "vmbr0", "bond0", "eth0", "enp1s0", "wg0", "tun0"}
	var body strings.Builder
	for _, name := range names {
		fmt.Fprintf(&body, "%s: 100 2 3 4 0 0 0 0 200 5 6 7 0 0 0 0\n", name)
	}
	if err := os.WriteFile(filepath.Join(root, "net/dev"), []byte(body.String()), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		exclude []string
		count   int
	}{{nil, 6}, {[]string{}, len(names)}, {[]string{"eth*"}, len(names) - 1}} {
		batch, err := (Collector{ProcRoot: root, Exclude: tc.exclude}).Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "host", ID: "server-01"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.Metrics) != tc.count*6 {
			t.Fatalf("metrics=%d want=%d", len(batch.Metrics), tc.count*6)
		}
		for _, m := range batch.Metrics {
			if m.Type != "counter" || strings.Contains(m.Name, "packets") || m.Dimensions["host"] != "" {
				t.Fatalf("metric: %+v", m)
			}
			if m.Resource.Key() != "network-interface:server-01:"+m.Dimensions["interface"] {
				t.Fatalf("resource %v", m.Resource)
			}
		}
	}
}
