package uptime

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

const (
	KindICMP = "icmp"
	KindDNS  = "dns"
	KindTCP  = "tcp"
	KindHTTP = "http"
)

type Target struct {
	ID             string
	Label          string
	Kind           string
	Address        string
	ExpectedStatus int
	Timeout        time.Duration
}

type Collector struct {
	Targets     []Target
	Concurrency int
	Timeout     time.Duration
	Resolver    *net.Resolver
	HTTPClient  *http.Client
	Ping        func(context.Context, string) error
}

type result struct {
	measured      bool
	success       bool
	duration      time.Duration
	err           string
	addresses     []string
	statusCode    int
	finalURL      string
	httpProtocol  string
	tlsServerName string
	tlsIssuer     string
	tlsExpiresAt  time.Time
}

func (c Collector) Name() string { return "uptime" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
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
		tb.State("uptime.check.measured", outcome.measured, dims)
		tb.State("uptime.check.success", outcome.success, dims)
		tb.State("uptime.check.type", target.Kind, dims)
		tb.State("uptime.check.target", target.Address, dims)
		tb.State("uptime.check.error", outcome.err, dims)
		if outcome.measured {
			availability := 0.0
			if outcome.success {
				availability = 1
			}
			tb.Metric("uptime.check.availability", "gauge", availability, "", dims)
			tb.Metric("uptime.check.duration", "gauge", durationMilliseconds(outcome.duration), "milliseconds", dims)
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
	timeout := target.Timeout
	if timeout <= 0 {
		timeout = c.Timeout
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	start := time.Now()

	var outcome result
	switch target.Kind {
	case KindICMP:
		outcome = c.checkICMP(ctx, target.Address)
	case KindDNS:
		outcome = c.checkDNS(ctx, target.Address)
	case KindTCP:
		outcome = c.checkTCP(ctx, target.Address)
	case KindHTTP:
		outcome = c.checkHTTP(ctx, target)
	default:
		outcome = result{err: "unsupported check type"}
	}
	outcome.duration = time.Since(start)
	return outcome
}

func (c Collector) checkICMP(ctx context.Context, address string) result {
	ping := c.Ping
	if ping == nil {
		ping = runPing
	}
	err := ping(ctx, address)
	if err == nil {
		return result{measured: true, success: true}
	}
	if errors.Is(err, exec.ErrNotFound) {
		return result{measured: false, success: false, err: "ping command unavailable"}
	}
	if ctx.Err() != nil {
		return result{measured: true, success: false, err: "timeout"}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		return result{measured: false, success: false, err: "ping command failed"}
	}
	return result{measured: true, success: false, err: "ping failed"}
}

func (c Collector) checkDNS(ctx context.Context, address string) result {
	resolver := c.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addresses, err := resolver.LookupHost(ctx, address)
	if err != nil {
		if ctx.Err() != nil {
			return result{measured: true, success: false, err: "timeout"}
		}
		return result{measured: true, success: false, err: "DNS lookup failed"}
	}
	sort.Strings(addresses)
	return result{measured: true, success: len(addresses) > 0, addresses: addresses}
}

func (c Collector) checkTCP(ctx context.Context, address string) result {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", address)
	if err != nil {
		if ctx.Err() != nil {
			return result{measured: true, success: false, err: "timeout"}
		}
		return result{measured: true, success: false, err: "TCP connection failed"}
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
			return result{measured: true, success: false, err: "timeout"}
		}
		return result{measured: true, success: false, err: "HTTP request failed"}
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))

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

func runPing(ctx context.Context, address string) error {
	return exec.CommandContext(ctx, "ping", "-n", "-c", "1", address).Run()
}

func endpointScope(scope monitoring.Scope, target Target) monitoring.Scope {
	scope.EntityType = "uptime-endpoint"
	scope.EntityID = entity.ID("uptime-endpoint", probeID(scope), target.ID)
	scope.Label = target.Label
	return scope
}

func probeID(scope monitoring.Scope) string {
	if value := scope.Dimensions["probe"]; value != "" {
		return entity.Key(value, "probe")
	}
	value := strings.TrimPrefix(scope.EntityID, "uptime-probe:")
	return entity.Key(value, "probe")
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}

func DefaultTargets(timeout time.Duration) []Target {
	return []Target{
		{ID: "cloudflare-icmp", Label: "Cloudflare ICMP", Kind: KindICMP, Address: "1.1.1.1", Timeout: timeout},
		{ID: "google-icmp", Label: "Google ICMP", Kind: KindICMP, Address: "8.8.8.8", Timeout: timeout},
		{ID: "cloudflare-dns", Label: "Cloudflare DNS", Kind: KindDNS, Address: "cloudflare.com", Timeout: timeout},
		{ID: "google-dns", Label: "Google DNS", Kind: KindDNS, Address: "google.com", Timeout: timeout},
		{ID: "cloudflare-tcp", Label: "Cloudflare HTTPS TCP", Kind: KindTCP, Address: "1.1.1.1:443", Timeout: timeout},
		{ID: "google-tcp", Label: "Google DNS TCP", Kind: KindTCP, Address: "8.8.8.8:53", Timeout: timeout},
		{ID: "cloudflare-http", Label: "Cloudflare HTTP", Kind: KindHTTP, Address: "https://www.cloudflare.com/cdn-cgi/trace", ExpectedStatus: 200, Timeout: timeout},
		{ID: "google-http", Label: "Google HTTP", Kind: KindHTTP, Address: "https://www.google.com/generate_204", ExpectedStatus: 204, Timeout: timeout},
	}
}
