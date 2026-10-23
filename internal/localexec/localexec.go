// Package localexec runs local read-only probes with bounded output.
package localexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
	stdout, stderr := &limitedBuffer{limit: limit}, &limitedBuffer{limit: 4096}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if stdout.overflow {
		return nil, fmt.Errorf("%s output exceeds %d bytes", name, limit)
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		exit.Stderr = stderr.buf.Bytes()
	}
	return stdout.buf.Bytes(), err
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
