package monitoring

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

func WriteReport(w io.Writer, batch pulse.Batch) error {
	fmt.Fprintf(w, "Pulse local report\n")
	fmt.Fprintf(w, "Metrics: %d  States: %d  Events: %d\n\n", len(batch.Metrics), len(batch.States), len(batch.Events))
	writeMetrics(w, batch.Metrics)
	writeStates(w, batch.States)
	writeEvents(w, batch.Events)
	return nil
}

func writeMetrics(w io.Writer, metrics []pulse.Metric) {
	if len(metrics) == 0 {
		return
	}
	sections := map[string]map[string][]pulse.Metric{}
	for _, metric := range metrics {
		section := sectionName(metric.Name)
		group := groupName(metric.Name, metric.Dimensions)
		if sections[section] == nil {
			sections[section] = map[string][]pulse.Metric{}
		}
		sections[section][group] = append(sections[section][group], metric)
	}
	for _, section := range sortedKeys(sections) {
		fmt.Fprintf(w, "%s\n", section)
		groups := sections[section]
		for _, group := range sortedKeys(groups) {
			fmt.Fprintf(w, "  %s\n", group)
			sort.Slice(groups[group], func(i, j int) bool {
				return metricLabel(groups[group][i].Name) < metricLabel(groups[group][j].Name)
			})
			for _, metric := range groups[group] {
				fmt.Fprintf(w, "    %-34s %s\n", metricLabel(metric.Name)+":", formatValue(metric.Value, metric.Unit))
			}
		}
		fmt.Fprintln(w)
	}
}

func writeStates(w io.Writer, states []pulse.State) {
	if len(states) == 0 {
		return
	}
	sort.Slice(states, func(i, j int) bool {
		return states[i].Key+dimensionSuffix(states[i].Dimensions) < states[j].Key+dimensionSuffix(states[j].Dimensions)
	})
	fmt.Fprintln(w, "States")
	for _, state := range states {
		name := state.Key
		if suffix := dimensionSuffix(state.Dimensions); suffix != "" {
			name += " " + suffix
		}
		fmt.Fprintf(w, "  %-48s %v\n", name+":", state.Value)
	}
	fmt.Fprintln(w)
}

func writeEvents(w io.Writer, events []pulse.Event) {
	if len(events) == 0 {
		return
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Kind < events[j].Kind
	})
	fmt.Fprintln(w, "Events")
	for _, event := range events {
		name := event.Kind
		if suffix := dimensionSuffix(event.Dimensions); suffix != "" {
			name += " " + suffix
		}
		fmt.Fprintf(w, "  %s\n", name)
		if len(event.Payload) > 0 {
			fmt.Fprintf(w, "    payload: %v\n", event.Payload)
		}
	}
	fmt.Fprintln(w)
}

func sectionName(name string) string {
	head, _, _ := strings.Cut(name, ".")
	switch head {
	case "docker":
		return "Docker"
	case "macos":
		return "macOS"
	case "package":
		return "Packages"
	case "system":
		return "System"
	default:
		return strings.Title(head)
	}
}

func groupName(name string, dims map[string]string) string {
	parts := strings.Split(name, ".")
	if len(parts) < 3 {
		if suffix := compactDims(dims); suffix != "" {
			return suffix
		}
		return "general"
	}
	groupParts := parts[1 : len(parts)-1]
	group := strings.Join(groupParts, ".")
	if suffix := preferredDimensionSuffix(dims); suffix != "" {
		group += " " + suffix
	}
	return group
}

func metricLabel(name string) string {
	parts := strings.Split(name, ".")
	if len(parts) == 0 {
		return name
	}
	return parts[len(parts)-1]
}

func preferredDimensionSuffix(dims map[string]string) string {
	keys := []string{"container", "mount", "display", "gpu", "interface", "volume", "script", "submodule"}
	var parts []string
	for _, key := range keys {
		if value := dims[key]; value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if len(parts) == 0 {
		return compactDims(dims)
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func dimensionSuffix(dims map[string]string) string {
	return preferredDimensionSuffix(dims)
}

func compactDims(dims map[string]string) string {
	var parts []string
	for _, key := range sortedKeys(dims) {
		if key == "collector" || key == "host" {
			continue
		}
		if dims[key] != "" {
			parts = append(parts, key+"="+dims[key])
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func formatValue(value float64, unit string) string {
	switch unit {
	case "bytes":
		return formatBytes(value)
	case "percent":
		return fmt.Sprintf("%.1f%%", value)
	case "seconds":
		return formatSeconds(value)
	case "celsius":
		return fmt.Sprintf("%.1f °C", value)
	case "count":
		return strconv.FormatFloat(value, 'f', 0, 64)
	case "hertz":
		return fmt.Sprintf("%.2f Hz", value)
	case "millivolt":
		return fmt.Sprintf("%.2f V", value/1000)
	case "milliampere":
		return fmt.Sprintf("%.0f mA", value)
	case "milliwatt":
		return fmt.Sprintf("%.2f W", value/1000)
	case "watt":
		return fmt.Sprintf("%.1f W", value)
	case "minutes":
		return fmt.Sprintf("%.0f min", value)
	default:
		if unit == "" {
			return strconv.FormatFloat(value, 'f', 2, 64)
		}
		return strconv.FormatFloat(value, 'f', 2, 64) + " " + unit
	}
}

func formatBytes(value float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	idx := 0
	for math.Abs(value) >= 1024 && idx < len(units)-1 {
		value /= 1024
		idx++
	}
	if idx == 0 {
		return fmt.Sprintf("%.0f %s", value, units[idx])
	}
	return fmt.Sprintf("%.2f %s", value, units[idx])
}

func formatSeconds(value float64) string {
	if value < 120 {
		return fmt.Sprintf("%.0f s", value)
	}
	if value < 7200 {
		return fmt.Sprintf("%.1f min", value/60)
	}
	if value < 172800 {
		return fmt.Sprintf("%.1f h", value/3600)
	}
	return fmt.Sprintf("%.1f d", value/86400)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
