package linuxruntime

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	ProcRoot string
}

func (c Collector) Name() string { return "linuxruntime" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	_ = ctx
	proc := c.ProcRoot
	if proc == "" {
		proc = "/proc"
	}
	b := monitoring.NewBuilder(scope)
	collectPressure(b, proc)
	collectProcesses(b, proc)
	collectSockets(b, proc)
	return b.Batch(), nil
}

type pressureSample struct {
	Resource string
	Scope    string
	Avg10    float64
	Avg60    float64
	Avg300   float64
	Total    float64
}

func collectPressure(b *monitoring.Builder, proc string) {
	dir := filepath.Join(proc, "pressure")
	entries, err := os.ReadDir(dir)
	if err != nil {
		b.State("system.pressure.available", false, nil)
		return
	}
	b.State("system.pressure.available", true, nil)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		resource := entry.Name()
		if resource != "cpu" && resource != "memory" && resource != "io" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, resource))
		if err != nil {
			b.State("system.pressure.resource.available", false, map[string]string{"resource": resource})
			continue
		}
		b.State("system.pressure.resource.available", true, map[string]string{"resource": resource})
		for _, sample := range parsePressure(resource, data) {
			dims := map[string]string{"resource": sample.Resource, "scope": sample.Scope}
			b.Metric("system.pressure.avg10", "gauge", sample.Avg10, "percent", dims)
			b.Metric("system.pressure.avg60", "gauge", sample.Avg60, "percent", dims)
			b.Metric("system.pressure.avg300", "gauge", sample.Avg300, "percent", dims)
			b.Metric("system.pressure.total", "counter", sample.Total, "microseconds", dims)
		}
	}
}

func parsePressure(resource string, data []byte) []pressureSample {
	var out []pressureSample
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		sample := pressureSample{Resource: resource, Scope: fields[0]}
		for _, field := range fields[1:] {
			key, raw, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			value, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				continue
			}
			switch key {
			case "avg10":
				sample.Avg10 = value
			case "avg60":
				sample.Avg60 = value
			case "avg300":
				sample.Avg300 = value
			case "total":
				sample.Total = value
			}
		}
		out = append(out, sample)
	}
	return out
}

func collectProcesses(b *monitoring.Builder, proc string) {
	entries, err := os.ReadDir(proc)
	if err != nil {
		b.State("system.processes.available", false, nil)
		return
	}
	counts := map[string]int{}
	total := 0
	for _, entry := range entries {
		if !entry.IsDir() || !isNumeric(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(proc, entry.Name(), "stat"))
		if err != nil {
			continue
		}
		state := processStateName(parseProcStatState(data))
		counts[state]++
		total++
	}
	b.State("system.processes.available", true, nil)
	b.Metric("system.processes.total", "gauge", float64(total), "count", nil)
	for state, count := range counts {
		b.Metric("system.processes.by_state", "gauge", float64(count), "count", map[string]string{"state": state})
	}
}

func parseProcStatState(data []byte) byte {
	text := strings.TrimSpace(string(data))
	closeParen := strings.LastIndex(text, ")")
	if closeParen < 0 || closeParen+2 >= len(text) {
		return 0
	}
	rest := strings.TrimSpace(text[closeParen+1:])
	if rest == "" {
		return 0
	}
	return rest[0]
}

func processStateName(state byte) string {
	switch state {
	case 'R':
		return "running"
	case 'S':
		return "sleeping"
	case 'D':
		return "disk_sleep"
	case 'Z':
		return "zombie"
	case 'T':
		return "stopped"
	case 't':
		return "tracing_stop"
	case 'X', 'x':
		return "dead"
	case 'I':
		return "idle"
	default:
		return "unknown"
	}
}

type socketSample struct {
	Protocol string
	Family   string
	State    string
	Count    int
}

func collectSockets(b *monitoring.Builder, proc string) {
	files := []struct {
		name     string
		protocol string
		family   string
	}{
		{name: "tcp", protocol: "tcp", family: "ipv4"},
		{name: "tcp6", protocol: "tcp", family: "ipv6"},
		{name: "udp", protocol: "udp", family: "ipv4"},
		{name: "udp6", protocol: "udp", family: "ipv6"},
	}
	hadFile := false
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(proc, "net", file.name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			b.State("system.network.sockets.file.available", false, map[string]string{"file": file.name})
			continue
		}
		hadFile = true
		b.State("system.network.sockets.file.available", true, map[string]string{"file": file.name})
		for _, sample := range parseSockets(file.protocol, file.family, data) {
			dims := map[string]string{
				"protocol": sample.Protocol,
				"family":   sample.Family,
				"state":    sample.State,
			}
			b.Metric("system.network.sockets", "gauge", float64(sample.Count), "count", dims)
		}
	}
	b.State("system.network.sockets.available", hadFile, nil)
}

func parseSockets(protocol, family string, data []byte) []socketSample {
	counts := map[string]int{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if strings.HasPrefix(line, "sl") {
				continue
			}
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		counts[socketStateName(protocol, fields[3])]++
	}
	out := make([]socketSample, 0, len(counts))
	for state, count := range counts {
		out = append(out, socketSample{
			Protocol: protocol,
			Family:   family,
			State:    state,
			Count:    count,
		})
	}
	return out
}

func socketStateName(protocol, raw string) string {
	hex := strings.ToUpper(strings.TrimSpace(raw))
	if protocol == "udp" && hex == "07" {
		return "open"
	}
	switch hex {
	case "01":
		return "established"
	case "02":
		return "syn_sent"
	case "03":
		return "syn_recv"
	case "04":
		return "fin_wait1"
	case "05":
		return "fin_wait2"
	case "06":
		return "time_wait"
	case "07":
		return "close"
	case "08":
		return "close_wait"
	case "09":
		return "last_ack"
	case "0A":
		return "listen"
	case "0B":
		return "closing"
	case "0C":
		return "new_syn_recv"
	default:
		return "unknown_" + strings.ToLower(hex)
	}
}

func isNumeric(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
