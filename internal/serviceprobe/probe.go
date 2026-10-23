// Package serviceprobe owns the bounded transport and credential handling shared
// by the opt-in native service collectors. Errors never include remote content.
package serviceprobe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

const MaxBytes = 1 << 20
const Timeout = 10 * time.Second

var ErrResponse = errors.New("service response missing, invalid or too large")

func ReadFile(path string, limit int) ([]byte, error) {
	f, e := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, errors.New("service file is unreadable")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("service file must be regular")
	}
	return Read(f, limit)
}
func Read(r io.Reader, limit int) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if e != nil || len(b) > limit {
		return nil, ErrResponse
	}
	return b, nil
}
func Password(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	b, e := ReadFile(path, 4096)
	if e != nil {
		return "", e
	}
	s := strings.TrimRight(string(b), "\r\n")
	if s == "" || strings.ContainsAny(s, "\r\n\x00") {
		return "", errors.New("invalid service password file")
	}
	return s, nil
}
func TLS(ca, name string, insecure bool) (*tls.Config, error) {
	c := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: name, InsecureSkipVerify: insecure}
	if ca != "" {
		b, e := ReadFile(ca, MaxBytes)
		if e != nil {
			return nil, e
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(b) {
			return nil, errors.New("invalid service CA file")
		}
		c.RootCAs = pool
	}
	return c, nil
}
func ValidURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}

type HTTP struct {
	URL, CAFile, ServerName, Username, PasswordFile string
	Insecure                                        bool
}
type Client struct {
	client             *http.Client
	base               string
	Password, Username string
}

func (h HTTP) Open() (*Client, error) {
	if !ValidURL(h.URL) {
		return nil, errors.New("invalid service URL")
	}
	pass, e := Password(h.PasswordFile)
	if e != nil {
		return nil, e
	}
	tc, e := TLS(h.CAFile, h.ServerName, h.Insecure)
	if e != nil {
		return nil, e
	}
	// No proxy environment or redirects: credentials belong only to this endpoint.
	tr := &http.Transport{TLSClientConfig: tc, DisableKeepAlives: true, MaxResponseHeaderBytes: 32 << 10}
	return &Client{client: &http.Client{Transport: tr, Timeout: Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: strings.TrimRight(h.URL, "/"), Password: pass, Username: h.Username}, nil
}
func (c *Client) Close() { c.client.CloseIdleConnections() }
func (c *Client) Request(ctx context.Context, method, path, body string, clickhouse bool) ([]byte, int, error) {
	req, e := http.NewRequestWithContext(ctx, method, c.base+path, strings.NewReader(body))
	if e != nil {
		return nil, 0, errors.New("service request invalid")
	}
	if clickhouse {
		req.Header.Set("X-ClickHouse-User", c.Username)
		req.Header.Set("X-ClickHouse-Key", c.Password)
	} else if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}
	resp, e := c.client.Do(req)
	if e != nil {
		return nil, 0, errors.New("service request failed (connection, TLS or timeout)")
	}
	defer resp.Body.Close()
	raw, e := Read(resp.Body, MaxBytes)
	if e != nil {
		return nil, resp.StatusCode, e
	}
	return raw, resp.StatusCode, nil
}
func (c *Client) JSON(ctx context.Context, path string, v any) error {
	raw, status, e := c.Request(ctx, http.MethodGet, path, "", false)
	if e != nil {
		return e
	}
	if status != 200 {
		return errors.New("service HTTP status is not 200")
	}
	return JSON(raw, v)
}
func JSON(raw []byte, v any) error {
	if len(raw) > MaxBytes || json.Unmarshal(raw, v) != nil {
		return ErrResponse
	}
	return nil
}
func Text(s string, secrets ...string) string {
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[redacted]")
		}
	}
	s = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return ' '
		}
		return r
	}, s)
	r := []rune(s)
	if len(r) > 240 {
		s = string(r[:240])
	}
	return s
}
func (c *Client) Text(s string) string {
	return Text(s, c.Password, base64.StdEncoding.EncodeToString([]byte(c.Username+":"+c.Password)))
}
func Number(s string) (float64, error) {
	v, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, ErrResponse
	}
	return v, nil
}

// Finish returns only a fixed, credential-free error. The runner emits exactly
// one ingestor.collector.failed event and retries on the next base tick.
func Finish(b *monitoring.Builder, service, group string, err error) (pulse.Batch, error) {
	b.State(service+".available", err == nil, map[string]string{"collection": group})
	if err != nil {
		return b.Batch(), errors.New(service + " " + group + " collection failed (connection, credentials, timeout or invalid response)")
	}
	return b.Batch(), nil
}
