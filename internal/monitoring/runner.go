package monitoring

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

type Collector interface {
	Name() string
	Collect(context.Context, Scope) (pulse.Batch, error)
}

type ScopedCollector struct {
	Collector  Collector
	Resource   pulse.ResourceRef
	Dimensions map[string]string
}

func (s ScopedCollector) Name() string {
	return s.Collector.Name()
}

func (s ScopedCollector) Collect(ctx context.Context, scope Scope) (pulse.Batch, error) {
	if s.Resource.Type != "" || s.Resource.ID != "" {
		scope.Resource = s.Resource
	}
	scope.Dimensions = mergeDims(scope.Dimensions, s.Dimensions)
	return s.Collector.Collect(ctx, scope)
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
}

func (r Runner) Once(ctx context.Context) error {
	if r.Interval > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Interval)
		defer cancel()
	}
	if r.Sender == nil {
		return errors.New("monitoring sender is required")
	}
	log := r.Logger
	if log == nil {
		log = slog.Default()
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	scope := Scope{
		Resource:   r.Resource,
		Dimensions: r.Dimensions,
		Timestamp:  now().UTC(),
	}
	batch := pulse.Batch{
		Metrics: []pulse.Metric{},
		Events:  []pulse.Event{},
		States:  []pulse.State{},
	}
	builder := NewBuilder(scope)

	for _, collector := range r.Collectors {
		if ctx.Err() != nil {
			break
		}
		collectorScope := scope
		collectCtx := ctx
		cancel := func() {}
		if r.Timeout > 0 {
			collectCtx, cancel = context.WithTimeout(ctx, r.Timeout)
		}
		part, err := collector.Collect(collectCtx, collectorScope)
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
				Attributes: map[string]any{"error": err.Error()},
			})
			continue
		}
		builder.State("ingestor.collector.ok", true, map[string]string{"collector": collector.Name()})
	}
	Merge(&batch, builder.Batch())

	if len(batch.Metrics) == 0 && len(batch.Events) == 0 && len(batch.States) == 0 {
		empty := NewBuilder(scope)
		empty.State("ingestor.collect.ok", false, nil)
		empty.Event("ingestor.collect.empty", nil, nil)
		Merge(&batch, empty.Batch())
	}
	if err := validation.Batch(batch); err != nil {
		return err
	}
	log.Info("sending pulse batch", "metrics", len(batch.Metrics), "events", len(batch.Events), "states", len(batch.States))
	return r.Sender.PostBatch(ctx, batch)
}

func (r Runner) Run(ctx context.Context) error {
	interval := r.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	r.logRunError(r.Once(ctx))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			r.logRunError(r.Once(ctx))
		}
	}
}

func (r Runner) logRunError(err error) {
	if err == nil {
		return
	}
	if r.Logger != nil {
		r.Logger.Error("pulse push failed", "err", err)
	}
}
