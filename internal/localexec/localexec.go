// Package localexec runs local read-only probes with bounded output.
package localexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

type Runner func(context.Context, string, ...string) ([]byte, error)

func Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return Output(ctx, 8<<20, name, args...)
}

func Output(ctx context.Context, limit int, name string, args ...string) ([]byte, error) {
	return output(ctx, limit, false, name, args...)
}

// CombinedOutput also includes bounded stderr for tools such as softwareupdate
// that write successful status messages there. Error strings omit that output.
func CombinedOutput(ctx context.Context, limit int, name string, args ...string) ([]byte, error) {
	return output(ctx, limit, true, name, args...)
}

func output(ctx context.Context, limit int, combined bool, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// A backup probe may spawn children. Cancel the entire probe group so a
	// descendant cannot outlive its timeout or keep stdout pipes open.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	stderr := &limitedBuffer{limit: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Read one extra byte to distinguish an exact-limit response from overflow.
	out, readErr := io.ReadAll(io.LimitReader(stdout, int64(limit)+1))
	overflow := len(out) > limit
	if overflow || readErr != nil {
		_ = cmd.Cancel()
	}
	err = cmd.Wait()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if overflow {
		return nil, fmt.Errorf("%s stdout exceeds %d bytes", name, limit)
	}
	if readErr != nil {
		return nil, readErr
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		exit.Stderr = stderr.buf.Bytes()
	}
	if combined {
		if stderr.overflow || len(out)+stderr.buf.Len() > limit {
			return nil, fmt.Errorf("%s combined output exceeds limit", name)
		}
		out = append(out, stderr.buf.Bytes()...)
	}
	return out, err
}

type limitedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.limit - b.buf.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, err := b.buf.Write(p)
	return n, err
}
