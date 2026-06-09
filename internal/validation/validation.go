package validation

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

var metricNamePattern = regexp.MustCompile(`^[a-z0-9]+(\.[a-z0-9_]+)+$`)

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
		if metric.Type != "gauge" && metric.Type != "counter" {
			errs = append(errs, fmt.Errorf("metric %d has invalid type %q", i, metric.Type))
		}
		if math.IsNaN(metric.Value) || math.IsInf(metric.Value, 0) {
			errs = append(errs, fmt.Errorf("metric %d has non-finite value", i))
		}
		if metric.Timestamp.IsZero() {
			errs = append(errs, fmt.Errorf("metric %d has zero timestamp", i))
		}
		if metric.EntityID == "" || metric.EntityType == "" {
			errs = append(errs, fmt.Errorf("metric %d missing entity", i))
		}
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
	}
	for i, state := range batch.States {
		if state.Key == "" {
			errs = append(errs, fmt.Errorf("state %d missing key", i))
		}
		if state.Timestamp.IsZero() {
			errs = append(errs, fmt.Errorf("state %d has zero timestamp", i))
		}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func shouldCapPercent(name string) bool {
	if name == "docker.container.cpu.usage" {
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
