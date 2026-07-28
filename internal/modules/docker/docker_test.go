package docker

import (
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

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

func TestContainerDimsIncludesReadableAndFilterableFacets(t *testing.T) {
	dims := containerDims(containerSummary{
		ID:    "1234567890abcdef",
		Names: []string{"/api-1"},
		Image: "example/api:latest",
		Labels: map[string]string{
			"com.docker.compose.project":          "pulse",
			"com.docker.compose.service":          "api",
			"com.docker.compose.container-number": "1",
		},
	})
	if dims["container"] != "api-1" {
		t.Fatalf("container dim = %q", dims["container"])
	}
	if dims["compose_project"] != "pulse" || dims["compose_service"] != "api" {
		t.Fatalf("compose dims = %v", dims)
	}
	if dims["compose_container_number"] != "1" {
		t.Fatalf("compose container number dim = %v", dims)
	}
	if _, ok := dims["container_id"]; ok {
		t.Fatalf("container_id must be a state, dimensions = %v", dims)
	}
	if _, ok := dims["image"]; ok {
		t.Fatalf("image must be a state, dimensions = %v", dims)
	}
}

func TestDockerEntityIDs(t *testing.T) {
	if got := dockerDaemonEntityID("server-01"); got != "docker:server-01" {
		t.Fatalf("daemon entity = %q", got)
	}
	composeContainer := containerSummary{
		ID:    "1234567890abcdef",
		Names: []string{"/pulse-api-1"},
		Labels: map[string]string{
			"com.docker.compose.project":          "pulse",
			"com.docker.compose.service":          "api",
			"com.docker.compose.container-number": "1",
		},
	}
	if got := containerEntityID("server-01", composeContainer); got != "container:server-01:compose:pulse:api:1" {
		t.Fatalf("compose container entity = %q", got)
	}
	namedContainer := containerSummary{ID: "abcdef1234567890", Names: []string{"/postgres"}}
	if got := containerEntityID("server-01", namedContainer); got != "container:server-01:name:postgres" {
		t.Fatalf("named container entity = %q", got)
	}
	unnamedContainer := containerSummary{ID: "fedcba0987654321"}
	if got := containerEntityID("server-01", unnamedContainer); got != "container:server-01:id:fedcba098765" {
		t.Fatalf("fallback container entity = %q", got)
	}
	if got := composeServiceEntityID("server-01", "my:project", "api/web"); got != "compose:server-01:my_project:api_web" {
		t.Fatalf("compose service entity = %q", got)
	}
	identity := imageIdentityFrom("server-01", "sha256:abcdef1234567890", []string{"ghcr.io/example/api:latest"}, nil, nil)
	if got := imageEntityID(identity); got != "image:server-01:ghcr.io_example_api:latest" {
		t.Fatalf("image entity = %q", got)
	}
	if identity.Label != "ghcr.io/example/api:latest" {
		t.Fatalf("image label = %q", identity.Label)
	}
	if got := composeProjectEntityID("server-01", "my:project"); got != "compose-project:server-01:my_project" {
		t.Fatalf("compose project entity = %q", got)
	}
}

func TestEmitImageInspectUsesImageResourceEntity(t *testing.T) {
	scope := monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{"host": "server-01", "collector": "docker"},
		Timestamp:  time.Unix(0, 0).UTC(),
	}
	identity := imageIdentityFrom("server-01", "sha256:abcdef1234567890", nil, []string{"ghcr.io/example/api:latest"}, nil)
	b := monitoring.NewBuilder(imageScope(scope, identity))
	emitImageInspect(b, imageDims(identity), imageInspectResponse{
		ID:           "sha256:abcdef1234567890",
		Architecture: "arm64",
		OS:           "linux",
	})
	batch := b.Batch()
	state := findState(batch.States, "docker.image.arch")
	if state == nil {
		t.Fatalf("missing image arch state: %#v", batch.States)
	}
	if state.EntityType != "docker-image" || state.EntityID != "docker-image:server-01:ghcr.io_example_api:latest" {
		t.Fatalf("image state entity = %s %s", state.EntityType, state.EntityID)
	}
	if state.Resource == nil || state.Resource.Label != "ghcr.io/example/api:latest" {
		t.Fatalf("image resource = %#v", state.Resource)
	}
	if _, ok := state.Dimensions["image_id"]; ok {
		t.Fatalf("image_id must be a state, dimensions = %v", state.Dimensions)
	}
	if state.Dimensions["repository"] != "ghcr.io/example/api" || state.Dimensions["tag"] != "latest" {
		t.Fatalf("image reference dims = %v", state.Dimensions)
	}
}

func TestEmitMountsUsesContainerResourceWithMountDimensions(t *testing.T) {
	scope := monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{"host": "server-01", "collector": "docker"},
		Timestamp:  time.Unix(0, 0).UTC(),
	}
	container := containerSummary{
		ID:    "1234567890abcdef",
		Names: []string{"/pulse-db-1"},
		Labels: map[string]string{
			"com.docker.compose.project":          "pulse",
			"com.docker.compose.service":          "db",
			"com.docker.compose.container-number": "1",
		},
	}
	mount := dockerMount{Type: "volume", Name: "pulse_db", Destination: "/var/lib/postgresql/data", Driver: "local", RW: true}
	b := monitoring.NewBuilder(containerScope(scope, "server-01", container))
	emitMounts(b, container, containerDims(container), "/", []dockerMount{mount})
	batch := b.Batch()
	state := findState(batch.States, "docker.container.mount.rw")
	if state == nil {
		t.Fatalf("missing mount state: %#v", batch.States)
	}
	if state.EntityType != "docker-container" || state.EntityID != "docker-container:server-01:compose:pulse:db:1" {
		t.Fatalf("mount state entity = %s %s", state.EntityType, state.EntityID)
	}
	if state.Dimensions["mount_destination"] != "/var/lib/postgresql/data" || state.Dimensions["volume"] != "pulse_db" {
		t.Fatalf("mount dims = %v", state.Dimensions)
	}
	if _, ok := state.Dimensions["container_id"]; ok {
		t.Fatalf("mount dims contain runtime container id: %v", state.Dimensions)
	}
	if state.Resource == nil || state.Resource.Label != "pulse-db-1" {
		t.Fatalf("mount resource = %#v", state.Resource)
	}
}

func TestEmitNetworksUsesContainerResourceWithNetworkDimensions(t *testing.T) {
	scope := monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{"host": "server-01", "collector": "docker"},
		Timestamp:  time.Unix(0, 0).UTC(),
	}
	container := containerSummary{ID: "1234567890abcdef", Names: []string{"/api"}}
	var inspect inspectResponse
	inspect.NetworkSettings.Networks = map[string]struct {
		NetworkID   string   `json:"NetworkID"`
		EndpointID  string   `json:"EndpointID"`
		Gateway     string   `json:"Gateway"`
		IPAddress   string   `json:"IPAddress"`
		IPPrefixLen int      `json:"IPPrefixLen"`
		MacAddress  string   `json:"MacAddress"`
		Aliases     []string `json:"Aliases"`
	}{
		"pulse_default": {
			NetworkID:   "network-1",
			EndpointID:  "endpoint-1",
			IPAddress:   "172.18.0.5",
			IPPrefixLen: 16,
		},
	}
	b := monitoring.NewBuilder(containerScope(scope, "server-01", container))
	emitNetworks(b, container, inspect)
	batch := b.Batch()
	state := findState(batch.States, "docker.container.network.connected")
	if state == nil {
		t.Fatalf("missing network state: %#v", batch.States)
	}
	if state.EntityType != "docker-container" || state.EntityID != "docker-container:server-01:name:api" {
		t.Fatalf("network state entity = %s %s", state.EntityType, state.EntityID)
	}
	if state.Dimensions["network"] != "pulse_default" {
		t.Fatalf("network dims = %v", state.Dimensions)
	}
	if state.Resource == nil || state.Resource.Label != "api" {
		t.Fatalf("network resource = %#v", state.Resource)
	}
}

func TestEmitContainerSummaryUsesResourceEntities(t *testing.T) {
	scope := monitoring.Scope{
		EntityID:   "host:server-01",
		EntityType: "host",
		Dimensions: map[string]string{"host": "server-01"},
		Timestamp:  time.Unix(0, 0).UTC(),
	}
	batch := pulse.Batch{}
	emitContainerSummary(&batch, scope, "server-01", []containerSummary{
		{
			ID:    "1234567890abcdef",
			State: "running",
			Labels: map[string]string{
				"com.docker.compose.project": "pulse",
				"com.docker.compose.service": "api",
			},
		},
		{
			ID:    "abcdef1234567890",
			State: "exited",
			Labels: map[string]string{
				"com.docker.compose.project": "pulse",
				"com.docker.compose.service": "api",
			},
		},
	})

	if countMetricEntity(batch.Metrics, "docker.compose.project.containers", "docker-compose-project", "docker-compose-project:server-01:pulse") != 1 {
		t.Fatalf("missing project metric: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "docker.compose.service.containers", "docker-compose-service", "docker-compose-service:server-01:pulse:api") != 1 {
		t.Fatalf("missing service count metric: %#v", batch.Metrics)
	}
	if countMetricEntity(batch.Metrics, "docker.compose.service.containers.running", "docker-compose-service", "docker-compose-service:server-01:pulse:api") != 1 {
		t.Fatalf("missing service running metric: %#v", batch.Metrics)
	}
	if countStateEntity(batch.States, "docker.compose.service.present", "docker-compose-service", "docker-compose-service:server-01:pulse:api") != 1 {
		t.Fatalf("missing service state: %#v", batch.States)
	}
	state := findState(batch.States, "docker.compose.service.present")
	if state == nil || state.Resource == nil || state.Resource.Label != "pulse/api" {
		t.Fatalf("service label = %#v", state)
	}
}

func TestImageIdentityPrefersTagThenDigestThenImageID(t *testing.T) {
	tag := imageIdentityFrom("server-01", "sha256:abcdef1234567890", []string{"postgres:15-alpine"}, nil, nil)
	if tag.Label != "postgres:15-alpine" || tag.EntityID != "image:server-01:library_postgres:15-alpine" {
		t.Fatalf("tag identity = %#v", tag)
	}
	if tag.Repository != "library/postgres" || tag.Tag != "15-alpine" {
		t.Fatalf("tag dims source = %#v", tag)
	}
	digest := imageIdentityFrom("server-01", "sha256:abcdef1234567890", nil, nil, []string{"ghcr.io/example/api@sha256:1234567890abcdef"})
	if digest.Label != "ghcr.io/example/api@sha256:1234567890ab" || digest.EntityID != "image:server-01:ghcr.io_example_api:sha256_1234567890ab" {
		t.Fatalf("digest identity = %#v", digest)
	}
	fallback := imageIdentityFrom("server-01", "sha256:abcdef1234567890", nil, nil, nil)
	if fallback.Label != "abcdef123456" || fallback.EntityID != "image:server-01:abcdef123456" {
		t.Fatalf("fallback identity = %#v", fallback)
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

func countMetricEntity(metrics []pulse.Metric, name, entityType, entityID string) int {
	count := 0
	for _, metric := range metrics {
		if metric.Name == name && metric.EntityType == entityType && metric.EntityID == entityID {
			count++
		}
	}
	return count
}

func countStateEntity(states []pulse.State, key, entityType, entityID string) int {
	count := 0
	for _, state := range states {
		if state.Key == key && state.EntityType == entityType && state.EntityID == entityID {
			count++
		}
	}
	return count
}

func findState(states []pulse.State, key string) *pulse.State {
	for i := range states {
		if states[i].Key == key {
			return &states[i]
		}
	}
	return nil
}
