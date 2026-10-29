package uptime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/validation"
)

func fakeSuccessfulPing(_ context.Context, target Target) ([]byte, error) {
	var output strings.Builder
	for i := 1; i <= target.pingCount(); i++ {
		fmt.Fprintf(&output, "64 bytes from 192.0.2.1: icmp_seq=%d ttl=64 time=5.000 ms\n", i)
	}
	fmt.Fprintf(&output, "%d packets transmitted, %d received, 0%% packet loss\n", target.pingCount(), target.pingCount())
	return []byte(output.String()), nil
}

func pingFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParsePing(t *testing.T) {
	for _, platform := range []string{"iputils", "macos"} {
		for _, tc := range []struct {
			name string
			want pingStats
		}{
			{"full", pingStats{4, 0, 1, 2.5, 4, math.Sqrt(1.25)}},
			{"partial", pingStats{2, 50, 1, 2.5, 4, 1.5}},
			{"total", pingStats{loss: 100}},
			{"duplicates", pingStats{4, 0, 1, 2.5, 4, math.Sqrt(1.25)}},
		} {
			t.Run(platform+"-"+tc.name, func(t *testing.T) {
				got, err := parsePing(pingFixture(t, platform+"-"+tc.name), 4)
				if err != nil || got != tc.want {
					t.Fatalf("stats=%+v want=%+v err=%v", got, tc.want, err)
				}
			})
		}
	}
}

func TestParsePingLateReplies(t *testing.T) {
	for _, platform := range []string{"iputils", "macos"} {
		stats, err := parsePing(pingFixture(t, platform+"-late"), 4)
		want := pingStats{received: 2, loss: 50, min: 1, median: 2, max: 3, stddev: 1}
		if err != nil || stats != want {
			t.Fatalf("%s stats=%+v want=%+v err=%v", platform, stats, want, err)
		}
	}
	// A hidden late duplicate must not remove a timely original reply.
	output := strings.Replace(string(pingFixture(t, "macos-full")), "0% packet loss", "0% packet loss, 1 packets out of wait time", 1)
	stats, err := parsePing([]byte(output), 4)
	if err != nil || stats.loss != 0 || stats.received != 4 {
		t.Fatalf("%+v %v", stats, err)
	}
}

func TestParsePingRejectsIncompleteOrMalformedOutput(t *testing.T) {
	full := string(pingFixture(t, "iputils-full"))
	for _, output := range []string{
		"", "ping: socket: Operation not permitted", strings.Split(full, "---")[0],
		strings.Replace(full, "4 packets transmitted", "3 packets transmitted", 1),
		strings.Replace(full, "4 received", "5 received", 1),
		strings.Replace(full, "time=2.000", "time=unknown", 1),
		strings.Replace(full, "icmp_seq=2", "icmp_seq=1", 1),
	} {
		if _, err := parsePing([]byte(output), 4); err == nil {
			t.Fatalf("accepted %q", output)
		}
	}
}

func TestParsePingOddMedianAndSubMillisecondReply(t *testing.T) {
	data := []byte("64 bytes from 192.0.2.1: icmp_seq=0 ttl=64 time<1 ms\n1 packets transmitted, 1 packets received, 0.0% packet loss\n")
	stats, err := parsePing(data, 1)
	if err != nil || stats.min != 1 || stats.median != 1 || stats.max != 1 || stats.stddev != 0 {
		t.Fatalf("%+v %v", stats, err)
	}
}

func TestICMPRoundMetrics(t *testing.T) {
	threshold := 50.0
	for _, tc := range []struct {
		name, fixture, state string
		threshold            *float64
		loss                 float64
		metricCount          int
	}{
		{"healthy", "full", "ok", nil, 0, 6},
		{"partial", "partial", "ok", nil, 50, 6},
		{"threshold", "partial", "failed", &threshold, 50, 6},
		{"total", "total", "failed", nil, 100, 2},
		{"duplicates", "duplicates", "ok", nil, 0, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output := pingFixture(t, "iputils-"+tc.fixture)
			c := Collector{Targets: []Target{{ID: "router", Label: "Router", Kind: KindICMP, Address: "router.example.lan", Count: 4, Interval: time.Second, LossThreshold: tc.threshold}},
				Ping: func(_ context.Context, target Target) ([]byte, error) {
					if target.Count != 4 || target.Interval != time.Second || target.Address != "router.example.lan" {
						t.Errorf("target=%+v", target)
					}
					return output, nil
				},
			}
			batch, err := c.Collect(context.Background(), fixedScope())
			if err != nil {
				t.Fatal(err)
			}
			if err := validation.Batch(batch); err != nil {
				t.Fatal(err)
			}
			if len(batch.Metrics) != tc.metricCount || metricValue(batch, "uptime.icmp.loss", "router") != tc.loss || stateValue(batch, "uptime.check.result", "router") != tc.state {
				t.Fatalf("batch=%+v", batch)
			}
			if metricValue(batch, "uptime.check.duration", "router") != -1 {
				t.Fatal("ICMP emitted command duration")
			}
			wantSuccess := 1.0
			if tc.state == "failed" {
				wantSuccess = 0
			}
			if metricValue(batch, "uptime.check.success", "router") != wantSuccess {
				t.Fatal("incorrect success")
			}
			for _, m := range batch.Metrics {
				wantUnit := "milliseconds"
				if m.Name == "uptime.icmp.loss" {
					wantUnit = "percent"
				}
				if m.Name == "uptime.check.success" {
					wantUnit = ""
				}
				if m.Unit != wantUnit || m.Type != "gauge" {
					t.Fatalf("metric=%+v", m)
				}
			}
		})
	}
}

func TestICMPUnmeasuredRoundsEmitNoMetrics(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output []byte
		err    error
		cancel bool
	}{
		{name: "missing", err: exec.ErrNotFound},
		{name: "execution", err: errors.New("permission denied")},
		{name: "parse", output: []byte("unexpected output")},
		{name: "incomplete", output: pingFixture(t, "iputils-full")},
		{name: "cancelled", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := Collector{Targets: []Target{{ID: "router", Kind: KindICMP}}, Ping: func(context.Context, Target) ([]byte, error) {
				if tc.cancel {
					cancel()
				}
				return tc.output, tc.err
			}}
			batch, err := c.Collect(ctx, fixedScope())
			if err != nil || len(batch.Metrics) != 0 || stateValue(batch, "uptime.check.result", "router") != "error" {
				t.Fatalf("batch=%+v err=%v", batch, err)
			}
		})
	}
}

func TestPingArgumentsAndRoundBudget(t *testing.T) {
	for _, platform := range []string{"linux", "darwin"} {
		lossExit, want := 1, []string{"-n", "-c", "20", "-i", "0.5", "-W", "2", "router.example.lan"}
		if platform == "darwin" {
			lossExit, want = 2, []string{"-n", "-c", "20", "-i", "0.5", "-W", "2000", "-t", "12", "router.example.lan"}
		}
		if got := pingArgs(Target{Address: "router.example.lan"}, platform); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %v", platform, got)
		}
		if pingLossExitCode(platform) != lossExit {
			t.Fatal("incorrect loss exit code")
		}
	}
	for _, tc := range []struct {
		target Target
		want   time.Duration
	}{
		{Target{}, 15 * time.Second}, {Target{Count: 1}, 6 * time.Second},
		{Target{Count: 100}, 55 * time.Second}, {Target{Count: 10, Interval: time.Second}, 15 * time.Second},
		{Target{Count: 1, Interval: time.Minute}, 65 * time.Second},
	} {
		if got := tc.target.ICMPTimeout(); got != tc.want {
			t.Fatalf("timeout=%s want=%s", got, tc.want)
		}
	}
	if (Collector{Timeout: 5 * time.Second}).targetTimeout(Target{Kind: KindICMP}) != 15*time.Second {
		t.Fatal("generic timeout truncates round")
	}
}

func TestRunPingUsesLocaleAndHandlesPlatformExitCodes(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("Unix ping platforms only")
	}
	path := t.TempDir()
	t.Setenv("PATH", path)
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	for _, tc := range []struct {
		exit      int
		wantError bool
	}{{0, false}, {pingLossExitCode(runtime.GOOS), false}, {3, true}} {
		script := fmt.Sprintf("#!/bin/sh\n[ \"$LC_ALL\" = C ] || exit 4\nprintf '%%s\\n' '4 packets transmitted, 0 received, 100%% packet loss'\nexit %d\n", tc.exit)
		if err := os.WriteFile(filepath.Join(path, "ping"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		output, err := runPing(context.Background(), Target{Count: 4, Address: "router.example.lan"})
		if (err != nil) != tc.wantError || !strings.Contains(string(output), "4 packets transmitted") {
			t.Fatalf("output=%s err=%v", output, err)
		}
	}
}

func TestICMPRoundsShareConcurrencyLimit(t *testing.T) {
	entered := make(chan struct{}, 3)
	release := make(chan struct{})
	var active, peak atomic.Int32
	c := Collector{Concurrency: 2, Targets: []Target{
		{ID: "a", Kind: KindICMP}, {ID: "b", Kind: KindICMP}, {ID: "c", Kind: KindICMP},
	}, Ping: func(ctx context.Context, target Target) ([]byte, error) {
		n := active.Add(1)
		for previous := peak.Load(); n > previous; previous = peak.Load() {
			if peak.CompareAndSwap(previous, n) {
				break
			}
		}
		entered <- struct{}{}
		<-release
		defer active.Add(-1)
		return fakeSuccessfulPing(ctx, target)
	}}
	done := make(chan error, 1)
	go func() { _, err := c.Collect(context.Background(), fixedScope()); done <- err }()
	<-entered
	<-entered
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 2 {
		t.Fatalf("peak concurrency=%d", peak.Load())
	}
	if len(entered) != 1 {
		t.Fatal("not all rounds completed")
	}
}
