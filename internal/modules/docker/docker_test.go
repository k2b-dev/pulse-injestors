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

func TestUniqueImageJobsDeduplicatesSortsAndKeepsReferences(t *testing.T) {
	got := uniqueImageJobs([]containerSummary{
		{ImageID: "sha256:bbb", Image: "repo/b:latest"},
		{ImageID: ""},
		{ImageID: "sha256:aaa", Image: "repo/a:latest"},
		{ImageID: "sha256:bbb", Image: "repo/b:latest"},
	})
	want := []imageJob{
		{ID: "sha256:aaa", References: []string{"repo/a:latest"}},
		{ID: "sha256:bbb", References: []string{"repo/b:latest"}},
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d got=%v", len(got), got)
	}
	for i := range want {
		if got[i].ID != want[i].ID || len(got[i].References) != 1 || got[i].References[0] != want[i].References[0] {
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

func TestParseImageReference(t *testing.T) {
	tests := map[string]imageReference{
		"postgres:15-alpine": {
			Original: "postgres:15-alpine", Registry: "registry-1.docker.io", Repository: "library/postgres", Tag: "15-alpine",
		},
		"docker.io/valkey/valkey:8-alpine": {
			Original: "docker.io/valkey/valkey:8-alpine", Registry: "registry-1.docker.io", Repository: "valkey/valkey", Tag: "8-alpine",
		},
		"ghcr.io/valentinkolb/filegate:latest": {
			Original: "ghcr.io/valentinkolb/filegate:latest", Registry: "ghcr.io", Repository: "valentinkolb/filegate", Tag: "latest",
		},
		"localhost:5000/example/api:v1": {
			Original: "localhost:5000/example/api:v1", Registry: "localhost:5000", Repository: "example/api", Tag: "v1",
		},
	}
	for input, want := range tests {
		got, ok := parseImageReference(input)
		if !ok {
			t.Fatalf("%s did not parse", input)
		}
		if got != want {
			t.Fatalf("%s got=%#v want=%#v", input, got, want)
		}
	}
	for _, input := range []string{"sha256:abc", "repo@sha256:abc", "local-build"} {
		if got, ok := parseImageReference(input); ok {
			t.Fatalf("%s parsed as %#v", input, got)
		}
	}
}

func TestMatchingLocalDigest(t *testing.T) {
	digest := matchingLocalDigest(
		imageReference{Registry: "registry-1.docker.io", Repository: "library/postgres", Tag: "15-alpine"},
		[]string{"postgres@sha256:aaa", "ghcr.io/x/y@sha256:bbb"},
	)
	if digest != "sha256:aaa" {
		t.Fatalf("digest=%q", digest)
	}
	digest = matchingLocalDigest(
		imageReference{Registry: "ghcr.io", Repository: "x/y", Tag: "latest"},
		[]string{"ghcr.io/x/y@sha256:bbb"},
	)
	if digest != "sha256:bbb" {
		t.Fatalf("digest=%q", digest)
	}
}

func TestRegistryCheckableRefsSkipsLocalNamesWithoutLocalDigest(t *testing.T) {
	refs := []imageReference{
		{Original: "cloud-app:latest", Registry: "registry-1.docker.io", Repository: "library/cloud-app", Tag: "latest"},
		{Original: "postgres:15-alpine", Registry: "registry-1.docker.io", Repository: "library/postgres", Tag: "15-alpine"},
		{Original: "valkey/valkey:8-alpine", Registry: "registry-1.docker.io", Repository: "valkey/valkey", Tag: "8-alpine"},
		{Original: "ghcr.io/example/app:latest", Registry: "ghcr.io", Repository: "example/app", Tag: "latest"},
	}
	got := registryCheckableRefs(refs, []string{"postgres@sha256:aaa", "valkey/valkey@sha256:bbb"})
	if len(got) != 2 {
		t.Fatalf("got=%v", got)
	}
	if got[0].Original != "valkey/valkey:8-alpine" || got[1].Original != "ghcr.io/example/app:latest" {
		t.Fatalf("got=%v", got)
	}
}

func TestHasExplicitRegistry(t *testing.T) {
	if hasExplicitRegistry("postgres:15-alpine") {
		t.Fatal("official docker image counted as explicit registry")
	}
	if !hasExplicitRegistry("ghcr.io/example/app:latest") {
		t.Fatal("ghcr ref not counted as explicit registry")
	}
	if !hasExplicitRegistry("localhost:5000/example/app:v1") {
		t.Fatal("localhost registry not counted as explicit registry")
	}
}

func TestParseBearerChallenge(t *testing.T) {
	got, ok := parseBearerChallenge(`Bearer realm="https://auth.example/token",service="registry.example",scope="repository:x/y:pull"`)
	if !ok {
		t.Fatal("challenge did not parse")
	}
	if got["realm"] != "https://auth.example/token" || got["service"] != "registry.example" || got["scope"] != "repository:x/y:pull" {
		t.Fatalf("got=%v", got)
	}
}
