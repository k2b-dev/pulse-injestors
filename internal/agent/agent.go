// Package agent runs an ingestor: collect on a schedule, queue durably, and
// deliver to Pulse independently of collection.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

// MemorySpoolBytes bounds the in-memory queue when no spool_dir is configured.
const MemorySpoolBytes = 16 << 20

type Options struct {
	// Name is the binary name, for example "pulse-linux".
	Name    string
	Version string
	// Command is "run" (service) or "once".
	Command string
	Local   bool
	Pretty  bool
	Config  config.Config
	Runner  *monitoring.Runner
	Logger  *slog.Logger
}

// Run executes the selected command until it finishes or ctx ends.
func Run(ctx context.Context, opt Options) error {
	if opt.Logger == nil {
		opt.Logger = slog.Default()
	}
	if opt.Local {
		opt.Runner.Sender = monitoring.StdoutSender{Writer: os.Stdout, Pretty: true, Report: opt.Pretty}
		if opt.Command != "run" {
			return opt.Runner.Once(ctx)
		}
		ticker := time.NewTicker(opt.Config.RunnerInterval())
		defer ticker.Stop()
		for ctx.Err() == nil {
			if err := opt.Runner.Sender.PostBatch(ctx, opt.Runner.Collect(ctx, false)); err != nil {
				return err
			}
			select {
			case <-ctx.Done():
				return nil
			case <-ticker.C:
			}
		}
		return nil
	}
	a, err := newAgent(opt)
	if err != nil {
		return err
	}
	defer a.close()
	if opt.Command == "run" {
		return a.serve(ctx)
	}
	return a.once(ctx)
}

type agent struct {
	opt    Options
	log    *slog.Logger
	client pulse.Client
	spool  *pulse.Spool
	states *monitoring.StateCache
	stats  *pulse.Stats
	// lastCycle is the duration of the previous collection.
	lastCycle time.Duration
}

func newAgent(opt Options) (*agent, error) {
	cfg := opt.Config
	stats := &pulse.Stats{}
	maxBytes := cfg.Pulse.SpoolMaxBytes
	if cfg.Pulse.SpoolDir == "" && maxBytes <= 0 {
		maxBytes = MemorySpoolBytes
	}
	a := &agent{
		opt:   opt,
		log:   opt.Logger,
		stats: stats,
		client: pulse.Client{
			URL:            cfg.Pulse.IngestURL,
			Token:          cfg.Pulse.IngestToken,
			HTTPClient:     &http.Client{Timeout: cfg.HTTPTimeout()},
			MaxRetries:     cfg.HTTP.MaxRetries,
			InitialBackoff: cfg.InitialBackoff(),
			Logger:         opt.Logger,
			Stats:          stats,
		},
		spool: &pulse.Spool{
			Dir:      cfg.Pulse.SpoolDir,
			MaxBytes: maxBytes,
			Binding:  pulse.SpoolBinding(cfg.Pulse.IngestURL, cfg.Pulse.IngestToken),
			Stats:    stats,
			Logger:   opt.Logger,
		},
		states: &monitoring.StateCache{},
	}
	if cfg.Pulse.SpoolDir != "" {
		a.states.Path = filepath.Join(cfg.Pulse.SpoolDir, ".states")
	}
	if err := a.spool.Open(time.Now()); err != nil {
		return nil, fmt.Errorf("open spool: %w", err)
	}
	if err := a.states.Load(); err != nil {
		a.log.Warn("state cache", "err", err)
	}
	return a, nil
}

func (a *agent) close() {
	if err := a.states.Save(time.Now()); err != nil {
		a.log.Warn("save state cache", "err", err)
	}
	a.spool.Close()
}

// once collects everything, queues it and delivers with bounded retries.
func (a *agent) once(ctx context.Context) error {
	if err := a.collect(ctx, true); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, a.opt.Config.RunnerInterval())
	defer cancel()
	result := a.drainWithRetries(ctx)
	// Rejections found while draining are reported now; a short run has no next cycle.
	if notices := a.noticeEvents(time.Now()); len(notices.Events) > 0 && result.Outcome == pulse.Delivered {
		if parts, err := pulse.SplitBatch(notices); err == nil && a.spool.Enqueue(parts, time.Now().UTC()) == nil {
			result = a.spool.Drain(ctx, a.client)
		}
	}
	if result.Outcome != pulse.Delivered {
		return a.undelivered(result)
	}
	return nil
}

func (a *agent) drainWithRetries(ctx context.Context) pulse.Result {
	for attempt := 0; ; attempt++ {
		result := a.spool.Drain(ctx, a.client)
		if result.Outcome != pulse.Retry || attempt >= a.opt.Config.HTTP.MaxRetries || ctx.Err() != nil {
			return result
		}
		wait := max(pulse.Backoff(a.client.InitialBackoff, attempt), result.RetryAfter)
		a.log.Warn("pulse delivery failed; retrying", "wait", wait, "err", result.Error())
		select {
		case <-ctx.Done():
			return result
		case <-time.After(wait):
		}
	}
}

func (a *agent) undelivered(result pulse.Result) error {
	state := a.spool.State()
	if a.opt.Config.Pulse.SpoolDir != "" {
		return fmt.Errorf("%s; %d parts stay queued for the next run", result.Error(), state.Parts)
	}
	return fmt.Errorf("%s; %d parts were not delivered and are lost (no spool_dir)", result.Error(), state.Parts)
}

// serve runs collection on the interval and delivery in its own loop, so a
// slow or blocked Pulse never delays collection.
func (a *agent) serve(ctx context.Context) error {
	wake := make(chan struct{}, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.deliverLoop(ctx, wake)
	}()
	notify("READY=1\nSTATUS=collecting")
	defer notify("STOPPING=1")

	interval := a.opt.Config.RunnerInterval()
	var lastCompleted atomic.Int64
	// Allow the first cycle the same grace period as subsequent cycles.
	lastCompleted.Store(time.Now().UnixNano())
	watchdogDone := make(chan struct{})
	go func() {
		defer close(watchdogDone)
		watchdogLoop(ctx, os.Getenv("WATCHDOG_USEC"), interval, a.opt.Runner.MaxCycleTimeout(), &lastCompleted)
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	all := true
	for {
		if err := a.collect(ctx, all); err != nil {
			a.log.Error("collection not queued", "err", err)
		}
		all = false
		lastCompleted.Store(time.Now().UnixNano())
		select {
		case wake <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			<-done
			<-watchdogDone
			return nil
		case <-ticker.C:
		}
	}
}

func (a *agent) deliverLoop(ctx context.Context, wake <-chan struct{}) {
	attempt := 0
	var blocked *pulse.Result
	for {
		result := a.spool.Drain(ctx, a.client)
		if ctx.Err() != nil {
			return
		}
		var wait time.Duration
		switch result.Outcome {
		case pulse.Delivered:
			if blocked != nil {
				a.log.Info("pulse delivery recovered", "after", blocked.Error())
			}
			attempt, blocked = 0, nil
			select {
			case <-ctx.Done():
				return
			case <-wake:
			}
			continue
		case pulse.Paused:
			// An operator must act; probe slowly.
			wait = max(result.RetryAfter, pulse.Backoff(time.Minute, attempt))
		default:
			wait = max(result.RetryAfter, pulse.Backoff(a.client.InitialBackoff, attempt))
		}
		attempt++
		state := a.spool.State()
		key := fmt.Sprintf("%s/%d/%s", result.Outcome, result.Status, result.Code)
		if a.stats.ShouldLog(key, time.Now(), 10*time.Minute) {
			level := slog.LevelWarn
			if result.Outcome == pulse.Paused {
				level = slog.LevelError
			}
			a.log.Log(ctx, level, "pulse delivery blocked; telemetry stays queued", "outcome", result.Outcome.String(),
				"err", result.Error(), "queued_parts", state.Parts, "queued_bytes", state.Bytes, "next_attempt_in", wait)
		}
		blocked = &result
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// collect builds one batch with self-monitoring, filters unchanged states and
// queues it. States are marked sent only after the batch is queued.
func (a *agent) collect(ctx context.Context, all bool) error {
	started := time.Now()
	batch := a.opt.Runner.Collect(ctx, all)
	batch.Metrics = a.withoutBlockedMetrics(batch.Metrics, started)
	monitoring.Merge(&batch, a.selfSignals(started))
	var commit func()
	batch.States, commit = a.states.Filter(batch.States, started)
	parts, err := pulse.SplitBatch(batch)
	if err != nil {
		return err
	}
	if err := a.spool.Enqueue(parts, time.Now().UTC()); err != nil {
		return err
	}
	commit()
	if err := a.states.Save(started); err != nil {
		a.log.Warn("save state cache", "err", err)
	}
	a.lastCycle = time.Since(started)
	if a.lastCycle > a.opt.Config.RunnerInterval()*8/10 {
		a.log.Warn("collection took most of the interval", "duration", a.lastCycle.Round(time.Millisecond))
	}
	return nil
}

func (a *agent) selfSignals(now time.Time) pulse.Batch {
	scope := a.opt.Runner.Scope(now)
	b := monitoring.NewBuilder(scope)
	dims := map[string]string{"ingestor": a.opt.Name}
	b.Metric("ingestor.heartbeat", "gauge", 1, "", dims)
	if a.lastCycle > 0 {
		b.Metric("ingestor.cycle.duration", "gauge", a.lastCycle.Seconds(), "seconds", dims)
	}
	spool := a.spool.State()
	b.Metric("ingestor.spool.bytes", "gauge", float64(spool.Bytes), "bytes", dims)
	b.Metric("ingestor.spool.parts", "gauge", float64(spool.Parts), "", dims)
	age := 0.0
	if !spool.Oldest.IsZero() {
		age = max(0, now.Sub(spool.Oldest).Seconds())
	}
	b.Metric("ingestor.spool.oldest_age", "gauge", age, "seconds", dims)
	stats := a.stats.Snapshot()
	b.Metric("ingestor.delivery.failures", "counter", float64(stats.Failures), "", dims)
	if stats.SkewKnown {
		b.Metric("ingestor.clock.skew", "gauge", stats.Skew.Seconds(), "seconds", dims)
		if abs(stats.Skew) > 2*time.Minute && a.stats.ShouldLog("skew", now, time.Hour) {
			a.log.Warn("local clock differs from Pulse; Pulse rejects data more than 5 minutes in the future", "skew", stats.Skew)
		}
	}
	b.State("ingestor.version", a.opt.Version, dims)
	b.Merge(a.noticeEvents(now))
	return b.Batch()
}

// withoutBlockedMetrics drops metrics Pulse refused recently (for example a
// unit conflict), so one bad metric costs no requests and blocks nothing else.
func (a *agent) withoutBlockedMetrics(metrics []pulse.Metric, now time.Time) []pulse.Metric {
	kept := metrics[:0]
	for _, m := range metrics {
		if a.stats.MetricBlocked(m.Name, now) {
			if a.stats.ShouldLog("blocked:"+m.Name, now, time.Hour) {
				a.log.Warn("metric is not sent because Pulse refused it; retrying after the block", "metric", m.Name, "block", pulse.MetricBlockDuration)
			}
			continue
		}
		kept = append(kept, m)
	}
	return kept
}

// noticeEvents turns pending delivery notices into events.
func (a *agent) noticeEvents(now time.Time) pulse.Batch {
	b := monitoring.NewBuilder(a.opt.Runner.Scope(now))
	dims := map[string]string{"ingestor": a.opt.Name}
	for _, n := range a.stats.TakeNotices() {
		if n.Kind == "dropped" || n.Kind == "rejected" {
			a.states.Reset()
		}
		kind := "ingestor.telemetry.dropped"
		if n.Kind == "rejected" {
			kind = "ingestor.delivery.rejected"
		}
		attributes := map[string]any{"reason": n.Reason, "parts": n.Parts, "bytes": n.Bytes}
		if n.Kind == "rejected" {
			attributes["status"] = n.Status
			attributes["message"] = n.Message
			if n.Item != "" {
				attributes["item"] = n.Item
			}
			attributes["metrics"], attributes["events"], attributes["states"] = n.Metrics, n.Events, n.States
		}
		b.EventDetails(kind, monitoring.MergeDimensions(dims, map[string]string{"reason": n.Reason}), monitoring.EventDetails{Attributes: attributes})
	}
	return b.Batch()
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
