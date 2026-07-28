package monitoring

import (
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Scope struct {
	EntityID   string
	EntityType string
	Label      string
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
		EntityID:   resourceKey(resource, b.scope.EntityID),
		EntityType: resourceType(resource, b.scope.EntityType),
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
		EntityID:      resourceKey(resource, b.scope.EntityID),
		EntityType:    resourceType(resource, b.scope.EntityType),
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
		EntityID:   resourceKey(resource, b.scope.EntityID),
		EntityType: resourceType(resource, b.scope.EntityType),
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
		applyResource(&batch.Metrics[i].EntityID, &batch.Metrics[i].EntityType, &batch.Metrics[i].Resource, scope, batch.Metrics[i].Dimensions)
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
		applyResource(&batch.Events[i].EntityID, &batch.Events[i].EntityType, &batch.Events[i].Resource, scope, batch.Events[i].Dimensions)
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
		applyResource(&batch.States[i].EntityID, &batch.States[i].EntityType, &batch.States[i].Resource, scope, batch.States[i].Dimensions)
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
	if scope.Label != "" {
		return scope.Label
	}
	if v := labelFromDimensions(scope.Dimensions); v != "" {
		return v
	}
	return labelFromEntityID(scope.EntityID)
}

func injectedLabel(scope Scope, entityID string, dims map[string]string) string {
	if entityID != "" && entityID != scope.EntityID {
		if v := labelFromDimensions(dims); v != "" {
			return v
		}
		return labelFromEntityID(entityID)
	}
	scoped := scope
	scoped.Dimensions = dims
	return signalLabel(scoped)
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

func labelFromEntityID(entityID string) string {
	entityID = strings.TrimSpace(entityID)
	if entityID == "" {
		return ""
	}
	parts := strings.Split(entityID, ":")
	for i := len(parts) - 1; i >= 0; i-- {
		if part := strings.TrimSpace(parts[i]); part != "" {
			return strings.NewReplacer("_", " ").Replace(part)
		}
	}
	return entityID
}

func resourceFromScope(scope Scope) *pulse.ResourceRef {
	return pulse.ResourceFromEntity(scope.EntityType, scope.EntityID, signalLabel(scope))
}

func applyResource(entityID, entityType *string, resource **pulse.ResourceRef, scope Scope, dims map[string]string) {
	if *resource == nil {
		label := injectedLabel(scope, *entityID, dims)
		*resource = pulse.ResourceFromEntity(*entityType, *entityID, label)
	}
	if *resource == nil {
		*resource = resourceFromScope(scope)
	}
	if *resource != nil {
		*entityID = (*resource).Key()
		*entityType = (*resource).Type
	}
}

func resourceKey(resource *pulse.ResourceRef, fallback string) string {
	if resource == nil {
		return fallback
	}
	return resource.Key()
}

func resourceType(resource *pulse.ResourceRef, fallback string) string {
	if resource == nil {
		return fallback
	}
	return resource.Type
}
