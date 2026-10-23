package valkey

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe/probetest"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

const info = `# Server
valkey_version:8.0.1
role:master
connected_clients:4
rejected_connections:2
rdb_changes_since_last_save:7
rdb_last_bgsave_status:ok
aof_enabled:1
aof_last_write_status:ok
used_memory:1024
maxmemory:4096
evicted_keys:3
db0:keys=5,expires=1,avg_ttl=0
db1:keys=7,expires=0,avg_ttl=0
`

// fake accepts one connection and only AUTH/INFO, never key/value commands.
func fake(t *testing.T, tc *tls.Config, auth []string, authReply, reply string) (string, <-chan struct{}) {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	if tc != nil {
		l = tls.NewListener(l, tc)
	}
	done := make(chan struct{})
	t.Cleanup(func() { l.Close(); <-done })
	go func() {
		defer close(done)
		conn, e := l.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		read := func() []string {
			line, e := r.ReadString('\n')
			if e != nil {
				return nil
			}
			var n int
			if _, e = fmt.Sscanf(line, "*%d\r\n", &n); e != nil || n > 8 {
				return nil
			}
			var args []string
			for range n {
				b, e := readBulk(r)
				if e != nil {
					return nil
				}
				args = append(args, string(b))
			}
			return args
		}
		if len(auth) > 0 {
			got := read()
			if got == nil {
				return
			}
			if !reflect.DeepEqual(got, auth) {
				t.Error("AUTH mismatch")
			}
			io.WriteString(conn, authReply)
			if authReply != "+OK\r\n" {
				return
			}
		}
		got := read()
		if got == nil {
			return
		}
		if !reflect.DeepEqual(got, []string{"INFO", "server", "clients", "memory", "persistence", "stats", "replication", "keyspace"}) {
			t.Error("unexpected command")
		}
		io.WriteString(conn, reply)
	}()
	return l.Addr().String(), done
}
func bulk(s string) string { return fmt.Sprintf("$%d\r\n%s\r\n", len(s), s) }
func TestTLSAuthAndGroups(t *testing.T) {
	certServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer certServer.Close()
	ca := probetest.CA(t, certServer)
	secret := "fixture-valkey-secret"
	for _, g := range []string{"health", "capacity", "version"} {
		address, done := fake(t, certServer.TLS, []string{"AUTH", "monitor", secret}, "+OK\r\n", bulk(info))
		b := probetest.Collect(t, Collector{Config: config.ValkeyConfig{Address: address, ServerName: "example.com", CAFile: ca, Username: "monitor", PasswordFile: probetest.File(t, secret)}, Group: g})
		<-done
		probetest.NoSecret(t, b, "", secret)
		switch g {
		case "health":
			probetest.State(t, b, "valkey.available", true)
			probetest.State(t, b, "valkey.role", "master")
			probetest.State(t, b, "valkey.persistence.ok", true)
			probetest.Metric(t, b, "valkey.clients", 4, "")
		case "capacity":
			probetest.Metric(t, b, "valkey.keys", 12, "")
			probetest.Metric(t, b, "valkey.memory.max", 4096, "bytes")
		case "version":
			probetest.State(t, b, "valkey.version", "8.0.1")
		}
	}
	address, _ := fake(t, certServer.TLS, nil, "", bulk(info))
	probetest.Failure(t, Collector{Config: config.ValkeyConfig{Address: address, CAFile: ca, ServerName: "wrong.invalid"}}, "valkey", secret)
}
func TestRESPFailuresAndSecrets(t *testing.T) {
	secret := "fixture-valkey-secret"
	plain := false
	for _, reply := range []string{"-ERR " + secret + "\r\n", "$1048577\r\n", "$-1\r\n", "$3\r\nx\r\n", bulk(secret), bulk(strings.Replace(info, "connected_clients:4", "connected_clients:NaN", 1))} {
		address, _ := fake(t, nil, []string{"AUTH", secret}, "+OK\r\n", reply)
		probetest.Failure(t, Collector{Config: config.ValkeyConfig{Address: address, TLS: &plain, PasswordFile: probetest.File(t, secret)}}, "valkey", secret)
	}
	address, _ := fake(t, nil, []string{"AUTH", secret}, "-ERR "+secret+"\r\n", "")
	probetest.Failure(t, Collector{Config: config.ValkeyConfig{Address: address, TLS: &plain, PasswordFile: probetest.File(t, secret)}}, "valkey", secret)
	address, _ = fake(t, nil, []string{"AUTH", secret}, "+OK\r\n", bulk(strings.Replace(info, "8.0.1", secret, 1)))
	probetest.NoSecret(t, probetest.Collect(t, Collector{Config: config.ValkeyConfig{Address: address, TLS: &plain, PasswordFile: probetest.File(t, secret)}, Group: "version"}), "", secret)
}
func TestUnlimitedMemoryAndPersistence(t *testing.T) {
	plain := false
	address, _ := fake(t, nil, nil, "", bulk(strings.Replace(info, "maxmemory:4096", "maxmemory:0", 1)))
	b := probetest.Collect(t, Collector{Config: config.ValkeyConfig{Address: address, TLS: &plain}, Group: "capacity"})
	for _, m := range b.Metrics {
		if m.Name == "valkey.memory.max" {
			t.Fatal("unlimited memory emitted as zero")
		}
	}
	address, _ = fake(t, nil, nil, "", bulk(strings.Replace(info, "aof_last_write_status:ok", "aof_last_write_status:err", 1)))
	b = probetest.Collect(t, Collector{Config: config.ValkeyConfig{Address: address, TLS: &plain}})
	probetest.State(t, b, "valkey.persistence.ok", false)
	for _, raw := range []string{"x", strings.Repeat("x", serviceprobe.MaxBytes+1)} {
		if _, e := parseInfo([]byte(raw)); e == nil {
			t.Fatal("bad INFO accepted")
		}
	}
}

func TestDuplicateInfoKeysAndPrimaryRole(t *testing.T) {
	plain := false
	address, _ := fake(t, nil, nil, "", bulk(info+"role:primary\nconnected_clients:9\n"))
	b := probetest.Collect(t, Collector{Config: config.ValkeyConfig{Address: address, TLS: &plain}})
	probetest.State(t, b, "valkey.role", "primary")
	probetest.Metric(t, b, "valkey.clients", 9, "")
	for _, s := range b.States {
		if s.Key == "valkey.up" {
			t.Fatal("redundant state")
		}
	}
}
