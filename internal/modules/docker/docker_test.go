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

func TestContainerDimsIncludesComposeLabels(t *testing.T) {
	dims := containerDims(containerSummary{
		ID:    "1234567890abcdef",
		Names: []string{"/api-1"},
		Image: "example/api:latest",
		Labels: map[string]string{
			"com.docker.compose.project": "pulse",
			"com.docker.compose.service": "api",
		},
	})
	if dims["container"] != "api-1" {
		t.Fatalf("container dim = %q", dims["container"])
	}
	if dims["compose_project"] != "pulse" || dims["compose_service"] != "api" {
		t.Fatalf("compose dims = %v", dims)
	}
}

func TestUniqueImageIDsDeduplicatesAndSorts(t *testing.T) {
	got := uniqueImageIDs([]containerSummary{
		{ImageID: "sha256:bbb"},
		{ImageID: ""},
		{ImageID: "sha256:aaa"},
		{ImageID: "sha256:bbb"},
	})
	want := []string{"sha256:aaa", "sha256:bbb"}
	if len(got) != len(want) {
		t.Fatalf("len=%d got=%v", len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got=%v want=%v", got, want)
		}
	}
}

func TestParseDockerTimeRejectsEmptyZeroAndInvalid(t *testing.T) {
	if _, ok := parseDockerTime(""); ok {
		t.Fatal("empty time parsed")
	}
	if _, ok := parseDockerTime("0001-01-01T00:00:00Z"); ok {
		t.Fatal("zero time parsed")
	}
	if _, ok := parseDockerTime("nope"); ok {
		t.Fatal("invalid time parsed")
	}
	if _, ok := parseDockerTime("2026-06-09T00:00:00.000000000Z"); !ok {
		t.Fatal("valid time did not parse")
	}
}
