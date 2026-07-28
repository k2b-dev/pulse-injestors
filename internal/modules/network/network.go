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

type Collector struct {
	ProcRoot string
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
	for _, row := range rows {
		dims := map[string]string{"interface": row.Interface}
		ib := monitoring.NewBuilder(interfaceScope(scope, row.Interface))
		ib.Metric("system.network.rx", "counter", float64(row.RxBytes), "bytes", dims)
		ib.Metric("system.network.rx_packets", "counter", float64(row.RxPackets), "count", dims)
		ib.Metric("system.network.rx_errors", "counter", float64(row.RxErrors), "count", dims)
		ib.Metric("system.network.rx_dropped", "counter", float64(row.RxDropped), "count", dims)
		ib.Metric("system.network.tx", "counter", float64(row.TxBytes), "bytes", dims)
		ib.Metric("system.network.tx_packets", "counter", float64(row.TxPackets), "count", dims)
		ib.Metric("system.network.tx_errors", "counter", float64(row.TxErrors), "count", dims)
		ib.Metric("system.network.tx_dropped", "counter", float64(row.TxDropped), "count", dims)
		b.Merge(ib.Batch())
	}
	return b.Batch(), nil
}

func interfaceScope(scope monitoring.Scope, iface string) monitoring.Scope {
	scope.EntityType = "network-interface"
	scope.EntityID = entity.ID("network-interface", entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions), iface)
	scope.Label = iface
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
