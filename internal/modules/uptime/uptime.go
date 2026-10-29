package uptime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

const (
	KindICMP       = "icmp"
	KindDNS        = "dns"
	KindTCP        = "tcp"
	KindHTTP       = "http"
	KindPorts      = "ports"
	KindDNSRecords = "dns_records"
	KindTLS        = "tls"
	KindDNSBL      = "dnsbl"
)

type Record struct {
	Type     string
	Expect   []string
	Contains string
}
type Target struct {
	Count          int
	Interval       time.Duration
	LossThreshold  *float64
	RatePerSecond  int
	Cadence        time.Duration
	Name           string
	Resolver       string
	Expected       []int
	Range          string
	Records        []Record
	CAFile         string
	ServerName     string
	WarnDays       int
	Lists          []string
	ID             string
	Label          string
	Kind           string
	Address        string
	ExpectedStatus int
	Timeout        time.Duration
}

type Collector struct {
	Every       time.Duration
	Now         func() time.Time
	Targets     []Target
	Concurrency int
	Timeout     time.Duration
	Resolver    *net.Resolver
	HTTPClient  *http.Client
	Ping        func(context.Context, Target) ([]byte, error)
	PortDial    func(context.Context, string) (net.Conn, error)
}

type result struct {
	icmp                pingStats
	unexpected, missing []int
	open, filtered      int
	lists               []listResult
	measured            bool
	success             bool
	duration            time.Duration
	err                 string
	addresses           []string
	statusCode          int
	finalURL            string
	httpProtocol        string
	tlsServerName       string
	tlsIssuer           string
	tlsExpiresAt        time.Time
}

func (c Collector) Name() string {
	if c.Every > 0 {
		return fmt.Sprintf("uptime-%ds", int(c.Every.Seconds()))
	}
	return "uptime"
}
func (c Collector) Cadence() time.Duration { return c.Every }
func (c Collector) CollectTimeout() time.Duration {
	// Include every target even when a worker exhausts its individual budget.
	var total time.Duration
	for _, target := range c.Targets {
		total += c.targetTimeout(target)
	}
	return total
}
func DefaultCadence(kind string) time.Duration {
	switch kind {
	case KindPorts:
		return 24 * time.Hour
	case KindDNSRecords, KindTLS, KindDNSBL:
		return time.Hour
	default:
		return time.Minute
	}
}

// Scheduled groups checks with the same cadence, preserving bounded parallelism.
func (c Collector) Scheduled() []monitoring.Collector {
	groups := map[time.Duration][]Target{}
	for _, target := range c.Targets {
		every := target.Cadence
		if every <= 0 {
			every = DefaultCadence(target.Kind)
		}
		groups[every] = append(groups[every], target)
	}
	var cadences []time.Duration
	for every := range groups {
		cadences = append(cadences, every)
	}
	sort.Slice(cadences, func(i, j int) bool { return cadences[i] < cadences[j] })
	var collectors []monitoring.Collector
	for _, every := range cadences {
		group := c
		group.Every = every
		group.Targets = groups[every]
		collectors = append(collectors, group)
	}
	return collectors
}
func (c Collector) targetTimeout(target Target) time.Duration {
	if target.Timeout > 0 {
		return target.Timeout
	}
	if target.Kind == KindPorts {
		return 900 * time.Second
	}
	if target.Kind == KindICMP {
		return max(c.Timeout, target.ICMPTimeout())
	}
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 5 * time.Second
}
func (r result) state() string {
	if !r.measured {
		return "error"
	}
	if !r.success {
		return "failed"
	}
	return "ok"
}
func bounded(value string) string {
	runes := []rune(value)
	if len(runes) > 500 {
		return string(runes[:499]) + "…"
	}
	return value
}
func emitResult(b *monitoring.Builder, r result, dims map[string]string) {
	b.State("uptime.check.result", r.state(), dims)
	b.State("uptime.check.message", bounded(r.err), dims)
	if r.measured {
		value := 0.0
		if r.success {
			value = 1
		}
		b.Metric("uptime.check.success", "gauge", value, "", dims)
	}
}

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now()
		if c.Now != nil {
			scope.Timestamp = c.Now()
		}
	}
	if len(c.Targets) == 0 {
		return pulse.Batch{}, errors.New("no uptime targets configured")
	}
	concurrency := c.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	results := make([]result, len(c.Targets))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, target := range c.Targets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[i] = result{measured: false, success: false, err: "collector cancelled"}
				return
			}
			results[i] = c.check(ctx, target)
		}()
	}
	wg.Wait()

	b := monitoring.NewBuilder(scope)
	for i, target := range c.Targets {
		targetScope := endpointScope(scope, target)
		tb := monitoring.NewBuilder(targetScope)
		dims := map[string]string{
			"probe":      probeID(scope),
			"endpoint":   target.ID,
			"check_type": target.Kind,
		}
		outcome := results[i]
		emitResult(tb, outcome, dims)
		tb.State("uptime.check.type", target.Kind, dims)
		address := target.Address
		if target.Kind == KindDNSRecords {
			address = target.Name
		}
		tb.State("uptime.check.target", address, dims)
		if target.Kind == KindICMP {
			if outcome.measured {
				tb.Metric("uptime.icmp.loss", "gauge", outcome.icmp.loss, "percent", dims)
				if outcome.icmp.received > 0 {
					tb.Metric("uptime.icmp.rtt.min", "gauge", outcome.icmp.min, "milliseconds", dims)
					tb.Metric("uptime.icmp.rtt.median", "gauge", outcome.icmp.median, "milliseconds", dims)
					tb.Metric("uptime.icmp.rtt.max", "gauge", outcome.icmp.max, "milliseconds", dims)
					tb.Metric("uptime.icmp.rtt.stddev", "gauge", outcome.icmp.stddev, "milliseconds", dims)
				}
			}
		} else {
			tb.Metric("uptime.check.duration", "gauge", durationMilliseconds(outcome.duration), "milliseconds", dims)
		}
		if target.Kind == KindPorts {
			if outcome.measured || len(outcome.unexpected) > 0 {
				tb.State("uptime.ports.unexpected", bounded(portList(outcome.unexpected)), dims)
			}
			if outcome.measured || len(outcome.missing) > 0 {
				tb.State("uptime.ports.missing", bounded(portList(outcome.missing)), dims)
			}
			if outcome.measured {
				tb.Metric("uptime.ports.open", "gauge", float64(outcome.open), "", dims)
				tb.Metric("uptime.ports.filtered", "gauge", float64(outcome.filtered), "", dims)
			}
		}
		for _, list := range outcome.lists {
			ld := monitoring.MergeDimensions(dims, map[string]string{"list": list.name})
			emitResult(tb, list.result, ld)
			if list.measured {
				tb.State("uptime.dnsbl.listed", !list.success, ld)
			}
		}

		if target.Kind == KindDNS {
			tb.State("uptime.dns.addresses", strings.Join(outcome.addresses, ", "), dims)
			if outcome.measured {
				tb.Metric("uptime.dns.address_count", "gauge", float64(len(outcome.addresses)), "", dims)
			}
		}
		if target.Kind == KindHTTP {
			tb.State("uptime.http.status_code", outcome.statusCode, dims)
			tb.State("uptime.http.final_url", outcome.finalURL, dims)
			tb.State("uptime.http.protocol", outcome.httpProtocol, dims)
		}
		if target.Kind == KindHTTP || target.Kind == KindTLS {
			tb.State("uptime.tls.server_name", outcome.tlsServerName, dims)
			tb.State("uptime.tls.issuer", outcome.tlsIssuer, dims)
			expiresAt := ""
			if !outcome.tlsExpiresAt.IsZero() {
				expiresAt = outcome.tlsExpiresAt.UTC().Format(time.RFC3339)
				expiresIn := outcome.tlsExpiresAt.Sub(scope.Timestamp).Seconds()
				tb.Metric("uptime.tls.certificate.expires_in", "gauge", expiresIn, "seconds", dims)
			}
			tb.State("uptime.tls.expires_at", expiresAt, dims)
		}
		b.Merge(tb.Batch())
	}
	return b.Batch(), nil
}

func (c Collector) check(parent context.Context, target Target) result {
	timeout := c.targetTimeout(target)
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	start := time.Now()

	var outcome result
	switch target.Kind {
	case KindICMP:
		outcome = c.checkICMP(ctx, target)
	case KindDNS:
		outcome = c.checkDNS(ctx, target.Address)
	case KindTCP:
		outcome = c.checkTCP(ctx, target.Address)
	case KindHTTP:
		outcome = c.checkHTTP(ctx, target)
	case KindPorts:
		outcome = c.checkPorts(ctx, target)
	case KindDNSRecords:
		outcome = c.checkRecords(ctx, target)
	case KindTLS:
		outcome = c.checkTLS(ctx, target)
	case KindDNSBL:
		outcome = c.checkDNSBL(ctx, target)
	default:
		outcome = result{err: "unsupported check type"}
	}
	if ctx.Err() != nil {
		outcome.measured = false
		outcome.success = false
		outcome.err = "timeout or cancellation"
	}
	outcome.duration = time.Since(start)
	return outcome
}

func (c Collector) checkDNS(ctx context.Context, address string) result {
	resolver := c.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupHost(ctx, address)
	if err != nil {
		if ctx.Err() != nil {
			return result{err: "timeout"}
		}
		return result{err: "DNS lookup failed"}
	}
	if len(addresses) == 0 {
		return result{err: "DNS answer missing"}
	}
	sort.Strings(addresses)
	return result{measured: true, success: len(addresses) > 0, addresses: addresses}
}

func (c Collector) checkTCP(ctx context.Context, address string) result {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		if ctx.Err() != nil {
			return result{err: "timeout"}
		}
		return result{measured: errors.Is(err, syscall.ECONNREFUSED), err: "TCP connection failed"}
	}
	_ = conn.Close()
	return result{measured: true, success: true}
}

func (c Collector) checkHTTP(ctx context.Context, target Target) result {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Address, nil)
	if err != nil {
		return result{measured: false, success: false, err: "invalid HTTP target"}
	}
	req.Header.Set("User-Agent", "pulse-uptime")
	client := c.httpClient()
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return result{err: "timeout"}
		}
		return result{measured: errors.Is(err, syscall.ECONNREFUSED), err: "HTTP request failed"}
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024)); err != nil {
		return result{err: "HTTP response read failed"}
	}

	success := response.StatusCode >= 200 && response.StatusCode < 400
	if target.ExpectedStatus != 0 {
		success = response.StatusCode == target.ExpectedStatus
	}
	outcome := result{
		measured:     true,
		success:      success,
		statusCode:   response.StatusCode,
		finalURL:     response.Request.URL.String(),
		httpProtocol: response.Proto,
	}
	if !success {
		outcome.err = "unexpected HTTP status"
	}
	if response.TLS != nil && len(response.TLS.PeerCertificates) > 0 {
		certificate := response.TLS.PeerCertificates[0]
		outcome.tlsServerName = certificate.Subject.CommonName
		outcome.tlsIssuer = certificate.Issuer.CommonName
		outcome.tlsExpiresAt = certificate.NotAfter
	}
	return outcome
}

func (c Collector) httpClient() *http.Client {
	var client http.Client
	if c.HTTPClient != nil {
		client = *c.HTTPClient
	} else {
		client.Transport = http.DefaultTransport
	}
	client.CheckRedirect = func(_ *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("redirect limit reached")
		}
		return nil
	}
	return &client
}

func endpointScope(scope monitoring.Scope, target Target) monitoring.Scope {
	scope.Resource.Type = "uptime-endpoint"
	scope.Resource.ID = entity.ID(probeID(scope), target.ID)
	scope.Resource.Label = target.Label
	return scope
}

func probeID(scope monitoring.Scope) string {
	if value := scope.Dimensions["probe"]; value != "" {
		return entity.Key(value, "probe")
	}
	value := scope.Resource.ID
	return entity.Key(value, "probe")
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

func DefaultTargets(timeout time.Duration) []Target {
	return []Target{
		{ID: "cloudflare-icmp", Label: "Cloudflare ICMP", Kind: KindICMP, Address: "1.1.1.1", Timeout: max(timeout, (Target{}).ICMPTimeout())},
		{ID: "google-icmp", Label: "Google ICMP", Kind: KindICMP, Address: "8.8.8.8", Timeout: max(timeout, (Target{}).ICMPTimeout())},
		{ID: "cloudflare-dns", Label: "Cloudflare DNS", Kind: KindDNS, Address: "cloudflare.com", Timeout: timeout},
		{ID: "google-dns", Label: "Google DNS", Kind: KindDNS, Address: "google.com", Timeout: timeout},
		{ID: "cloudflare-tcp", Label: "Cloudflare HTTPS TCP", Kind: KindTCP, Address: "1.1.1.1:443", Timeout: timeout},
		{ID: "google-tcp", Label: "Google DNS TCP", Kind: KindTCP, Address: "8.8.8.8:53", Timeout: timeout},
		{ID: "cloudflare-http", Label: "Cloudflare HTTP", Kind: KindHTTP, Address: "https://www.cloudflare.com/cdn-cgi/trace", ExpectedStatus: 200, Timeout: timeout},
		{ID: "google-http", Label: "Google HTTP", Kind: KindHTTP, Address: "https://www.google.com/generate_204", ExpectedStatus: 204, Timeout: timeout},
	}
}
