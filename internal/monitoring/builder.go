package monitoring

import (
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Scope struct {
	EntityID   string
	EntityType string
	Dimensions map[string]string
	Timestamp  time.Time
}

type Builder struct {
	scope Scope
	batch pulse.Batch
}

func NewBuilder(scope Scope) *Builder {
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now().UTC()
	}
	return &Builder{
		scope: scope,
		batch: pulse.Batch{
			Metrics: []pulse.Metric{},
			Events:  []pulse.Event{},
			States:  []pulse.State{},
		},
	}
}

func (b *Builder) Metric(name, typ string, value float64, unit string, dims map[string]string) {
	b.batch.Metrics = append(b.batch.Metrics, pulse.Metric{
		Name:       name,
		Type:       typ,
		Value:      value,
		Unit:       unit,
		Timestamp:  b.scope.Timestamp,
		EntityID:   b.scope.EntityID,
		EntityType: b.scope.EntityType,
		Dimensions: mergeDims(b.scope.Dimensions, dims),
	})
}

func (b *Builder) Event(kind string, dims map[string]string, payload map[string]any) {
	b.batch.Events = append(b.batch.Events, pulse.Event{
		Kind:       kind,
		Timestamp:  b.scope.Timestamp,
		EntityID:   b.scope.EntityID,
		EntityType: b.scope.EntityType,
		Dimensions: mergeDims(b.scope.Dimensions, dims),
		Payload:    payload,
	})
}

func (b *Builder) State(key string, value any, dims map[string]string) {
	b.batch.States = append(b.batch.States, pulse.State{
		Key:        key,
		Value:      value,
		Timestamp:  b.scope.Timestamp,
		EntityID:   b.scope.EntityID,
		EntityType: b.scope.EntityType,
		Dimensions: mergeDims(b.scope.Dimensions, dims),
	})
}

func (b *Builder) Batch() pulse.Batch {
	return b.batch
}

func Inject(batch pulse.Batch, scope Scope, extraDims map[string]string) pulse.Batch {
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now().UTC()
	}
	for i := range batch.Metrics {
		if batch.Metrics[i].Timestamp.IsZero() {
			batch.Metrics[i].Timestamp = scope.Timestamp
		}
		if batch.Metrics[i].EntityID == "" {
			batch.Metrics[i].EntityID = scope.EntityID
		}
		if batch.Metrics[i].EntityType == "" {
			batch.Metrics[i].EntityType = scope.EntityType
		}
		batch.Metrics[i].Dimensions = mergeDims(scope.Dimensions, mergeDims(extraDims, batch.Metrics[i].Dimensions))
	}
	for i := range batch.Events {
		if batch.Events[i].Timestamp.IsZero() {
			batch.Events[i].Timestamp = scope.Timestamp
		}
		if batch.Events[i].EntityID == "" {
			batch.Events[i].EntityID = scope.EntityID
		}
		if batch.Events[i].EntityType == "" {
			batch.Events[i].EntityType = scope.EntityType
		}
		batch.Events[i].Dimensions = mergeDims(scope.Dimensions, mergeDims(extraDims, batch.Events[i].Dimensions))
	}
	for i := range batch.States {
		if batch.States[i].Timestamp.IsZero() {
			batch.States[i].Timestamp = scope.Timestamp
		}
		if batch.States[i].EntityID == "" {
			batch.States[i].EntityID = scope.EntityID
		}
		if batch.States[i].EntityType == "" {
			batch.States[i].EntityType = scope.EntityType
		}
		batch.States[i].Dimensions = mergeDims(scope.Dimensions, mergeDims(extraDims, batch.States[i].Dimensions))
	}
	return batch
}

func Merge(dst *pulse.Batch, src pulse.Batch) {
	dst.Metrics = append(dst.Metrics, src.Metrics...)
	dst.Events = append(dst.Events, src.Events...)
	dst.States = append(dst.States, src.States...)
}

func mergeDims(base, extra map[string]string) map[string]string {
	if len(base) == 0 && len(extra) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		if v != "" {
			out[k] = v
		}
	}
	for k, v := range extra {
		if v != "" {
			out[k] = v
		}
	}
	return out
}
