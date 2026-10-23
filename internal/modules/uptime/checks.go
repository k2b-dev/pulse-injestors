package uptime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func portRange(value string) (int, int, error) {
	if value == "" {
		return 1, 10000, nil
	}
	a, z, ok := strings.Cut(value, "-")
	lo, e1 := strconv.Atoi(a)
	hi, e2 := strconv.Atoi(z)
	if !ok || e1 != nil || e2 != nil || lo < 1 || hi > 65535 || lo > hi {
		return 0, 0, errors.New("invalid port range")
	}
	return lo, hi, nil
}
func portList(ports []int) string {
	values := make([]string, len(ports))
	for i, port := range ports {
		values[i] = strconv.Itoa(port)
	}
	return strings.Join(values, ",")
}
func (c Collector) checkPorts(ctx context.Context, target Target) result {
	lo, hi, err := portRange(target.Range)
	if err != nil {
		return result{err: err.Error()}
	}
	host := strings.Trim(target.Address, "[]")
	// Resolve once so a scan cannot silently mix different hosts behind round-robin DNS.
	addresses, err := c.resolver().LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return result{err: "scan host resolution failed"}
	}
	if len(addresses) != 1 {
		return result{err: "scan host has multiple addresses; configure one target per IP"}
	}
	host = addresses[0].String()
	expected := map[int]bool{}
	for _, port := range target.Expected {
		if port < lo || port > hi {
			return result{err: "expected port outside scan range"}
		}
		expected[port] = true
	}
	type scanned struct {
		done     bool
		open     bool
		filtered bool
		err      error
	}
	results := make([]scanned, hi-lo+1)
	jobs := make(chan int)
	dial := c.PortDial
	if dial == nil {
		dial = func(ctx context.Context, address string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "tcp", address)
		}
	}
	var wg sync.WaitGroup
	for i := 0; i < min(64, len(results)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for port := range jobs {
				conn, err := dial(ctx, net.JoinHostPort(host, strconv.Itoa(port)))
				results[port-lo].done = true
				var netErr net.Error
				if err == nil {
					conn.Close()
					results[port-lo].open = true
				} else if errors.Is(err, syscall.ECONNREFUSED) {
					// A refusal confirms that the host answered and the port is closed.
				} else if ctx.Err() == nil && errors.As(err, &netErr) && netErr.Timeout() {
					results[port-lo].filtered = true
				} else {
					results[port-lo].err = err
				}
			}
		}()
	}
dispatch:
	for port := lo; port <= hi; port++ {
		select {
		case jobs <- port:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	out := result{measured: true, success: true}
	scanError := ctx.Err() != nil
	refused := 0
	for i, port := range results {
		n := lo + i
		if !port.done || port.err != nil {
			scanError = true
			continue
		}
		if port.open {
			out.open++
			if !expected[n] {
				out.unexpected = append(out.unexpected, n)
			}
		} else {
			if port.filtered {
				out.filtered++
			} else {
				refused++
			}
			if expected[n] {
				out.missing = append(out.missing, n)
			}
		}
	}
	if len(out.unexpected)+len(out.missing) > 0 {
		out.success = false
		out.err = "unexpected open ports: " + portList(out.unexpected) + "; missing expected ports: " + portList(out.missing)
	}
	if scanError {
		out.measured = false
		out.success = false
		out.err = "scan incomplete: connection error or scan budget exhausted; " + out.err
	} else if out.open == 0 && refused == 0 {
		out.measured = false
		out.success = false
		out.err = "scan host gave no answer: cannot distinguish host down from all ports filtered; " + out.err
	}
	return out
}
func (c Collector) resolver() *net.Resolver {
	if c.Resolver != nil {
		return c.Resolver
	}
	return net.DefaultResolver
}

func (c Collector) checkTLS(ctx context.Context, target Target) result {
	host, _, err := net.SplitHostPort(target.Address)
	if err != nil {
		return result{err: "invalid TLS address"}
	}
	serverName := target.ServerName
	if serverName == "" {
		serverName = host
	}
	var roots *x509.CertPool
	if target.CAFile != "" {
		data, err := os.ReadFile(target.CAFile)
		if err != nil {
			return result{err: "cannot read TLS CA file"}
		}
		roots = x509.NewCertPool()
		if !roots.AppendCertsFromPEM(data) {
			return result{err: "TLS CA file contains no certificates"}
		}
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", target.Address)
	if err != nil {
		return result{measured: errors.Is(err, syscall.ECONNREFUSED), err: "TLS connection failed"}
	}
	defer conn.Close()
	// Verify explicitly after handshake so expiry and issuer remain available for
	// an untrusted or expired certificate. This connection carries no application data.
	tc := tls.Client(conn, &tls.Config{ServerName: serverName, InsecureSkipVerify: true}) //nolint:gosec
	defer tc.Close()
	if err := tc.HandshakeContext(ctx); err != nil {
		return result{err: "TLS handshake failed"}
	}
	state := tc.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return result{err: "TLS certificate missing"}
	}
	now := time.Now()
	if c.Now != nil {
		now = c.Now()
	}
	leaf := state.PeerCertificates[0]
	out := result{measured: true, success: true, tlsServerName: serverName, tlsIssuer: leaf.Issuer.CommonName, tlsExpiresAt: leaf.NotAfter}
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: serverName, Roots: roots, Intermediates: intermediates, CurrentTime: now}); err != nil {
		out.success = false
		out.err = "TLS verification failed: " + err.Error()
		return out
	}
	days := target.WarnDays
	if days == 0 {
		days = 14
	}
	if leaf.NotAfter.Sub(now) < time.Duration(days)*24*time.Hour {
		out.success = false
		out.err = fmt.Sprintf("TLS certificate expires in less than %d days", days)
	}
	return out
}

func normalizeRecord(typ, value string) string {
	value = strings.TrimSpace(value)
	switch typ {
	case "A", "AAAA":
		if ip := net.ParseIP(value); ip != nil {
			return ip.String()
		}
	case "MX":
		fields := strings.Fields(value)
		if len(fields) == 2 {
			return fields[0] + " " + strings.ToLower(strings.TrimSuffix(fields[1], "."))
		}
	case "NS", "CNAME":
		return strings.ToLower(strings.TrimSuffix(value, "."))
	}
	// TXT content is case-sensitive (SPF/DMARC values can carry case-sensitive data).
	return value
}
func recordSet(typ string, values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[normalizeRecord(typ, value)] = true
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
func (c Collector) checkRecords(ctx context.Context, target Target) result {
	out := result{measured: true, success: true}
	var messages []string
	for _, record := range target.Records {
		typ := strings.ToUpper(record.Type)
		values, absent, err := lookupRecords(ctx, target.Resolver, target.Name, typ)
		if err != nil || absent || len(values) == 0 {
			out.measured = false
			out.success = false
			messages = append(messages, typ+": DNS lookup failed or answer missing")
			continue
		}
		actual := recordSet(typ, values)
		if record.Contains != "" {
			matched := false
			for _, value := range actual {
				if strings.Contains(value, record.Contains) {
					matched = true
				}
			}
			if !matched {
				out.success = false
				messages = append(messages, fmt.Sprintf("%s missing substring %q; got %q", typ, record.Contains, actual))
			}
		} else {
			expected := recordSet(typ, record.Expect)
			if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
				out.success = false
				messages = append(messages, fmt.Sprintf("%s expected %q; got %q", typ, expected, actual))
			}
		}
	}
	out.err = strings.Join(messages, "; ")
	return out
}

type listResult struct {
	name string
	result
}

func (c Collector) checkDNSBL(ctx context.Context, target Target) result {
	ip := net.ParseIP(target.Address).To4()
	if ip == nil {
		return result{err: "DNSBL requires IPv4"}
	}
	out := result{measured: true, success: true}
	for _, list := range target.Lists {
		list = strings.ToLower(strings.TrimSuffix(list, "."))
		lr := listResult{name: list}
		reference, absent, err := lookupRecords(ctx, target.Resolver, "2.0.0.127."+list, "A")
		if err != nil || absent || !dnsblAnswer(reference) {
			lr.err = "DNSBL reference check failed"
		} else {
			values, absent, err := lookupRecords(ctx, target.Resolver, fmt.Sprintf("%d.%d.%d.%d.%s", ip[3], ip[2], ip[1], ip[0], list), "A")
			switch {
			case err != nil:
				lr.err = "DNSBL lookup failed"
			case absent:
				lr.measured = true
				lr.success = true
			case dnsblAnswer(values):
				lr.measured = true
				lr.err = "IP listed"
			default:
				lr.err = "DNSBL answer missing or invalid"
			}
		}
		if !lr.measured {
			out.measured = false
		}
		if !lr.success {
			out.success = false
		}
		if lr.err != "" {
			if out.err != "" {
				out.err += "; "
			}
			out.err += list + ": " + lr.err
		}
		out.lists = append(out.lists, lr)
	}
	return out
}
func dnsblAnswer(values []string) bool {
	if len(values) == 0 {
		return false
	}
	for _, value := range values {
		ip := net.ParseIP(value).To4()
		if ip == nil || ip[0] != 127 || ip[1] != 0 || ip[2] != 0 {
			return false
		}
	}
	return true
}
