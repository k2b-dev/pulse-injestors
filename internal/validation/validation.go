package validation

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var metricNamePattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9_]+)+$`)

const (
	dimensionKeyLimit       = 32
	dimensionKeyMaxLength   = 80
	dimensionValueMaxLength = 500
	resourceTypeMaxLength   = 80
	resourceIDMaxLength     = 500
	resourceLabelMaxLength  = 240
	eventAttributeKeyLimit  = 64
	eventSensitiveKeyLimit  = 32
	eventFieldKeyMaxLength  = 80
	eventAttributesMaxBytes = 32 * 1024
	eventSensitiveMaxBytes  = 32 * 1024
	eventPayloadMaxBytes    = 64 * 1024
	eventAttributesMaxDepth = 4
	eventSensitiveMaxDepth  = 4
	eventPayloadMaxDepth    = 8
)

type ErrorList []error

func (e ErrorList) Error() string {
	parts := make([]string, 0, len(e))
	for _, err := range e {
		parts = append(parts, err.Error())
	}
	return strings.Join(parts, "; ")
}

func Batch(batch pulse.Batch) error {
	var errs ErrorList
	seen := map[string]int{}
	for i, metric := range batch.Metrics {
		if metric.Name == "" || !metricNamePattern.MatchString(metric.Name) {
			errs = append(errs, fmt.Errorf("metric %d has invalid name %q", i, metric.Name))
		}
		if metric.Type != "gauge" && metric.Type != "counter" && metric.Type != "histogram" && metric.Type != "summary" {
			errs = append(errs, fmt.Errorf("metric %d has invalid type %q", i, metric.Type))
		}
		if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
			errs = append(errs, fmt.Errorf("metric %d has non-finite value", i))
		}
		if metric.Timestamp.IsZero() {
			errs = append(errs, fmt.Errorf("metric %d has zero timestamp", i))
		}
		validateSignalIdentity(&errs, "metric", i, metric.EntityID, metric.EntityType, metric.Resource)
		validateDimensions(&errs, "metric", i, metric.Dimensions)
		if metric.Type == "counter" && metric.Value < 0 {
			errs = append(errs, fmt.Errorf("metric %d counter is negative", i))
		}
		if metric.Unit == "percent" {
			if metric.Value < 0 {
				errs = append(errs, fmt.Errorf("metric %d percent is negative", i))
			}
			if shouldCapPercent(metric.Name) && metric.Value > 100 {
				errs = append(errs, fmt.Errorf("metric %d percent exceeds 100", i))
			}
		}
		key := metricKey(metric)
		seen[key]++
		if seen[key] > 1 {
			errs = append(errs, fmt.Errorf("metric %d duplicates same series and timestamp", i))
		}
	}
	for i, event := range batch.Events {
		if event.Kind == "" {
			errs = append(errs, fmt.Errorf("event %d missing kind", i))
		}
		if event.Timestamp.IsZero() {
			errs = append(errs, fmt.Errorf("event %d has zero timestamp", i))
		}
		validateSignalIdentity(&errs, "event", i, event.EntityID, event.EntityType, event.Resource)
		validateDimensions(&errs, "event", i, event.Dimensions)
		validateJSONField(&errs, "event", i, "attributes", event.Attributes, eventAttributeKeyLimit, eventAttributesMaxBytes, eventAttributesMaxDepth)
		validateJSONField(&errs, "event", i, "sensitive", event.Sensitive, eventSensitiveKeyLimit, eventSensitiveMaxBytes, eventSensitiveMaxDepth)
		validateJSONField(&errs, "event", i, "payload", event.Payload, 0, eventPayloadMaxBytes, eventPayloadMaxDepth)
	}
	for i, state := range batch.States {
		if state.Key == "" {
			errs = append(errs, fmt.Errorf("state %d missing key", i))
		}
		if !validStateValue(state.Value) {
			errs = append(errs, fmt.Errorf("state %d has invalid value type %T", i, state.Value))
		}
		if state.Timestamp.IsZero() {
			errs = append(errs, fmt.Errorf("state %d has zero timestamp", i))
		}
		validateSignalIdentity(&errs, "state", i, state.EntityID, state.EntityType, state.Resource)
		validateDimensions(&errs, "state", i, state.Dimensions)
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validStateValue(value any) bool {
	switch typed := value.(type) {
	case string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64, nil:
		switch number := any(typed).(type) {
		case float32:
			return !math.IsNaN(float64(number)) && !math.IsInf(float64(number), 0)
		case float64:
			return !math.IsNaN(number) && !math.IsInf(number, 0)
		default:
			return true
		}
	default:
		return false
	}
}

func validateSignalIdentity(errs *ErrorList, kind string, index int, entityID, entityType string, resource *pulse.ResourceRef) {
	if resource == nil {
		*errs = append(*errs, fmt.Errorf("%s %d missing resource", kind, index))
		return
	}
	if resource.Type == "" || resource.ID == "" || resource.Label == "" {
		*errs = append(*errs, fmt.Errorf("%s %d has incomplete resource", kind, index))
	}
	if len(resource.Type) > resourceTypeMaxLength || len(resource.ID) > resourceIDMaxLength || len(resource.Label) > resourceLabelMaxLength {
		*errs = append(*errs, fmt.Errorf("%s %d resource exceeds Pulse limits", kind, index))
	}
	if entityID != resource.Key() || entityType != resource.Type {
		*errs = append(*errs, fmt.Errorf("%s %d entity and resource identities differ", kind, index))
	}
}

func validateDimensions(errs *ErrorList, kind string, index int, dimensions map[string]string) {
	if len(dimensions) > dimensionKeyLimit {
		*errs = append(*errs, fmt.Errorf("%s %d dimensions exceed %d keys", kind, index, dimensionKeyLimit))
	}
	for key, value := range dimensions {
		if strings.TrimSpace(key) == "" || len(key) > dimensionKeyMaxLength {
			*errs = append(*errs, fmt.Errorf("%s %d has invalid dimension key %q", kind, index, key))
		}
		if len(value) > dimensionValueMaxLength {
			*errs = append(*errs, fmt.Errorf("%s %d dimension %q exceeds %d characters", kind, index, key, dimensionValueMaxLength))
		}
	}
}

func validateJSONField(errs *ErrorList, kind string, index int, field string, value map[string]any, keyLimit, byteLimit, depthLimit int) {
	if keyLimit > 0 && len(value) > keyLimit {
		*errs = append(*errs, fmt.Errorf("%s %d %s exceed %d keys", kind, index, field, keyLimit))
	}
	for key := range value {
		if strings.TrimSpace(key) == "" || len(key) > eventFieldKeyMaxLength {
			*errs = append(*errs, fmt.Errorf("%s %d has invalid %s key %q", kind, index, field, key))
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s %d %s are not valid JSON", kind, index, field))
		return
	}
	if len(encoded) > byteLimit {
		*errs = append(*errs, fmt.Errorf("%s %d %s exceed %d bytes", kind, index, field, byteLimit))
	}
	if jsonDepth(value, 0) > depthLimit {
		*errs = append(*errs, fmt.Errorf("%s %d %s exceed %d nested levels", kind, index, field, depthLimit))
	}
}

func jsonDepth(value any, depth int) int {
	maxDepth := depth
	switch typed := value.(type) {
	case map[string]any:
		for _, child := range typed {
			maxDepth = max(maxDepth, jsonDepth(child, depth+1))
		}
	case []any:
		for _, child := range typed {
			maxDepth = max(maxDepth, jsonDepth(child, depth+1))
		}
	}
	return maxDepth
}

func shouldCapPercent(name string) bool {
	switch name {
	case "docker.container.cpu.usage", "docker.compose.service.cpu.usage":
		return false
	}
	return strings.HasSuffix(name, ".usage") || strings.HasSuffix(name, ".cpu.usage")
}

func metricKey(metric pulse.Metric) string {
	dims := make([]string, 0, len(metric.Dimensions))
	for k, v := range metric.Dimensions {
		dims = append(dims, k+"="+v)
	}
	sort.Strings(dims)
	return strings.Join([]string{
		metric.Name,
		metric.Type,
		metric.Unit,
		metric.Timestamp.Format("2006-01-02T15:04:05.000000000Z07:00"),
		metric.EntityID,
		metric.EntityType,
		strings.Join(dims, ","),
	}, "|")
}
