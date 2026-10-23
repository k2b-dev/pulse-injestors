package network

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

// DefaultExclude removes transient container links while retaining host bridges and VPNs.
var DefaultExclude = []string{"lo", "veth*", "docker*", "br-*", "virbr*", "cni*", "flannel*", "cali*", "vxlan*", "tap*", "fwbr*", "fwpr*", "fwln*", "kube-*", "cilium*", "lxc*"}

type Collector struct {
	ProcRoot string
	Exclude  []string
}

func (c Collector) Name() string { return "network" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	_ = ctx
	proc := c.ProcRoot
	if proc == "" {
		proc = "/proc"
	}
	b := monitoring.NewBuilder(scope)
	rows, err := readNetDev(filepath.Join(proc, "net", "dev"))
	if err != nil {
		b.State("system.network.available", false, nil)
		return b.Batch(), err
	}
	b.State("system.network.available", true, nil)
	exclude := c.Exclude
	if exclude == nil {
		exclude = DefaultExclude
	}
	for _, row := range rows {
		skip := false
		for _, pattern := range exclude {
			match, err := filepath.Match(pattern, row.Interface)
			if err != nil {
				return b.Batch(), err
			}
			skip = skip || match
		}
		if skip {
			continue
		}
		dims := map[string]string{"interface": row.Interface}
		ib := monitoring.NewBuilder(interfaceScope(scope, row.Interface))
		ib.Metric("system.network.rx", "counter", float64(row.RxBytes), "bytes", dims)
		ib.Metric("system.network.rx_errors", "counter", float64(row.RxErrors), "", dims)
		ib.Metric("system.network.rx_dropped", "counter", float64(row.RxDropped), "", dims)
		ib.Metric("system.network.tx", "counter", float64(row.TxBytes), "bytes", dims)
		ib.Metric("system.network.tx_errors", "counter", float64(row.TxErrors), "", dims)
		ib.Metric("system.network.tx_dropped", "counter", float64(row.TxDropped), "", dims)
		b.Merge(ib.Batch())
	}
	return b.Batch(), nil
}

func interfaceScope(scope monitoring.Scope, iface string) monitoring.Scope {
	scope.Resource.Type = "network-interface"
	scope.Resource.ID = entity.ID(entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions), iface)
	scope.Resource.Label = iface
	return scope
}

type NetDevRow struct {
	Interface string
	RxBytes   uint64
	RxPackets uint64
	RxErrors  uint64
	RxDropped uint64
	TxBytes   uint64
	TxPackets uint64
	TxErrors  uint64
	TxDropped uint64
}

func readNetDev(path string) ([]NetDevRow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseNetDev(data)
}

func parseNetDev(data []byte) ([]NetDevRow, error) {
	var rows []NetDevRow
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		iface := strings.TrimSpace(parts[0])
		values := strings.Fields(parts[1])
		if len(values) < 16 {
			continue
		}
		num := func(i int) uint64 {
			v, _ := strconv.ParseUint(values[i], 10, 64)
			return v
		}
		rows = append(rows, NetDevRow{
			Interface: iface,
			RxBytes:   num(0),
			RxPackets: num(1),
			RxErrors:  num(2),
			RxDropped: num(3),
			TxBytes:   num(8),
			TxPackets: num(9),
			TxErrors:  num(10),
			TxDropped: num(11),
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no interfaces in net/dev")
	}
	return rows, nil
}
