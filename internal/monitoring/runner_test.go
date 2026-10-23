package monitoring

import (
	"context"
	"errors"
	"fmt"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
	"reflect"
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

func TestRunnerGrantsEachCollectorItsBudget(t *testing.T) {
	calls := 0
	sender := &recordingSender{}
	runner := Runner{Resource: pulse.ResourceRef{Type: "host", ID: "test"}, Interval: 20 * time.Millisecond, Timeout: 20 * time.Millisecond, Sender: sender, Collectors: []Collector{deadlineCollector{&calls}, deadlineCollector{&calls}}}
	start := time.Now()
	err := runner.Once(context.Background())
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("started %d collectors", calls)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cycle exceeded budget")
	}
	if len(sender.batch.Events) != 2 {
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

type callbackCollector struct {
	name    string
	collect func(context.Context, Scope) (pulse.Batch, error)
}

func (c callbackCollector) Name() string { return c.name }
func (c callbackCollector) Collect(ctx context.Context, scope Scope) (pulse.Batch, error) {
	return c.collect(ctx, scope)
}

func TestRunnerRetriesUnsuccessfulCollectorsNextTick(t *testing.T) {
	for _, failure := range []string{"error", "cancelled with nil error", "invalid batch"} {
		t.Run(failure, func(t *testing.T) {
			now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			c := callbackCollector{name: "daily", collect: func(_ context.Context, scope Scope) (pulse.Batch, error) {
				calls++
				if calls == 1 {
					switch failure {
					case "error":
						return pulse.Batch{}, errors.New("failed")
					case "cancelled with nil error":
						cancel()
					case "invalid batch":
						b := NewBuilder(scope)
						b.Metric("test.invalid", "invalid", 1, "", nil)
						return b.Batch(), nil
					}
				}
				return pulse.Batch{}, nil
			}}
			r := Runner{Interval: time.Minute, Now: func() time.Time { return now },
				Collectors: []Collector{WithCadence{Collector: c, Every: 24 * time.Hour}}}
			b := r.Collect(ctx, false)
			if len(b.Events) != 1 || len(r.lastRun) != 0 {
				t.Fatalf("failure was marked successful: events=%v lastRun=%v", b.Events, r.lastRun)
			}
			now = now.Add(time.Minute)
			r.Collect(context.Background(), false)
			if calls != 2 || r.lastRun["daily"] != now {
				t.Fatalf("not retried next tick: calls=%d lastRun=%v", calls, r.lastRun)
			}
			now = now.Add(time.Minute)
			r.Collect(context.Background(), false)
			if calls != 2 {
				t.Fatal("successful collector ignored its cadence")
			}
		})
	}
}

func TestRunnerPrioritizesHealthCollectorsWithStableOrder(t *testing.T) {
	for _, exhaustBudget := range []bool{false, true} {
		t.Run(fmt.Sprint(exhaustBudget), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var order []string
			makeCollector := func(name string, cadence time.Duration) Collector {
				return WithCadence{Every: cadence, Collector: callbackCollector{name: name,
					collect: func(context.Context, Scope) (pulse.Batch, error) {
						order = append(order, name)
						if exhaustBudget && name == "daily" {
							cancel()
						}
						return pulse.Batch{}, nil
					}}}
			}
			r := Runner{Interval: time.Minute, Collectors: []Collector{
				makeCollector("daily", 24*time.Hour), makeCollector("health-a", time.Minute),
				makeCollector("hourly", time.Hour), makeCollector("health-b", 30*time.Second),
			}}
			r.Collect(ctx, false)
			want := []string{"health-a", "health-b", "daily", "hourly"}
			if exhaustBudget {
				want = want[:3]
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("order=%v want=%v", order, want)
			}
			if r.Collectors[0].Name() != "daily" {
				t.Fatal("runner mutated configured collector order")
			}
		})
	}
}

type budgetCollector struct {
	callbackCollector
	budget time.Duration
}

func (c budgetCollector) CollectTimeout() time.Duration { return c.budget }

func TestRunnerCollectorAndCycleBudgets(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var order []string
	var healthDeadline, backupDeadline time.Time
	probe := func(name string, deadline *time.Time) callbackCollector {
		return callbackCollector{name: name, collect: func(ctx context.Context, _ Scope) (pulse.Batch, error) {
			order = append(order, name)
			var ok bool
			*deadline, ok = ctx.Deadline()
			if !ok {
				t.Fatal("missing deadline")
			}
			return pulse.Batch{}, nil
		}}
	}
	health := probe("health", &healthDeadline)
	backup := ScopedCollector{Collector: WithCadence{Every: 15 * time.Minute, Collector: budgetCollector{probe("backup", &backupDeadline), 180 * time.Second}}}
	runner := Runner{Interval: time.Minute, Timeout: 10 * time.Second, Now: func() time.Time { return now }, Collectors: []Collector{backup, health}}
	if got := runner.MaxCycleTimeout(); got != 190*time.Second {
		t.Fatalf("wrapped timeout=%s", got)
	}
	for _, all := range []bool{false, true} {
		order = nil
		started := time.Now()
		runner.Collect(context.Background(), all)
		if !reflect.DeepEqual(order, []string{"health", "backup"}) {
			t.Fatal(order)
		}
		for _, tc := range []struct {
			deadline time.Time
			want     time.Duration
		}{{healthDeadline, 10 * time.Second}, {backupDeadline, 180 * time.Second}} {
			got := tc.deadline.Sub(started)
			if got < tc.want-time.Second || got > tc.want+time.Second {
				t.Fatalf("budget=%s want=%s", got, tc.want)
			}
		}
	}
	// A budget on a collector that is not due must not extend the cycle.
	runner.Timeout = 0
	order = nil
	started := time.Now()
	runner.Collect(context.Background(), false)
	if !reflect.DeepEqual(order, []string{"health"}) {
		t.Fatal(order)
	}
	if got := healthDeadline.Sub(started); got < 59*time.Second || got > 61*time.Second {
		t.Fatalf("non-due backup extended cycle: %s", got)
	}
	// A parent's cancellation/deadline always wins, including once mode.
	parentDeadline := time.Now().Add(5 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), parentDeadline)
	defer cancel()
	runner.Collect(ctx, true)
	if !backupDeadline.Equal(parentDeadline) {
		t.Fatal("parent deadline ignored")
	}
}

func TestSlowTimeoutKeepsCadenceAndDoesNotShortenFollowingBudgets(t *testing.T) {
	now := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	fast, slow := 0, 0
	var healthDeadline time.Time
	health := budgetCollector{callbackCollector{name: "health", collect: func(ctx context.Context, _ Scope) (pulse.Batch, error) {
		fast++
		healthDeadline, _ = ctx.Deadline()
		return pulse.Batch{}, nil
	}}, 20 * time.Millisecond}
	daily := WithCadence{Every: 24 * time.Hour, Collector: budgetCollector{callbackCollector{name: "daily", collect: func(ctx context.Context, _ Scope) (pulse.Batch, error) {
		slow++
		deadline, _ := ctx.Deadline()
		if !deadline.After(healthDeadline) {
			t.Fatal("earlier health collection shortened slow budget")
		}
		<-ctx.Done()
		return pulse.Batch{}, ctx.Err()
	}}, 20 * time.Millisecond}}
	r := Runner{Interval: time.Millisecond, Now: func() time.Time { return now }, Collectors: []Collector{health, daily}}
	for i := 0; i < 3; i++ {
		r.Collect(context.Background(), false)
		now = now.Add(time.Minute)
	}
	if fast != 3 || slow != 1 {
		t.Fatalf("health=%d daily=%d", fast, slow)
	}
	if _, ok := r.lastRun["daily"]; ok {
		t.Fatal("timeout marked successful")
	}
	now = now.Add(24 * time.Hour)
	r.Collect(context.Background(), false)
	if slow != 2 {
		t.Fatal("timed-out phase never retried")
	}
}
