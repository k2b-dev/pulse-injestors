package pulse

import (
	"fmt"
	"testing"
	"time"
)

func TestBlockMetricPrunesExpiredEntries(t *testing.T) {
	stats := &Stats{}
	stats.blockMetric("expired", fixedNow.Add(-MetricBlockDuration))
	stats.blockMetric("active", fixedNow.Add(-time.Minute))
	stats.blockMetric("new", fixedNow)
	if len(stats.blocked) != 2 || !stats.MetricBlocked("active", fixedNow) || !stats.MetricBlocked("new", fixedNow) {
		t.Fatalf("blocked = %v", stats.blocked)
	}
	if stats.MetricBlocked("new", fixedNow.Add(MetricBlockDuration)) {
		t.Fatal("block did not expire")
	}
}

func TestShouldLogPrunesOldKeysAboveThreshold(t *testing.T) {
	stats := &Stats{}
	for i := 0; i < 1000; i++ {
		stats.ShouldLog(fmt.Sprint(i), fixedNow.Add(-25*time.Hour), time.Minute)
	}
	if len(stats.lastNotice) != 1000 {
		t.Fatal("unexpected initial log entries")
	}
	stats.ShouldLog("recent", fixedNow, time.Minute)
	if len(stats.lastNotice) != 1 || stats.ShouldLog("recent", fixedNow.Add(time.Second), time.Minute) {
		t.Fatalf("log entries = %v", stats.lastNotice)
	}
}
