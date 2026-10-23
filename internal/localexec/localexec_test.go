package localexec

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOutputBoundAndCancellation(t *testing.T) {
	out, err := Output(context.Background(), 4, "sh", "-c", "printf 1234")
	if err != nil || string(out) != "1234" {
		t.Fatalf("%q %v", out, err)
	}
	_, err = Output(context.Background(), 4, "sh", "-c", "printf 12345")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatal(err)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	if _, err := Output(ctx, 4, "sh", "-c", "printf 1"); err == nil {
		t.Fatal("cancel ignored")
	}
}

func TestTimeoutKillsProbeChildren(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := Output(ctx, 4096, "sh", "-c", "sleep 30 & wait")
	if err != context.DeadlineExceeded {
		t.Fatalf("err=%v", err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("probe descendants kept pipes open past timeout")
	}
}

func TestCombinedOutputIncludesStatusAndBoundsBothStreams(t *testing.T) {
	out, err := CombinedOutput(context.Background(), 32, "sh", "-c", "printf header; printf status >&2")
	if err != nil || string(out) != "headerstatus" {
		t.Fatal(string(out), err)
	}
	if _, err := CombinedOutput(context.Background(), 8, "sh", "-c", "printf header; printf status >&2"); err == nil {
		t.Fatal("combined output limit ignored")
	}
	out, err = Output(context.Background(), 32, "sh", "-c", "printf header; printf status >&2")
	if err != nil || string(out) != "header" {
		t.Fatal("existing stdout-only behavior changed", string(out), err)
	}
}
