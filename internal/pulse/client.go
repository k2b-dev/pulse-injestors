package pulse

import (
	"bytes"
	"context"
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

func (c Client) PostBatch(ctx context.Context, batch Batch) error {
	body, err := json.Marshal(batch)
	if err != nil {
		return fmt.Errorf("encode pulse batch: %w", err)
	}
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

	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		err := c.postOnce(ctx, body)
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

func (c Client) postOnce(ctx context.Context, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create pulse request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

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
