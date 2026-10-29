package uptime

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const pingReplyTimeout = 2 * time.Second

func (t Target) pingCount() int {
	if t.Count == 0 {
		return 20
	}
	return t.Count
}

func (t Target) pingInterval() time.Duration {
	if t.Interval == 0 {
		return 500 * time.Millisecond
	}
	return t.Interval
}

// ICMPTimeout budgets the packet train, final reply, and command startup/DNS.
// BSD ping waits one more interval before entering its final-reply wait.
func (t Target) ICMPTimeout() time.Duration {
	budget := time.Duration(t.pingCount())*t.pingInterval() + pingReplyTimeout + 3*time.Second
	return ((budget + time.Second - 1) / time.Second) * time.Second
}

type pingStats struct {
	received                       int
	loss, min, median, max, stddev float64
}

var pingSummary = regexp.MustCompile(`(?m)^(\d+) packets transmitted, (\d+) (?:packets )?received`)
var pingLate = regexp.MustCompile(`, (\d+) packets out of wait time`)
var pingReply = regexp.MustCompile(`icmp_seq[= ](\d+).*\btime[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)

// parsePing uses the first reply per sequence. Duplicates affect neither loss
// nor RTT statistics. Summary counts guard against incomplete/unknown output.
func parsePing(output []byte, count int) (pingStats, error) {
	summary := pingSummary.FindSubmatch(output)
	if summary == nil {
		return pingStats{}, errors.New("ping summary missing")
	}
	sent, err := strconv.Atoi(string(summary[1]))
	if err != nil || sent != count || sent < 1 || sent > 100 {
		return pingStats{}, errors.New("incomplete ping round")
	}
	received, err := strconv.Atoi(string(summary[2]))
	if err != nil || received > sent {
		return pingStats{}, errors.New("invalid ping reply count")
	}
	late := 0
	if match := pingLate.FindSubmatch(output); match != nil {
		late, err = strconv.Atoi(string(match[1]))
		if err != nil {
			return pingStats{}, errors.New("invalid ping late reply count")
		}
	}
	seen := make(map[string]bool, received)
	var rtts []float64
	for _, match := range pingReply.FindAllSubmatch(output, -1) {
		seq := string(match[1])
		if seen[seq] {
			continue
		}
		value, err := strconv.ParseFloat(string(match[2]), 64)
		if err != nil || math.IsInf(value, 0) {
			return pingStats{}, errors.New("invalid ping RTT")
		}
		seen[seq] = true
		// macOS hides replies beyond -W; apply the same cutoff to iputils.
		if value <= float64(pingReplyTimeout/time.Millisecond) {
			rtts = append(rtts, value)
		}
	}
	// macOS's late counter includes duplicates, so it may exceed the
	// number of hidden unique replies. Visible unique replies cannot exceed
	// the summary, and every missing reply must be explained by that counter.
	if len(seen) > received || received-len(seen) > late {
		return pingStats{}, errors.New("ping reply count does not match RTTs")
	}
	received = len(rtts)
	stats := pingStats{received: received, loss: 100 * float64(sent-received) / float64(sent)}
	if received == 0 {
		return stats, nil
	}
	sort.Float64s(rtts)
	stats.min, stats.max = rtts[0], rtts[len(rtts)-1]
	stats.median = (rtts[(received-1)/2] + rtts[received/2]) / 2
	var mean float64
	for _, value := range rtts {
		mean += value / float64(received)
	}
	for _, value := range rtts {
		stats.stddev += (value - mean) * (value - mean) / float64(received)
	}
	stats.stddev = math.Sqrt(stats.stddev)
	return stats, nil
}

func (c Collector) checkICMP(ctx context.Context, target Target) result {
	ping := c.Ping
	if ping == nil {
		ping = runPing
	}
	output, err := ping(ctx, target)
	if errors.Is(err, exec.ErrNotFound) {
		return result{err: "ping command unavailable"}
	}
	if err != nil {
		message := "ping command failed"
		if line, _, _ := strings.Cut(strings.TrimSpace(string(output)), "\n"); line != "" {
			message += ": " + bounded(line)
		}
		return result{err: message}
	}
	stats, err := parsePing(output, target.pingCount())
	if err != nil {
		return result{err: err.Error()}
	}
	threshold := 100.0
	if target.LossThreshold != nil {
		threshold = *target.LossThreshold
	}
	outcome := result{measured: true, success: stats.loss < threshold, icmp: stats}
	if !outcome.success {
		outcome.err = fmt.Sprintf("packet loss %.2f%% reached threshold %.2f%%", stats.loss, threshold)
	}
	return outcome
}

func pingArgs(target Target, goos string) []string {
	wait, deadlineFlag := "2", "-w" // iputils uses seconds; macOS uses milliseconds.
	if goos == "darwin" {
		wait, deadlineFlag = "2000", "-t"
	}
	// Let ping print its summary before the Go context kills the process.
	deadline := strconv.FormatInt(int64((target.ICMPTimeout()-3*time.Second)/time.Second), 10)
	return []string{"-n", "-c", strconv.Itoa(target.pingCount()), "-i", strconv.FormatFloat(target.pingInterval().Seconds(), 'f', -1, 64), "-W", wait, deadlineFlag, deadline, target.Address}
}

func runPing(ctx context.Context, target Target) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ping", pingArgs(target, runtime.GOOS)...)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	output, err := cmd.CombinedOutput()
	// No replies is exit 1 in iputils and exit 2 on macOS. Execution errors
	// remain unmeasured, even if they happened to produce a partial summary.
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && pingLossExitCode(runtime.GOOS) == exitErr.ExitCode() {
		err = nil
	}
	return output, err
}

func pingLossExitCode(goos string) int {
	if goos == "darwin" {
		return 2
	}
	return 1
}
