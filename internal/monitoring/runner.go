package monitoring

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

type Collector interface {
	Name() string
	Collect(context.Context, Scope) (pulse.Batch, error)
}

// Cadenced collectors run less often than the runner interval.
type Cadenced interface {
	Cadence() time.Duration
}

type ScopedCollector struct {
	Collector  Collector
	Resource   pulse.ResourceRef
	Dimensions map[string]string
}

func (s ScopedCollector) Name() string {
	return s.Collector.Name()
}

func (s ScopedCollector) Cadence() time.Duration {
	return cadenceOf(s.Collector)
}

func (s ScopedCollector) Collect(ctx context.Context, scope Scope) (pulse.Batch, error) {
	if s.Resource.Type != "" || s.Resource.ID != "" {
		scope.Resource = s.Resource
	}
	scope.Dimensions = mergeDims(scope.Dimensions, s.Dimensions)
	return s.Collector.Collect(ctx, scope)
}

// WithCadence overrides how often a collector runs.
type WithCadence struct {
	Collector
	Every time.Duration
}

func (w WithCadence) Cadence() time.Duration { return w.Every }

func cadenceOf(c Collector) time.Duration {
	if cadenced, ok := c.(Cadenced); ok {
		return cadenced.Cadence()
	}
	return 0
}

type Sender interface {
	PostBatch(context.Context, pulse.Batch) error
}

type Runner struct {
	Resource   pulse.ResourceRef
	Dimensions map[string]string
	Collectors []Collector
	Sender     Sender
	Timeout    time.Duration
	Interval   time.Duration
	Logger     *slog.Logger
	Now        func() time.Time

	lastRun map[string]time.Time
}

// Once collects every collector and hands the batch to Sender.
func (r *Runner) Once(ctx context.Context) error {
	if r.Sender == nil {
		return errors.New("monitoring sender is required")
	}
	return r.Sender.PostBatch(ctx, r.Collect(ctx, true))
}

// Collect runs the collectors that are due, or all of them, within one
// interval. Collector failures become ingestor.collector.* signals.
func (r *Runner) Collect(ctx context.Context, all bool) pulse.Batch {
	if r.Interval > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Interval)
		defer cancel()
	}
	log := r.Logger
	if log == nil {
		log = slog.Default()
	}
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	if r.lastRun == nil {
		r.lastRun = map[string]time.Time{}
	}
	scope := r.Scope(now())
	batch := pulse.Batch{Metrics: []pulse.Metric{}, Events: []pulse.Event{}, States: []pulse.State{}}
	builder := NewBuilder(scope)

	// Health collectors must run before work that can consume the cycle budget.
	collectors := append([]Collector(nil), r.Collectors...)
	sort.SliceStable(collectors, func(i, j int) bool {
		return cadenceOf(collectors[i]) <= r.Interval && cadenceOf(collectors[j]) > r.Interval
	})
	for _, collector := range collectors {
		if ctx.Err() != nil {
			break
		}
		if !all && !r.due(collector, scope.Timestamp) {
			continue
		}
		collectCtx := ctx
		cancel := func() {}
		if r.Timeout > 0 {
			collectCtx, cancel = context.WithTimeout(ctx, r.Timeout)
		}
		part, err := collector.Collect(collectCtx, scope)
		err = errors.Join(err, collectCtx.Err())
		cancel()
		part = Inject(part, scope, nil)
		candidate := batch
		Merge(&candidate, part)
		if validationErr := validation.Batch(candidate); validationErr != nil {
			err = errors.Join(err, validationErr)
		} else {
			batch = candidate
		}
		if err != nil {
			log.Warn("collector failed", "collector", collector.Name(), "err", err)
			builder.State("ingestor.collector.ok", false, map[string]string{"collector": collector.Name()})
			builder.EventDetails("ingestor.collector.failed", map[string]string{"collector": collector.Name()}, EventDetails{
				Attributes: map[string]any{"error": truncate(err.Error(), 2000)},
			})
			continue
		}
		r.lastRun[collector.Name()] = scope.Timestamp
		builder.State("ingestor.collector.ok", true, map[string]string{"collector": collector.Name()})
	}
	Merge(&batch, builder.Batch())
	return batch
}

// Scope is the shared resource, dimensions and timestamp of one cycle.
func (r *Runner) Scope(ts time.Time) Scope {
	return Scope{Resource: r.Resource, Dimensions: r.Dimensions, Timestamp: ts.UTC()}
}

// due allows half an interval of jitter so a cadence is not skipped a tick late.
func (r *Runner) due(c Collector, now time.Time) bool {
	cadence := cadenceOf(c)
	if cadence <= r.Interval {
		return true
	}
	last, ok := r.lastRun[c.Name()]
	return !ok || now.Sub(last) >= cadence-r.Interval/2
}

func truncate(value string, limit int) string {
	if runes := []rune(value); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}
