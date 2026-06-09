package monitoring

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector interface {
	Name() string
	Collect(context.Context, Scope) (pulse.Batch, error)
}

type Sender interface {
	PostBatch(context.Context, pulse.Batch) error
}

type Runner struct {
	EntityID   string
	EntityType string
	Dimensions map[string]string
	Collectors []Collector
	Sender     Sender
	Timeout    time.Duration
	Interval   time.Duration
	Logger     *slog.Logger
	Now        func() time.Time
}

func (r Runner) Once(ctx context.Context) error {
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
		EntityID:   r.EntityID,
		EntityType: r.EntityType,
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
		collectorScope := scope
		collectorScope.Dimensions = mergeDims(scope.Dimensions, map[string]string{"collector": collector.Name()})
		collectCtx := ctx
		cancel := func() {}
		if r.Timeout > 0 {
			collectCtx, cancel = context.WithTimeout(ctx, r.Timeout)
		}
		part, err := collector.Collect(collectCtx, collectorScope)
		cancel()
		part = Inject(part, scope, map[string]string{"collector": collector.Name()})
		Merge(&batch, part)
		if err != nil {
			log.Warn("collector failed", "collector", collector.Name(), "err", err)
			builder.State("ingestor.collector.ok", false, map[string]string{"collector": collector.Name()})
			builder.Event("ingestor.collector.failed", map[string]string{"collector": collector.Name()}, map[string]any{"error": err.Error()})
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
