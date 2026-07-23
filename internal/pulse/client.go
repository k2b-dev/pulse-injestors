package pulse

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

type Client struct {
	URL            string
	Token          string
	HTTPClient     *http.Client
	MaxRetries     int
	InitialBackoff time.Duration
	Logger         *slog.Logger
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
		backoff := c.InitialBackoff << attempt
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
		return fmt.Errorf("create pulse request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return retryableError{err: fmt.Errorf("send pulse request: %w", err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	err = fmt.Errorf("pulse ingest returned HTTP %d: %s", resp.StatusCode, string(bytes.TrimSpace(msg)))
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return retryableError{err: err}
	}
	return err
}

type retryableError struct {
	err error
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
