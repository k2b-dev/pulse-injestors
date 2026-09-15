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
	"time"
)

type Client struct {
	URL            string
	Token          string
	HTTPClient     *http.Client
	MaxRetries     int
	InitialBackoff time.Duration
	Logger         *slog.Logger
	SpoolDir       string
	SpoolMaxBytes  int64
}

const (
	maxItemsPerIngestCollection = 500
	maxItemsPerIngestPayload    = 1500
	maxBytesPerIngestPayload    = 5 * 1024 * 1024
)

func (c Client) PostBatch(ctx context.Context, batch Batch) error {
	if c.HTTPClient == nil {
		c.HTTPClient = http.DefaultClient
	}
	httpClient := *c.HTTPClient
	httpClient.CheckRedirect = func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }
	c.HTTPClient = &httpClient
	if c.InitialBackoff <= 0 {
		c.InitialBackoff = 500 * time.Millisecond
	}
	log := c.Logger
	if log == nil {
		log = slog.Default()
	}

	parts, err := splitBatch(batch, maxItemsPerIngestCollection, maxItemsPerIngestPayload, maxBytesPerIngestPayload)
	if err != nil {
		return err
	}
	if c.SpoolDir != "" {
		return c.deliverQueued(ctx, parts, log)
	}
	for i, part := range parts {
		if err := c.postBatch(ctx, part, log); err != nil {
			return fmt.Errorf("post pulse batch part %d/%d: %w", i+1, len(parts), err)
		}
	}
	return nil
}

func (c Client) postBatch(ctx context.Context, batch Batch, log *slog.Logger) error {
	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("encode pulse batch: %w", err)
	}
	return c.postBody(ctx, body, log)
}

func (c Client) postBody(ctx context.Context, body []byte, log *slog.Logger) error {
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		err := c.postOnce(ctx, body, idempotencyKey(body))
		if err == nil {
			if attempt > 0 {
				log.Info("pulse batch sent after retry", "attempt", attempt+1)
			}
			return nil
		}
		lastErr = err
		if !isRetryable(err) || attempt == c.MaxRetries {
			break
		}
		backoff := c.InitialBackoff
		for n := 0; n < attempt && backoff < 30*time.Second; n++ {
			backoff *= 2
		}
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
		var retry retryableError
		if errors.As(err, &retry) && retry.after > backoff {
			backoff = retry.after
		}
		log.Warn("pulse batch send failed; retrying", "attempt", attempt+1, "backoff", backoff, "err", err)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return lastErr
}

func splitBatch(batch Batch, collectionLimit, itemLimit, byteLimit int) ([]Batch, error) {
	if collectionLimit <= 0 {
		collectionLimit = maxItemsPerIngestCollection
	}
	if itemLimit <= 0 {
		itemLimit = maxItemsPerIngestPayload
	}
	if byteLimit <= 0 {
		byteLimit = maxBytesPerIngestPayload
	}
	parts := []Batch{}
	current := emptyBatch()
	flush := func() {
		parts = append(parts, current)
		current = emptyBatch()
	}
	var add func(string, any) error
	add = func(kind string, value any) error {
		candidate := current
		switch kind {
		case "metric":
			candidate.Metrics = appendCopy(current.Metrics, value.(Metric))
		case "event":
			candidate.Events = appendCopy(current.Events, value.(Event))
		case "state":
			candidate.States = appendCopy(current.States, value.(State))
		}
		if batchItems(candidate) > itemLimit || len(candidate.Metrics) > collectionLimit || len(candidate.Events) > collectionLimit || len(candidate.States) > collectionLimit || encodedSize(candidate) > byteLimit {
			if batchItems(current) == 0 {
				return fmt.Errorf("single pulse %s exceeds ingest payload limits", kind)
			}
			flush()
			return addToEmpty(&current, kind, value, byteLimit)
		}
		current = candidate
		return nil
	}
	for _, metric := range batch.Metrics {
		if err := add("metric", metric); err != nil {
			return nil, err
		}
	}
	for _, event := range batch.Events {
		if err := add("event", event); err != nil {
			return nil, err
		}
	}
	for _, state := range batch.States {
		if err := add("state", state); err != nil {
			return nil, err
		}
	}
	if batchItems(current) > 0 || len(parts) == 0 {
		flush()
	}
	return parts, nil
}

func emptyBatch() Batch {
	return Batch{Metrics: []Metric{}, Events: []Event{}, States: []State{}}
}

func addToEmpty(batch *Batch, kind string, value any, byteLimit int) error {
	switch kind {
	case "metric":
		batch.Metrics = append(batch.Metrics, value.(Metric))
	case "event":
		batch.Events = append(batch.Events, value.(Event))
	case "state":
		batch.States = append(batch.States, value.(State))
	}
	if encodedSize(*batch) > byteLimit {
		return fmt.Errorf("single pulse %s exceeds ingest payload byte limit", kind)
	}
	return nil
}

func appendCopy[T any](values []T, value T) []T {
	out := make([]T, len(values), len(values)+1)
	copy(out, values)
	return append(out, value)
}

func batchItems(batch Batch) int {
	return len(batch.Metrics) + len(batch.Events) + len(batch.States)
}

func encodedSize(batch Batch) int {
	body, err := json.Marshal(batch)
	if err != nil {
		return maxBytesPerIngestPayload + 1
	}
	return len(body)
}

func idempotencyKey(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("pulse-ingestor-%x", sum)
}

func (c Client) postOnce(ctx context.Context, body []byte, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid pulse request configuration")
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return retryableError{err: errors.New("pulse request transport failed")}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var counts struct {
			Metrics *uint64 `json:"metrics"`
			Events  *uint64 `json:"events"`
			States  *uint64 `json:"states"`
		}
		decoder := json.NewDecoder(io.LimitReader(resp.Body, 4097))
		if err := decoder.Decode(&counts); err != nil || counts.Metrics == nil || counts.Events == nil || counts.States == nil {
			return retryableError{err: errors.New("pulse ingest returned an invalid acknowledgement; delivery is uncertain")}
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return retryableError{err: errors.New("pulse ingest returned an invalid acknowledgement; delivery is uncertain")}
		}
		return nil
	}

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	err = httpStatusError{code: resp.StatusCode}
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return retryableError{err: err, after: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	return err
}

type retryableError struct {
	after time.Duration
	err   error
}

func (e retryableError) Error() string {
	return e.err.Error()
}

func (e retryableError) Unwrap() error {
	return e.err
}

func isRetryable(err error) bool {
	_, ok := err.(retryableError)
	return ok
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

type httpStatusError struct{ code int }

func (e httpStatusError) Error() string { return fmt.Sprintf("pulse ingest returned HTTP %d", e.code) }
