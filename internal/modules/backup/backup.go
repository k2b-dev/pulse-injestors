package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

type Marker struct {
	Name, MarkerFile string
	Command          []string
}
type Collector struct {
	Markers []Marker
	Run     localexec.Runner
}

func (c Collector) Name() string { return "backup" }
func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	if scope.Timestamp.IsZero() {
		scope.Timestamp = time.Now().UTC()
	}
	b := monitoring.NewBuilder(scope)
	for _, marker := range c.Markers {
		bs := scope
		bs.Resource = pulse.ResourceRef{Type: "backup", ID: entity.ID(entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions), marker.Name), Label: marker.Name}
		mb := monitoring.NewBuilder(bs)
		last, err := c.lastSuccess(ctx, marker)
		mb.State("backup.marker.available", err == nil, nil)
		if err == nil {
			mb.Metric("backup.last_success.age", "gauge", scope.Timestamp.Sub(last).Seconds(), "seconds", nil)
			mb.State("backup.last_success.time", last.UTC().Format(time.RFC3339), nil)
		} else if !errors.Is(err, os.ErrNotExist) {
			// Command stdout/stderr can contain credentials; never include it in telemetry.
			mb.EventDetails("backup.collect.failed", nil, monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
		}
		b.Merge(mb.Batch())
	}
	return b.Batch(), nil
}
func (c Collector) lastSuccess(ctx context.Context, m Marker) (time.Time, error) {
	if m.MarkerFile != "" {
		info, err := os.Stat(m.MarkerFile)
		if err != nil {
			return time.Time{}, err
		}
		if !info.Mode().IsRegular() {
			return time.Time{}, errors.New("backup marker is not a regular file")
		}
		return info.ModTime(), nil
	}
	if len(m.Command) == 0 {
		return time.Time{}, errors.New("backup command missing")
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	run := c.Run
	if run == nil {
		run = func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return localexec.Output(ctx, 4096, name, args...)
		}
	}
	out, err := run(runCtx, m.Command[0], m.Command[1:]...)
	if err != nil {
		return time.Time{}, fmt.Errorf("backup command failed: %w", err)
	}
	if len(out) > 4096 {
		return time.Time{}, errors.New("backup output exceeds 4096 bytes")
	}
	value := strings.TrimSpace(string(out))
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= -62135596800 && seconds <= 253402300799 {
		return time.Unix(seconds, 0).UTC(), nil
	}
	if ts, err := time.Parse(time.RFC3339, value); err == nil {
		return ts, nil
	}
	return time.Time{}, errors.New("backup command must print RFC3339 or unix seconds")
}
