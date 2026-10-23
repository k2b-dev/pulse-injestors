package valkey

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
)

type Collector struct {
	Config config.ValkeyConfig
	Group  string
}

func (c Collector) Name() string {
	if c.Group == "" || c.Group == "health" {
		return "valkey"
	}
	return "valkey-" + c.Group
}
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceprobe.Timeout)
	defer cancel()
	b := monitoring.NewBuilder(scope)
	group := c.Group
	if group == "" {
		group = "health"
	}
	e := c.collect(ctx, b, group)
	return serviceprobe.Finish(b, "valkey", group, e)
}
func (c Collector) collect(ctx context.Context, b *monitoring.Builder, group string) error {
	pass, e := serviceprobe.Password(c.Config.PasswordFile)
	if e != nil {
		return e
	}
	dialer := net.Dialer{Timeout: serviceprobe.Timeout}
	var conn net.Conn
	if c.Config.TLS == nil || *c.Config.TLS {
		tc, e := serviceprobe.TLS(c.Config.CAFile, c.Config.ServerName, false)
		if e != nil {
			return e
		}
		td := tls.Dialer{NetDialer: &dialer, Config: tc}
		conn, e = td.DialContext(ctx, "tcp", c.Config.Address)
		if e != nil {
			return serviceprobe.ErrResponse
		}
	} else {
		conn, e = dialer.DialContext(ctx, "tcp", c.Config.Address)
		if e != nil {
			return serviceprobe.ErrResponse
		}
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline := time.Now().Add(serviceprobe.Timeout)
	if d, ok := ctx.Deadline(); ok {
		deadline = d
	}
	if e = conn.SetDeadline(deadline); e != nil {
		return serviceprobe.ErrResponse
	}
	reader := bufio.NewReaderSize(conn, 4096)
	if pass != "" {
		args := []string{"AUTH", pass}
		if c.Config.Username != "" {
			args = []string{"AUTH", c.Config.Username, pass}
		}
		if e = writeCommand(conn, args...); e != nil {
			return serviceprobe.ErrResponse
		}
		line, e := reader.ReadSlice('\n')
		if e != nil || string(line) != "+OK\r\n" {
			return serviceprobe.ErrResponse
		}
	}
	if e = writeCommand(conn, "INFO", "server", "clients", "memory", "persistence", "stats", "replication", "keyspace"); e != nil {
		return serviceprobe.ErrResponse
	}
	raw, e := readBulk(reader)
	if e != nil {
		return e
	}
	fields, e := parseInfo(raw)
	if e != nil {
		return e
	}
	if group == "version" {
		version := fields["valkey_version"]
		if version == "" {
			version = fields["redis_version"]
		}
		if version == "" {
			return serviceprobe.ErrResponse
		}
		b.State("valkey.version", serviceprobe.Text(version, pass), nil)
		return nil
	}
	number := func(key string) (float64, error) { return serviceprobe.Number(fields[key]) }
	if group == "capacity" {
		used, e := number("used_memory")
		if e != nil {
			return e
		}
		max, e := number("maxmemory")
		if e != nil {
			return e
		}
		evicted, e := number("evicted_keys")
		if e != nil {
			return e
		}
		keys := float64(0)
		for k, v := range fields {
			if strings.HasPrefix(k, "db") {
				if _, e := strconv.ParseUint(strings.TrimPrefix(k, "db"), 10, 32); e != nil {
					continue
				}
				found := false
				for _, item := range strings.Split(v, ",") {
					key, value, ok := strings.Cut(item, "=")
					if ok && key == "keys" {
						n, e := serviceprobe.Number(value)
						if e != nil {
							return e
						}
						keys += n
						found = true
					}
				}
				if !found {
					return serviceprobe.ErrResponse
				}
			}
		}
		b.Metric("valkey.memory.used", "gauge", used, "bytes", nil)
		if max > 0 {
			b.Metric("valkey.memory.max", "gauge", max, "bytes", nil)
		}
		b.Metric("valkey.evicted_keys", "counter", evicted, "", nil)
		b.Metric("valkey.keys", "gauge", keys, "", nil)
		return nil
	}
	role := fields["role"]
	if role != "master" && role != "primary" && role != "slave" && role != "replica" {
		return serviceprobe.ErrResponse
	}
	clients, e := number("connected_clients")
	if e != nil {
		return e
	}
	rejected, e := number("rejected_connections")
	if e != nil {
		return e
	}
	changes, e := number("rdb_changes_since_last_save")
	if e != nil {
		return e
	}
	rdb := fields["rdb_last_bgsave_status"]
	aof := fields["aof_enabled"]
	if (rdb != "ok" && rdb != "err") || (aof != "0" && aof != "1") {
		return serviceprobe.ErrResponse
	}
	aofStatus := fields["aof_last_write_status"]
	if aof == "1" && aofStatus != "ok" && aofStatus != "err" {
		return serviceprobe.ErrResponse
	}
	b.State("valkey.role", role, nil)
	b.State("valkey.persistence.ok", rdb == "ok" && (aof == "0" || aofStatus == "ok"), nil)
	b.Metric("valkey.clients", "gauge", clients, "", nil)
	b.Metric("valkey.rejected_connections", "counter", rejected, "", nil)
	b.Metric("valkey.rdb.changes_since_save", "gauge", changes, "", nil)
	return nil
}
func writeCommand(w io.Writer, args ...string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, arg := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(arg), arg)
	}
	_, e := io.WriteString(w, b.String())
	return e
}
func readBulk(r *bufio.Reader) ([]byte, error) {
	line, e := r.ReadSlice('\n')
	if e != nil || len(line) < 4 || line[0] != '$' || line[len(line)-2] != '\r' {
		return nil, serviceprobe.ErrResponse
	}
	n, e := strconv.Atoi(string(line[1 : len(line)-2]))
	if e != nil || n < 0 || n > serviceprobe.MaxBytes {
		return nil, serviceprobe.ErrResponse
	}
	raw := make([]byte, n+2)
	if _, e = io.ReadFull(r, raw); e != nil || string(raw[n:]) != "\r\n" {
		return nil, serviceprobe.ErrResponse
	}
	return raw[:n], nil
}
func parseInfo(raw []byte) (map[string]string, error) {
	if len(raw) > serviceprobe.MaxBytes {
		return nil, serviceprobe.ErrResponse
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, v, ok := strings.Cut(line, ":")
		if !ok || key == "" {
			return nil, serviceprobe.ErrResponse
		}
		m[key] = v
	}
	return m, nil
}
