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

// Timeouter overrides the runner's per-collector timeout. A nonpositive value
// uses the runner default.
type Timeouter interface {
	CollectTimeout() time.Duration
}

func timeoutOf(c Collector) time.Duration {
	if timed, ok := c.(Timeouter); ok {
		return timed.CollectTimeout()
	}
	return 0
}

type ScopedCollector struct {
	Collector  Collector
	Resource   pulse.ResourceRef
	Dimensions map[string]string
}

func (s ScopedCollector) CollectTimeout() time.Duration { return timeoutOf(s.Collector) }

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

func (w WithCadence) CollectTimeout() time.Duration { return timeoutOf(w.Collector) }

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

	lastRun     map[string]time.Time
	lastTimeout map[string]time.Time
}

func (r *Runner) collectorTimeout(c Collector) time.Duration {
	if timeout := timeoutOf(c); timeout > 0 {
		return timeout
	}
	return r.Timeout
}

// MaxCycleTimeout bounds a cycle containing every collector. Each collector
// gets its full budget even after earlier collectors have used theirs.
// The watchdog must allow this same worst-case collection duration.
func (r *Runner) MaxCycleTimeout() time.Duration {
	var total time.Duration
	for _, c := range r.Collectors {
		total += max(0, r.collectorTimeout(c))
	}
	return max(r.Interval, total)
}

// Once collects every collector and hands the batch to Sender.
func (r *Runner) Once(ctx context.Context) error {
	if r.Sender == nil {
		return errors.New("monitoring sender is required")
	}
	return r.Sender.PostBatch(ctx, r.Collect(ctx, true))
}

// Collect runs the collectors that are due, or all of them, within one
// cycle budget. Collector failures become ingestor.collector.* signals.
func (r *Runner) Collect(ctx context.Context, all bool) pulse.Batch {
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
	// Only due collectors may extend this cycle. Once includes all collectors.
	var cycleBudget time.Duration
	for _, c := range r.Collectors {
		if all || r.due(c, scope.Timestamp) {
			cycleBudget += max(0, r.collectorTimeout(c))
		}
	}
	cycleBudget = max(r.Interval, cycleBudget)
	if cycleBudget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cycleBudget)
		defer cancel()
	}
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
		if timeout := r.collectorTimeout(collector); timeout > 0 {
			collectCtx, cancel = context.WithTimeout(ctx, timeout)
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
			// Retrying a long timed-out phase every minute can monopolize the
			// process. Keep its configured cadence; ordinary failures retry next tick.
			if errors.Is(err, context.DeadlineExceeded) && cadenceOf(collector) > r.Interval {
				if r.lastTimeout == nil {
					r.lastTimeout = map[string]time.Time{}
				}
				r.lastTimeout[collector.Name()] = scope.Timestamp
			}
			log.Warn("collector failed", "collector", collector.Name(), "err", err)
			builder.State("ingestor.collector.ok", false, map[string]string{"collector": collector.Name()})
			builder.EventDetails("ingestor.collector.failed", map[string]string{"collector": collector.Name()}, EventDetails{
				Attributes: map[string]any{"error": truncate(err.Error(), 2000)},
			})
			continue
		}
		delete(r.lastTimeout, collector.Name())
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
	if timedOut, exists := r.lastTimeout[c.Name()]; exists && (!ok || timedOut.After(last)) {
		last, ok = timedOut, true
	}
	return !ok || now.Sub(last) >= cadence-r.Interval/2
}

func truncate(value string, limit int) string {
	if runes := []rune(value); len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return value
}
