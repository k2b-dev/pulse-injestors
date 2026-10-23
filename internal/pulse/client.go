package pulse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	URL            string
	Token          string
	HTTPClient     *http.Client
	MaxRetries     int
	InitialBackoff time.Duration
	Logger         *slog.Logger
	// Stats is optional; delivery counters and clock skew are recorded there.
	Stats *Stats
}

const (
	maxItemsPerIngestCollection = 500
	maxItemsPerIngestPayload    = 1500
	maxBytesPerIngestPayload    = 5 * 1024 * 1024
	maxBackoff                  = 5 * time.Minute
)

// Outcome classifies one ingest attempt.
type Outcome int

const (
	// Delivered: Pulse acknowledged the batch.
	Delivered Outcome = iota
	// Retry: transport failure, 408, 429 or 5xx; retry the same body soon.
	Retry
	// Paused: 3xx, 401, 403, 404 or non-idempotency 409; the source, token or base is not writable
	// right now. Keep the body and retry slowly until an operator fixes it.
	Paused
	// Rejected: idempotency conflicts or any other 4xx; the body itself is invalid and never succeeds.
	Rejected
)

func (o Outcome) String() string {
	switch o {
	case Delivered:
		return "delivered"
	case Retry:
		return "retry"
	case Paused:
		return "paused"
	default:
		return "rejected"
	}
}

// Result describes one attempt. Code and Message come from Pulse's error body.
type Result struct {
	Outcome    Outcome
	Status     int
	Code       string
	Message    string
	RetryAfter time.Duration
	Err        error
}

func (r Result) Error() string {
	if r.Outcome == Delivered {
		return ""
	}
	if r.Status == 0 {
		if r.Err != nil {
			return r.Err.Error()
		}
		return "pulse request failed"
	}
	parts := []string{fmt.Sprintf("pulse ingest returned HTTP %d", r.Status)}
	if r.Code != "" {
		parts = append(parts, r.Code)
	}
	if r.Message != "" {
		parts = append(parts, r.Message)
	}
	return strings.Join(parts, ": ")
}

func (c *Client) init() {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	httpClient := *c.HTTPClient
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	c.HTTPClient = &httpClient
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = 500 * time.Millisecond
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
}

// PostBatch splits and sends a batch without persistence. Each part is retried
// in place; paused and rejected parts end the call with an error.
func (c Client) PostBatch(ctx context.Context, batch Batch) error {
	c.init()
	parts, err := splitBatch(batch, maxItemsPerIngestCollection, maxItemsPerIngestPayload, maxBytesPerIngestPayload)
	if err != nil {
		return err
	}
	for i, part := range parts {
		body, err := json.Marshal(part)
		if err != nil {
			return fmt.Errorf("encode pulse batch: %w", err)
		}
		if result := c.SendWithRetries(ctx, body); result.Outcome != Delivered {
			return fmt.Errorf("post pulse batch part %d/%d: %s", i+1, len(parts), result.Error())
		}
	}
	return nil
}

// SendWithRetries retries Retry outcomes up to MaxRetries times, honoring
// Retry-After. Paused and rejected outcomes return immediately.
func (c Client) SendWithRetries(ctx context.Context, body []byte) Result {
	c.init()
	var result Result
	for attempt := 0; ; attempt++ {
		result = c.Send(ctx, body)
		if result.Outcome != Retry || attempt >= c.MaxRetries {
			return result
		}
		wait := max(Backoff(c.InitialBackoff, attempt), result.RetryAfter)
		c.Logger.Warn("pulse batch send failed; retrying", "attempt", attempt+1, "wait", wait, "err", result.Error())
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return Result{Outcome: Retry, Err: ctx.Err()}
		case <-timer.C:
		}
	}
}

// Backoff doubles initial per attempt and caps the delay at five minutes.
func Backoff(initial time.Duration, attempt int) time.Duration {
	backoff := initial
	for n := 0; n < attempt && backoff < maxBackoff; n++ {
		backoff *= 2
	}
	return min(backoff, maxBackoff)
}

// Send makes exactly one attempt. The idempotency key is derived from the body,
// so a retried body always carries the same key.
func (c Client) Send(ctx context.Context, body []byte) Result {
	c.init()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return Result{Outcome: Rejected, Err: errors.New("invalid pulse request configuration")}
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", IdempotencyKey(body))

	started := time.Now()
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		c.Stats.addFailure()
		if ctx.Err() != nil {
			return Result{Outcome: Retry, Err: ctx.Err()}
		}
		// Transport errors can contain the URL but never the token.
		return Result{Outcome: Retry, Err: errors.New("pulse request transport failed")}
	}
	defer resp.Body.Close()
	c.Stats.observeServerTime(resp.Header.Get("Date"), started, time.Now())

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if !validAcknowledgement(resp.Body) {
			c.Stats.addFailure()
			return Result{Outcome: Retry, Status: resp.StatusCode, Message: "invalid acknowledgement; delivery is uncertain"}
		}
		c.Stats.addDelivered()
		return Result{Outcome: Delivered, Status: resp.StatusCode}
	}

	c.Stats.addFailure()
	code, message := readErrorBody(resp.Body)
	result := Result{
		Status:     resp.StatusCode,
		Code:       code,
		Message:    message,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now()),
	}
	switch {
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		result.Outcome = Retry
	case resp.StatusCode == http.StatusConflict && strings.Contains(strings.ToLower(message), "idempotency key"):
		result.Outcome = Rejected
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		result.Outcome = Paused
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden ||
		resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusConflict:
		// Pulse answers 404 for a disabled source or a base that is being cleared.
		result.Outcome = Paused
	default:
		result.Outcome = Rejected
	}
	return result
}

func validAcknowledgement(body io.Reader) bool {
	var counts struct {
		Metrics *uint64 `json:"metrics"`
		Events  *uint64 `json:"events"`
		States  *uint64 `json:"states"`
	}
	decoder := json.NewDecoder(io.LimitReader(body, 4097))
	if err := decoder.Decode(&counts); err != nil || counts.Metrics == nil || counts.Events == nil || counts.States == nil {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}

const maxErrorMessage = 300

// readErrorBody extracts Pulse's {code, message}. Pulse never echoes credentials.
func readErrorBody(body io.Reader) (string, string) {
	data, _ := io.ReadAll(io.LimitReader(body, 8192))
	var parsed struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &parsed) != nil {
		return "", ""
	}
	return cleanText(parsed.Code, 80), cleanText(parsed.Message, maxErrorMessage)
}

func cleanText(value string, limit int) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, strings.TrimSpace(value))
	if runes := []rune(value); len(runes) > limit {
		value = string(runes[:limit]) + "…"
	}
	return value
}

func IdempotencyKey(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("pulse-ingestor-%x", sum)
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		if seconds > int64((24*time.Hour)/time.Second) {
			return 24 * time.Hour
		}
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(value); err == nil && when.After(now) {
		return min(when.Sub(now), 24*time.Hour)
	}
	return 0
}
