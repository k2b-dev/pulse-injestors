package monitoring

import (
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Scope struct {
	Resource   pulse.ResourceRef
	Dimensions map[string]string
	Timestamp  time.Time
}

type EventDetails struct {
	Value         *float64
	Attributes    map[string]any
	Sensitive     map[string]any
	ActorID       string
	SessionID     string
	CorrelationID string
	Payload       map[string]any
}

type Builder struct {
	scope Scope
	batch pulse.Batch
}

func NewBuilder(scope Scope) *Builder {
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now().UTC()
	}
	scope.Timestamp = scope.Timestamp.UTC()
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
	resource := resourceFromScope(b.scope)
	b.batch.Metrics = append(b.batch.Metrics, pulse.Metric{
		Name:       name,
		Type:       typ,
		Value:      value,
		Unit:       unit,
		Timestamp:  b.scope.Timestamp,
		Resource:   resource,
		Dimensions: mergeDims(b.scope.Dimensions, dims),
	})
}

func (b *Builder) Event(kind string, dims map[string]string, payload map[string]any) {
	b.EventDetails(kind, dims, EventDetails{Payload: payload})
}

func (b *Builder) EventDetails(kind string, dims map[string]string, details EventDetails) {
	resource := resourceFromScope(b.scope)
	b.batch.Events = append(b.batch.Events, pulse.Event{
		Kind:          kind,
		Timestamp:     b.scope.Timestamp,
		Value:         details.Value,
		Resource:      resource,
		Dimensions:    mergeDims(b.scope.Dimensions, dims),
		Attributes:    details.Attributes,
		Sensitive:     details.Sensitive,
		ActorID:       details.ActorID,
		SessionID:     details.SessionID,
		CorrelationID: details.CorrelationID,
		Payload:       details.Payload,
	})
}

func (b *Builder) State(key string, value any, dims map[string]string) {
	resource := resourceFromScope(b.scope)
	b.batch.States = append(b.batch.States, pulse.State{
		Key:        key,
		Value:      value,
		Timestamp:  b.scope.Timestamp,
		Resource:   resource,
		Dimensions: mergeDims(b.scope.Dimensions, dims),
	})
}

func (b *Builder) Batch() pulse.Batch {
	return b.batch
}

func (b *Builder) Merge(src pulse.Batch) {
	Merge(&b.batch, src)
}

func Inject(batch pulse.Batch, scope Scope, extraDims map[string]string) pulse.Batch {
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now().UTC()
	}
	for i := range batch.Metrics {
		if batch.Metrics[i].Type == "" {
			batch.Metrics[i].Type = "gauge"
		}
		if batch.Metrics[i].Timestamp.IsZero() {
			batch.Metrics[i].Timestamp = scope.Timestamp
		}
		batch.Metrics[i].Timestamp = batch.Metrics[i].Timestamp.UTC()
		batch.Metrics[i].Dimensions = mergeDims(scope.Dimensions, mergeDims(extraDims, batch.Metrics[i].Dimensions))
		applyResource(&batch.Metrics[i].Resource, scope)
	}
	for i := range batch.Events {
		if batch.Events[i].Timestamp.IsZero() {
			batch.Events[i].Timestamp = scope.Timestamp
		}
		batch.Events[i].Timestamp = batch.Events[i].Timestamp.UTC()
		batch.Events[i].Dimensions = mergeDims(scope.Dimensions, mergeDims(extraDims, batch.Events[i].Dimensions))
		applyResource(&batch.Events[i].Resource, scope)
	}
	for i := range batch.States {
		if batch.States[i].Timestamp.IsZero() {
			batch.States[i].Timestamp = scope.Timestamp
		}
		batch.States[i].Timestamp = batch.States[i].Timestamp.UTC()
		batch.States[i].Dimensions = mergeDims(scope.Dimensions, mergeDims(extraDims, batch.States[i].Dimensions))
		applyResource(&batch.States[i].Resource, scope)
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

func MergeDimensions(base, extra map[string]string) map[string]string {
	return mergeDims(base, extra)
}

func signalLabel(scope Scope) string {
	if scope.Resource.Label != "" {
		return scope.Resource.Label
	}
	if v := labelFromDimensions(scope.Dimensions); v != "" {
		return v
	}
	return labelFromResourceID(scope.Resource.ID)
}

func labelFromDimensions(dims map[string]string) string {
	for _, key := range []string{
		"container",
		"service",
		"guest",
		"display",
		"gpu",
		"mount",
		"datastore",
		"job",
		"pool",
		"dataset",
		"node",
		"device",
		"interface",
		"manager",
		"host",
	} {
		if v := dims[key]; v != "" {
			return v
		}
	}
	return ""
}

func labelFromResourceID(resourceID string) string {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return ""
	}
	parts := strings.Split(resourceID, ":")
	for i := len(parts) - 1; i >= 0; i-- {
		if part := strings.TrimSpace(parts[i]); part != "" {
			return strings.NewReplacer("_", " ").Replace(part)
		}
	}
	return resourceID
}

func resourceFromScope(scope Scope) *pulse.ResourceRef {
	if scope.Resource.Type == "" && scope.Resource.ID == "" {
		return nil
	}
	resource := scope.Resource
	if resource.Label == "" {
		resource.Label = signalLabel(scope)
	}
	return &resource
}

func applyResource(resource **pulse.ResourceRef, scope Scope) {
	if *resource == nil {
		*resource = resourceFromScope(scope)
	}
}
