package docker

import "testing"

func TestDockerMemoryUsagePrefersInactiveFile(t *testing.T) {
	got := dockerMemoryUsage(1000, map[string]uint64{"inactive_file": 200, "cache": 900})
	if got != 800 {
		t.Fatalf("usage = %d", got)
	}
}

func TestDockerBlockIOAggregatesByOperation(t *testing.T) {
	var stats statsResponse
	stats.BlkioStats.IoServiceBytesRecursive = append(stats.BlkioStats.IoServiceBytesRecursive,
		struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		}{Op: "Read", Value: 10},
		struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		}{Op: "read", Value: 5},
		struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		}{Op: "Write", Value: 7},
	)
	read, write := dockerBlockIO(stats)
	if read != 15 || write != 7 {
		t.Fatalf("read=%d write=%d", read, write)
	}
}
