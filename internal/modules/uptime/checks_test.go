package uptime

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

// A minimal authoritative UDP responder; every query stays on loopback.
func fakeDNS(t *testing.T, reply func(string, uint16) (int, [][]byte)) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			buf := make([]byte, 4096)
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			query := buf[:n]
			name, pos, err := dnsName(query, 12)
			if err != nil || pos+4 > n {
				continue
			}
			qt := binary.BigEndian.Uint16(query[pos : pos+2])
			code, answers := reply(name, qt)
			if code < 0 {
				continue
			}
			response := append([]byte(nil), query[:pos+4]...)
			binary.BigEndian.PutUint16(response[2:4], uint16(0x8180|code))
			binary.BigEndian.PutUint16(response[6:8], uint16(len(answers)))
			for _, data := range answers {
				response = append(response, 0xc0, 0x0c, byte(qt>>8), byte(qt), 0, 1, 0, 0, 0, 60, byte(len(data)>>8), byte(len(data)))
				response = append(response, data...)
			}
			_, _ = conn.WriteTo(response, addr)
		}
	}()
	t.Cleanup(func() { conn.Close(); wg.Wait() })
	return conn.LocalAddr().String()
}
func wireName(name string) []byte {
	var b []byte
	for _, p := range strings.Split(strings.TrimSuffix(name, "."), ".") {
		b = append(b, byte(len(p)))
		b = append(b, p...)
	}
	return append(b, 0)
}
func fixedScope() monitoring.Scope {
	return monitoring.Scope{Resource: pulse.ResourceRef{Type: "uptime-probe", ID: "test", Label: "Test"}, Timestamp: time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)}
}
func collectTarget(t *testing.T, target Target, c Collector) pulse.Batch {
	t.Helper()
	target.ID = "target"
	target.Label = "Target"
	c.Targets = []Target{target}
	batch, err := c.Collect(context.Background(), fixedScope())
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.Batch(batch); err != nil {
		t.Fatal(err)
	}
	return batch
}
func assertOutcome(t *testing.T, batch pulse.Batch, expected string) {
	t.Helper()
	if got := stateValue(batch, "uptime.check.result", "target"); got != expected {
		t.Fatalf("result = %v want %s; states=%+v", got, expected, batch.States)
	}
	want := 1.0
	if expected == "failed" {
		want = 0
	}
	if expected == "error" {
		want = -1
	}
	if got := metricValue(batch, "uptime.check.success", "target"); got != want {
		t.Fatalf("success=%v want %v", got, want)
	}
	if metricValue(batch, "uptime.check.duration", "target") < 0 {
		t.Fatal("missing duration")
	}
	for _, s := range batch.States {
		if v, ok := s.Value.(string); ok && (s.Key == "uptime.check.message" || strings.HasPrefix(s.Key, "uptime.ports.")) && len([]rune(v)) > 500 {
			t.Fatal("unbounded state")
		}
	}
}
func TestDNSRecordsSetsAndErrors(t *testing.T) {
	records := []Record{{Type: "A", Expect: []string{"192.0.2.1"}}, {Type: "AAAA", Expect: []string{"2001:db8::1"}}, {Type: "MX", Expect: []string{"10 MX.Example.TEST."}}, {Type: "NS", Expect: []string{"NS.Example.TEST."}}, {Type: "CNAME", Expect: []string{"Alias.Example.TEST."}}, {Type: "TXT", Contains: "v=spf1"}}
	for _, tc := range []struct {
		name            string
		code            int
		empty, mismatch bool
		want            string
	}{{"success", 0, false, false, "ok"}, {"mismatch", 0, false, true, "failed"}, {"empty", 0, true, false, "error"}, {"NXDOMAIN", 3, false, false, "error"}, {"SERVFAIL", 2, false, false, "error"}, {"REFUSED", 5, false, false, "error"}, {"timeout", -1, false, false, "error"}} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := fakeDNS(t, func(_ string, qt uint16) (int, [][]byte) {
				if tc.empty {
					return tc.code, nil
				}
				var data []byte
				switch qt {
				case 1:
					data = []byte{192, 0, 2, 1}
					if tc.mismatch {
						data[3] = 2
					}
				case 28:
					data = net.ParseIP("2001:db8::1").To16()
				case 15:
					data = append([]byte{0, 10}, wireName("mx.example.test")...)
				case 2:
					data = wireName("ns.example.test")
				case 5:
					data = wireName("alias.example.test")
				case 16:
					data = append([]byte{6}, []byte("v=spf1")...)
					data = append(data, 5)
					data = append(data, []byte(" -all")...)
				}
				return tc.code, [][]byte{data, data}
			})
			batch := collectTarget(t, Target{Kind: KindDNSRecords, Name: "example.test", Resolver: resolver, Records: records, Timeout: 100 * time.Millisecond}, Collector{})
			assertOutcome(t, batch, tc.want)
		})
	}
}
func TestDNSBLReferenceAndMissingAnswers(t *testing.T) {
	for _, tc := range []struct {
		name          string
		refCode, code int
		ref, answer   []byte
		want          string
		listed        any
	}{
		{"not listed", 0, 3, []byte{127, 0, 0, 2}, nil, "ok", false},
		{"listed", 0, 0, []byte{127, 0, 0, 2}, []byte{127, 0, 0, 3}, "failed", true},
		{"reference NXDOMAIN", 3, 3, nil, nil, "error", nil},
		{"reference SERVFAIL", 2, 3, nil, nil, "error", nil},
		{"reference invalid", 0, 3, []byte{1, 2, 3, 4}, nil, "error", nil},
		{"empty answer", 0, 0, []byte{127, 0, 0, 2}, nil, "error", nil},
		{"query refused", 0, 5, []byte{127, 0, 0, 2}, nil, "error", nil},
		{"query timeout", 0, -1, []byte{127, 0, 0, 2}, nil, "error", nil},
		{"invalid listing", 0, 0, []byte{127, 0, 0, 2}, []byte{127, 255, 255, 254}, "error", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolver := fakeDNS(t, func(name string, _ uint16) (int, [][]byte) {
				code, data := tc.code, tc.answer
				if strings.HasPrefix(name, "2.0.0.127.") {
					code, data = tc.refCode, tc.ref
				}
				if data == nil {
					return code, nil
				}
				return code, [][]byte{data}
			})
			batch := collectTarget(t, Target{Kind: KindDNSBL, Address: "192.0.2.10", Resolver: resolver, Lists: []string{"bl.example.test"}, Timeout: 100 * time.Millisecond}, Collector{})
			assertOutcome(t, batch, tc.want)
			if got := stateValue(batch, "uptime.dnsbl.listed", "target"); got != tc.listed {
				t.Fatalf("listed=%v", got)
			}
			found := false
			for _, s := range batch.States {
				if s.Key == "uptime.check.result" && s.Dimensions["list"] == "bl.example.test" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing per-list result")
			}
		})
	}
}
func TestDNSBLAnyUnavailableListMakesTargetError(t *testing.T) {
	resolver := fakeDNS(t, func(name string, _ uint16) (int, [][]byte) {
		if strings.HasSuffix(name, "bad.test") {
			return 2, nil
		}
		return 0, [][]byte{{127, 0, 0, 2}}
	})
	batch := collectTarget(t, Target{Kind: KindDNSBL, Address: "192.0.2.10", Resolver: resolver, Lists: []string{"listed.test", "bad.test"}}, Collector{})
	assertOutcome(t, batch, "error")
	for _, s := range batch.States {
		if s.Key == "uptime.check.result" && s.Dimensions["list"] == "listed.test" && s.Value != "failed" {
			t.Fatal("lost listed finding")
		}
	}
}
func TestPortScanFindingsAndCancellation(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			host := "127.0.0.1"
			if network == "tcp6" {
				host = "::1"
			}
			listener, err := net.Listen(network, net.JoinHostPort(host, "0"))
			if err != nil {
				t.Skip(err)
			}
			defer listener.Close()
			port := listener.Addr().(*net.TCPAddr).Port
			go func() {
				for {
					conn, err := listener.Accept()
					if err != nil {
						return
					}
					conn.Close()
				}
			}()
			target := Target{Kind: KindPorts, Address: "[" + host + "]", Range: fmt.Sprintf("%d-%d", port, port), Expected: []int{port}}
			assertOutcome(t, collectTarget(t, target, Collector{}), "ok")
			target.Expected = nil
			batch := collectTarget(t, target, Collector{})
			assertOutcome(t, batch, "failed")
			if stateValue(batch, "uptime.ports.unexpected", "target") != strconv.Itoa(port) {
				t.Fatal("unexpected port missing")
			}
			listener.Close()
			target.Expected = []int{port}
			batch = collectTarget(t, target, Collector{})
			assertOutcome(t, batch, "failed")
			if stateValue(batch, "uptime.ports.missing", "target") != strconv.Itoa(port) {
				t.Fatal("missing port missing")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if r := (Collector{}).check(ctx, target); r.state() != "error" {
				t.Fatal(r)
			}
		})
	}
}
func TestTLSVerificationAndExpiry(t *testing.T) {
	server := httptest.NewTLSServer(nil)
	defer server.Close()
	cert := server.Certificate()
	now := cert.NotBefore.Add(24 * time.Hour)
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	target := Target{Kind: KindTLS, Address: strings.TrimPrefix(server.URL, "https://"), CAFile: path}
	c := Collector{Now: func() time.Time { return now }}
	batch := collectTarget(t, target, c)
	assertOutcome(t, batch, "ok")
	if metricValue(batch, "uptime.tls.certificate.expires_in", "target") < 0 {
		t.Fatal("no certificate metric")
	}
	c.Now = func() time.Time { return cert.NotAfter.Add(-24 * time.Hour) }
	assertOutcome(t, collectTarget(t, target, c), "failed")
	c.Now = func() time.Time { return cert.NotAfter.Add(time.Hour) }
	assertOutcome(t, collectTarget(t, target, c), "failed")
	c.Now = func() time.Time { return now }
	target.ServerName = "wrong.example.test"
	assertOutcome(t, collectTarget(t, target, c), "failed")
	target.ServerName = ""
	target.CAFile = ""
	assertOutcome(t, collectTarget(t, target, c), "failed")
	target.CAFile = path + "missing"
	assertOutcome(t, collectTarget(t, target, c), "error")
}
func TestTLSUsesSNI(t *testing.T) {
	server := httptest.NewUnstartedServer(nil)
	names := make(chan string, 1)
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) { names <- hello.ServerName; return nil, nil }}
	server.StartTLS()
	defer server.Close()
	collectTarget(t, Target{Kind: KindTLS, Address: strings.TrimPrefix(server.URL, "https://"), ServerName: "mail.example.test"}, Collector{Now: func() time.Time { return time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC) }})
	if name := <-names; name != "mail.example.test" {
		t.Fatal(name)
	}
}
func TestCadencesKeepFrequentChecksIndependent(t *testing.T) {
	calls := 0
	now := fixedScope().Timestamp
	c := Collector{Ping: func(context.Context, string) error { calls++; return nil }, Targets: []Target{{ID: "minute", Kind: KindICMP}, {ID: "hour", Kind: KindICMP, Cadence: time.Hour}}}
	r := monitoring.Runner{Resource: fixedScope().Resource, Collectors: c.Scheduled(), Interval: time.Minute, Timeout: time.Second, Now: func() time.Time { return now }}
	r.Collect(context.Background(), false)
	if calls != 2 {
		t.Fatal(calls)
	}
	now = now.Add(time.Minute)
	r.Collect(context.Background(), false)
	if calls != 3 {
		t.Fatal(calls)
	}
	now = now.Add(59 * time.Minute)
	r.Collect(context.Background(), false)
	if calls != 5 {
		t.Fatal(calls)
	}
	for kind, want := range map[string]time.Duration{KindICMP: time.Minute, KindDNS: time.Minute, KindTCP: time.Minute, KindHTTP: time.Minute, KindDNSRecords: time.Hour, KindTLS: time.Hour, KindDNSBL: time.Hour, KindPorts: 24 * time.Hour} {
		if DefaultCadence(kind) != want {
			t.Fatal(kind)
		}
	}
	if (Collector{Targets: []Target{{Kind: KindPorts}}}).CollectTimeout() != 900*time.Second {
		t.Fatal("port budget truncated")
	}
}
func TestTimeoutsNeverEmitSuccessGauge(t *testing.T) {
	for _, kind := range []string{KindICMP, KindDNS, KindTCP, KindHTTP, KindTLS, KindPorts, KindDNSRecords, KindDNSBL} {
		target := Target{Kind: kind, Address: "127.0.0.1:1", Name: "example.test", Records: []Record{{Type: "A", Expect: []string{"192.0.2.1"}}}, Resolver: "127.0.0.1:1", Lists: []string{"bl.test"}, Timeout: time.Nanosecond}
		c := Collector{Ping: func(ctx context.Context, _ string) error { <-ctx.Done(); return ctx.Err() }}
		assertOutcome(t, collectTarget(t, target, c), "error")
	}
}

func localTestResolver(t *testing.T) *net.Resolver {
	address := fakeDNS(t, func(_ string, qt uint16) (int, [][]byte) {
		if qt == 1 {
			return 0, [][]byte{{127, 0, 0, 1}}
		}
		if qt == 28 {
			return 0, [][]byte{net.ParseIP("::1").To16()}
		}
		return 0, nil
	})
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
}

func TestPortBudgetKeepsPartialFindings(t *testing.T) {
	var tried atomic.Int32
	c := Collector{PortDial: func(ctx context.Context, address string) (net.Conn, error) {
		tried.Add(1)
		if strings.HasSuffix(address, ":1") {
			a, b := net.Pipe()
			b.Close()
			return a, nil
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	batch := collectTarget(t, Target{Kind: KindPorts, Address: "127.0.0.1", Range: "1-10000", Expected: []int{3, 10000}, Timeout: 50 * time.Millisecond}, c)
	assertOutcome(t, batch, "error")
	if stateValue(batch, "uptime.ports.unexpected", "target") != "1" || stateValue(batch, "uptime.ports.missing", "target") != nil {
		t.Fatalf("lost partial finding or reported unscanned port closed: %+v", batch.States)
	}
	if tried.Load() >= 10000 {
		t.Fatal("fixture did not leave untried ports")
	}
	for _, key := range []string{"uptime.ports.open", "uptime.ports.filtered"} {
		if metricValue(batch, key, "target") != -1 {
			t.Fatalf("incomplete scan emitted %s", key)
		}
	}
}

func TestPortScanFilteredAndConnectionErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ports      []string
		expected   []int
		want       string
		missing    any
		unexpected any
		open       float64
		filtered   float64
	}{
		{"open and filtered", []string{"open", "filtered"}, []int{1}, "ok", "", "", 1, 1},
		{"refused and filtered", []string{"refused", "filtered"}, nil, "ok", "", "", 0, 1},
		{"expected filtered", []string{"open", "filtered"}, []int{1, 2}, "failed", "2", "", 1, 1},
		{"expected refused", []string{"refused", "filtered"}, []int{1}, "failed", "1", "", 0, 1},
		{"unexpected open", []string{"open", "filtered"}, nil, "failed", "", "1", 1, 1},
		{"all refused", []string{"refused", "refused"}, nil, "ok", "", "", 0, 0},
		{"all filtered", []string{"filtered", "filtered"}, nil, "error", nil, nil, -1, -1},
		{"all filtered with expected", []string{"filtered", "filtered"}, []int{2}, "error", "2", nil, -1, -1},
		{"routing error keeps findings", []string{"open", "filtered", "refused", "unreachable"}, []int{2, 3, 4}, "error", "2,3", "1", -1, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tried atomic.Int32
			c := Collector{PortDial: func(_ context.Context, address string) (net.Conn, error) {
				tried.Add(1)
				_, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, err
				}
				n, err := strconv.Atoi(port)
				if err != nil {
					return nil, err
				}
				switch tc.ports[n-1] {
				case "open":
					a, b := net.Pipe()
					b.Close()
					return a, nil
				case "refused":
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
				case "filtered":
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: os.ErrDeadlineExceeded}
				default:
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ENETUNREACH}
				}
			}}
			batch := collectTarget(t, Target{Kind: KindPorts, Address: "127.0.0.1", Range: fmt.Sprintf("1-%d", len(tc.ports)), Expected: tc.expected}, c)
			assertOutcome(t, batch, tc.want)
			if int(tried.Load()) != len(tc.ports) {
				t.Fatalf("tried %d ports, want %d", tried.Load(), len(tc.ports))
			}
			if got := stateValue(batch, "uptime.ports.missing", "target"); got != tc.missing {
				t.Fatalf("missing=%v want %v", got, tc.missing)
			}
			if got := stateValue(batch, "uptime.ports.unexpected", "target"); got != tc.unexpected {
				t.Fatalf("unexpected=%v want %v", got, tc.unexpected)
			}
			for key, want := range map[string]float64{"uptime.ports.open": tc.open, "uptime.ports.filtered": tc.filtered} {
				if got := metricValue(batch, key, "target"); got != want {
					t.Fatalf("%s=%v want %v", key, got, want)
				}
			}
			for _, metric := range batch.Metrics {
				if metric.Name == "uptime.ports.filtered" && (metric.Type != "gauge" || metric.Unit != "") {
					t.Fatalf("invalid filtered metric: %+v", metric)
				}
			}
			if strings.HasPrefix(tc.name, "all filtered") {
				message, _ := stateValue(batch, "uptime.check.message", "target").(string)
				if !strings.Contains(message, "cannot distinguish host down from all ports filtered") {
					t.Fatalf("missing no-answer explanation: %s", message)
				}
			}
		})
	}
}

func TestPortTimeoutDefaultAndOverride(t *testing.T) {
	c := Collector{Timeout: 5 * time.Second}
	if got := c.targetTimeout(Target{Kind: KindPorts}); got != 900*time.Second {
		t.Fatalf("default=%v", got)
	}
	if got := c.targetTimeout(Target{Kind: KindPorts, Timeout: 17 * time.Second}); got != 17*time.Second {
		t.Fatalf("override=%v", got)
	}
}
func TestDNSBLListedAndCleanIsFailed(t *testing.T) {
	resolver := fakeDNS(t, func(name string, _ uint16) (int, [][]byte) {
		if strings.HasPrefix(name, "2.0.0.127.") || strings.HasSuffix(name, "listed.test") {
			return 0, [][]byte{{127, 0, 0, 2}}
		}
		return 3, nil
	})
	assertOutcome(t, collectTarget(t, Target{Kind: KindDNSBL, Address: "192.0.2.10", Resolver: resolver, Lists: []string{"listed.test", "clean.test"}}, Collector{}), "failed")
}
func TestLongMismatchMessageIsBounded(t *testing.T) {
	resolver := fakeDNS(t, func(string, uint16) (int, [][]byte) { return 0, [][]byte{{192, 0, 2, 1}} })
	var expected []string
	for i := 1; i < 255; i++ {
		expected = append(expected, fmt.Sprintf("192.0.2.%d", i))
	}
	batch := collectTarget(t, Target{Kind: KindDNSRecords, Name: "example.test", Resolver: resolver, Records: []Record{{Type: "A", Expect: expected}}}, Collector{})
	assertOutcome(t, batch, "failed")
	message := stateValue(batch, "uptime.check.message", "target").(string)
	if len([]rune(message)) != 500 {
		t.Fatalf("truncation not exercised: %d", len([]rune(message)))
	}
}
