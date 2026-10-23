package monitoring

import (
	"context"
	"errors"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"testing"
	"time"
)

type deadlineCollector struct{ calls *int }

func (c deadlineCollector) Name() string { return "deadline" }
func (c deadlineCollector) Collect(ctx context.Context, _ Scope) (pulse.Batch, error) {
	*c.calls++
	<-ctx.Done()
	return pulse.Batch{}, ctx.Err()
}

type recordingSender struct{ batch pulse.Batch }

func (s *recordingSender) PostBatch(_ context.Context, b pulse.Batch) error { s.batch = b; return nil }

func TestRunnerSharesCycleDeadlineAndStopsStartingCollectors(t *testing.T) {
	calls := 0
	sender := &recordingSender{}
	runner := Runner{Resource: pulse.ResourceRef{Type: "host", ID: "test"}, Interval: 20 * time.Millisecond, Timeout: time.Second, Sender: sender, Collectors: []Collector{deadlineCollector{&calls}, deadlineCollector{&calls}}}
	start := time.Now()
	err := runner.Once(context.Background())
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("started %d collectors", calls)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cycle exceeded budget")
	}
	if len(sender.batch.Events) != 1 {
		t.Fatalf("failure events: %d", len(sender.batch.Events))
	}
}

type valueCollector struct {
	name string
	typ  string
}

func (c valueCollector) Name() string { return c.name }
func (c valueCollector) Collect(_ context.Context, scope Scope) (pulse.Batch, error) {
	b := NewBuilder(scope)
	b.Metric("test.value", c.typ, 1, "", nil)
	return b.Batch(), nil
}

func TestRunnerRejectsInvalidCollectorAndDeliversHealthySignals(t *testing.T) {
	sender := &recordingSender{}
	runner := Runner{Resource: pulse.ResourceRef{Type: "host", ID: "test"}, Sender: sender, Collectors: []Collector{valueCollector{"invalid", "histogram"}, valueCollector{"healthy", "gauge"}}}
	if err := runner.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(sender.batch.Metrics) != 1 || sender.batch.Metrics[0].Type != "gauge" {
		t.Fatalf("metrics: %#v", sender.batch.Metrics)
	}
	if len(sender.batch.Events) != 1 || sender.batch.Events[0].Dimensions["collector"] != "invalid" {
		t.Fatalf("events: %#v", sender.batch.Events)
	}
}

type countingCollector struct {
	name  string
	every time.Duration
	calls *int
}

func (c countingCollector) Name() string           { return c.name }
func (c countingCollector) Cadence() time.Duration { return c.every }
func (c countingCollector) Collect(_ context.Context, scope Scope) (pulse.Batch, error) {
	*c.calls++
	return NewBuilder(scope).Batch(), nil
}

func TestRunnerRunsCollectorsOnTheirCadence(t *testing.T) {
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	var fast, slow int
	runner := &Runner{
		Interval:   time.Minute,
		Now:        func() time.Time { return now },
		Collectors: []Collector{countingCollector{"fast", 0, &fast}, countingCollector{"slow", 5 * time.Minute, &slow}},
	}
	for i := 0; i < 11; i++ {
		runner.Collect(context.Background(), false)
		now = now.Add(time.Minute + time.Duration(i%3)*time.Second)
	}
	if fast != 11 || slow != 3 {
		t.Fatalf("fast=%d slow=%d", fast, slow)
	}
	runner.Collect(context.Background(), true)
	if slow != 4 {
		t.Fatal("collect all skipped a slow collector")
	}
}
