package script

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Script struct {
	Name           string
	Command        []string
	Timeout        time.Duration
	MaxOutputBytes int64
	Dimensions     map[string]string
}

type Collector struct {
	Scripts []Script
}

type output struct {
	Dimensions map[string]string `json:"dimensions"`
	Metrics    []pulse.Metric    `json:"metrics"`
	Events     []pulse.Event     `json:"events"`
	States     []pulse.State     `json:"states"`
}

func (c Collector) Name() string { return "script" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	var merged pulse.Batch
	var errs []error
	for _, script := range c.Scripts {
		part, err := runScript(ctx, scope, script)
		if err != nil {
			errs = append(errs, err)
			b.EventDetails("script.failed", map[string]string{"script": script.Name, "operation": "execute"}, monitoring.EventDetails{
				Attributes: map[string]any{"error": err.Error()},
			})
			b.State("script.ok", false, map[string]string{"script": script.Name})
			continue
		}
		monitoring.Merge(&merged, part)
		b.State("script.ok", true, map[string]string{"script": script.Name})
	}
	monitoring.Merge(&merged, b.Batch())
	if len(merged.Metrics) > 0 || len(merged.Events) > 0 || len(merged.States) > 0 {
		return merged, nil
	}
	return merged, errors.Join(errs...)
}

func runScript(ctx context.Context, scope monitoring.Scope, script Script) (pulse.Batch, error) {
	if script.Name == "" {
		return pulse.Batch{}, errors.New("script name is required")
	}
	if len(script.Command) == 0 {
		return pulse.Batch{}, fmt.Errorf("script %q command is required", script.Name)
	}
	timeout := script.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	maxOutput := script.MaxOutputBytes
	if maxOutput <= 0 {
		maxOutput = 1 << 20
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, script.Command[0], script.Command[1:]...)
	stdout := &limitBuffer{limit: int(maxOutput)}
	stderr := &limitBuffer{limit: 4096}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	if runCtx.Err() != nil {
		return pulse.Batch{}, fmt.Errorf("script %q timed out: %w", script.Name, runCtx.Err())
	}
	if stdout.Truncated() {
		return pulse.Batch{}, fmt.Errorf("script %q output too large", script.Name)
	}
	if err != nil {
		msg := bytes.TrimSpace(stderr.Bytes())
		if len(msg) > 0 {
			return pulse.Batch{}, fmt.Errorf("script %q failed: %w: %s", script.Name, err, string(msg))
		}
		return pulse.Batch{}, fmt.Errorf("script %q failed: %w", script.Name, err)
	}
	var out output
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return pulse.Batch{}, fmt.Errorf("script %q invalid json: %w", script.Name, err)
	}
	batch := pulse.Batch{
		Metrics: out.Metrics,
		Events:  out.Events,
		States:  out.States,
	}
	return monitoring.Inject(batch, scope, merge(script.Dimensions, merge(out.Dimensions, map[string]string{"script": script.Name}))), nil
}

type limitBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitBuffer) Write(p []byte) (int, error) {
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			_, _ = b.buf.Write(p[:remaining])
			b.truncated = true
		} else {
			_, _ = b.buf.Write(p)
		}
	} else if len(p) > 0 {
		b.truncated = true
	}
	return len(p), nil
}

func (b *limitBuffer) Bytes() []byte {
	return b.buf.Bytes()
}

func (b *limitBuffer) Truncated() bool {
	return b.truncated
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		if v != "" {
			out[k] = v
		}
	}
	for k, v := range b {
		if v != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
