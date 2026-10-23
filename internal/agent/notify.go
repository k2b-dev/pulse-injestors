package agent

import (
	"context"
	"net"
	"os"
	"strconv"
	"sync/atomic"
	"time"
)

// notify sends a systemd sd_notify message when NOTIFY_SOCKET is set.
// Without systemd it does nothing.
func notify(state string) {
	name := os.Getenv("NOTIFY_SOCKET")
	if name == "" {
		return
	}
	if name[0] == '@' {
		name = "\x00" + name[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}

// watchdogAlive allows slow collection but stops pings if the loop stalls.
func watchdogAlive(lastCompleted, now time.Time, interval, longestCollectTimeout time.Duration) bool {
	return !lastCompleted.IsZero() && now.Sub(lastCompleted) <= 2*interval+longestCollectTimeout+60*time.Second
}

func watchdogLoop(ctx context.Context, usec string, interval, longestCollectTimeout time.Duration, lastCompleted *atomic.Int64) {
	period, err := time.ParseDuration(usec + "us")
	if _, parseErr := strconv.ParseUint(usec, 10, 64); parseErr != nil || err != nil || period <= 0 {
		return
	}
	ticker := time.NewTicker(period / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if watchdogAlive(time.Unix(0, lastCompleted.Load()), now, interval, longestCollectTimeout) {
				notify("WATCHDOG=1")
			}
		}
	}
}
