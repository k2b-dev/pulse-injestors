package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

// Collector collects lifecycle and health independently of the slower stats and
// inventory phases. All phases use logical container identities.
type Collector struct {
	SocketPath       string
	StableHostID     string
	Timeout          time.Duration
	ContainerTimeout time.Duration
	Concurrency      int
	StatsTimeout     time.Duration
	RegistryChecks   bool
	RegistryTimeout  time.Duration
}

func (c Collector) Name() string           { return "docker" }
func (c Collector) Cadence() time.Duration { return time.Minute }

// StatsCollector samples resource use without inspecting container metadata.
type StatsCollector struct{ Collector }

func (c StatsCollector) Name() string           { return "docker.stats" }
func (c StatsCollector) Cadence() time.Duration { return 5 * time.Minute }
func (c StatsCollector) CollectTimeout() time.Duration {
	if c.StatsTimeout > 0 {
		return c.StatsTimeout
	}
	return 2 * time.Minute
}

// InventoryCollector inspects only images referenced by existing containers.
type InventoryCollector struct{ Collector }

func (c InventoryCollector) Name() string           { return "docker.inventory" }
func (c InventoryCollector) Cadence() time.Duration { return 24 * time.Hour }

func (c Collector) api() apiClient {
	socket := c.SocketPath
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return apiClient{client: newClient(socket, timeout)}
}

func (c Collector) hostID(scope monitoring.Scope) string {
	if id := NormalizeStableHostID(c.StableHostID); id != "" {
		return id
	}
	return stableHostIDFromScope(scope)
}

func (c Collector) containerTimeout() time.Duration {
	if c.ContainerTimeout > 0 {
		return c.ContainerTimeout
	}
	return 10 * time.Second
}

func (c Collector) concurrency() int {
	if c.Concurrency > 0 {
		return c.Concurrency
	}
	return 4
}

func daemonScope(scope monitoring.Scope, hostID string) monitoring.Scope {
	return withResourceLabel(scope, "docker-daemon", dockerDaemonResourceID(hostID), "Docker on "+hostID)
}

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	api := c.api()
	defer api.client.CloseIdleConnections()
	hostID := c.hostID(scope)
	b := monitoring.NewBuilder(daemonScope(scope, hostID))
	containers, err := api.containers(ctx)
	if err != nil {
		b.State("docker.available", false, nil)
		b.EventDetails("docker.unavailable", map[string]string{"operation": "containers"}, monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
		return b.Batch(), nil
	}
	b.State("docker.available", true, nil)
	running := 0
	for _, container := range containers {
		if container.State == "running" {
			running++
		}
	}
	b.Metric("docker.containers.total", "gauge", float64(len(containers)), "", nil)
	b.Metric("docker.containers.running", "gauge", float64(running), "", nil)
	batch := b.Batch()
	emitContainerSummary(&batch, scope, hostID, containers)
	for _, part := range collectContainers(ctx, containers, c.concurrency(), func(container containerSummary) pulse.Batch {
		cb := monitoring.NewBuilder(containerScope(scope, hostID, container))
		dims := containerDims(container)
		cb.State("docker.container.status", container.State, dims)
		cb.State("docker.container.image", container.Image, dims)
		inspectCtx, cancel := context.WithTimeout(ctx, c.containerTimeout())
		defer cancel()
		inspect, err := api.inspect(inspectCtx, container.ID)
		if err != nil {
			cb.EventDetails("docker.container.collect.failed", map[string]string{"operation": "inspect"}, monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
		} else {
			emitInspect(cb, dims, inspect)
		}
		return cb.Batch()
	}) {
		monitoring.Merge(&batch, part)
	}
	return batch, nil
}

func (c StatsCollector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	// Also enforce the budget for callers outside Runner.
	ctx, cancel := context.WithTimeout(ctx, c.CollectTimeout())
	defer cancel()
	api := c.api()
	defer api.client.CloseIdleConnections()
	hostID := c.hostID(scope)
	containers, err := api.containers(ctx)
	if err != nil {
		b := monitoring.NewBuilder(daemonScope(scope, hostID))
		b.EventDetails("docker.collect.failed", map[string]string{"operation": "containers"}, monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
		return b.Batch(), nil
	}
	running := make([]containerSummary, 0, len(containers))
	for _, container := range containers {
		if container.State == "running" {
			running = append(running, container)
		}
	}
	var batch pulse.Batch
	for _, part := range collectContainers(ctx, running, c.concurrency(), func(container containerSummary) pulse.Batch {
		b := monitoring.NewBuilder(containerScope(scope, hostID, container))
		statsCtx, cancel := context.WithTimeout(ctx, c.containerTimeout())
		defer cancel()
		stats, err := api.stats(statsCtx, container.ID)
		if err != nil {
			b.EventDetails("docker.container.collect.failed", map[string]string{"operation": "stats"}, monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
		} else {
			emitStats(b, containerDims(container), stats)
		}
		return b.Batch()
	}) {
		monitoring.Merge(&batch, part)
	}
	return batch, ctx.Err()
}

func (c InventoryCollector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	api := c.api()
	defer api.client.CloseIdleConnections()
	hostID := c.hostID(scope)
	b := monitoring.NewBuilder(daemonScope(scope, hostID))
	version, versionErr := api.version(ctx)
	if versionErr != nil {
		b.EventDetails("docker.collect.failed", map[string]string{"operation": "version"}, monitoring.EventDetails{Attributes: map[string]any{"error": versionErr.Error()}})
	} else {
		b.State("docker.version", version.Version, nil)
	}
	containers, err := api.containers(ctx)
	if err != nil {
		b.EventDetails("docker.collect.failed", map[string]string{"operation": "containers"}, monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
		return b.Batch(), errors.Join(versionErr, err)
	}
	batch := b.Batch()
	registryTimeout := c.RegistryTimeout
	if registryTimeout <= 0 {
		registryTimeout = 10 * time.Second
	}
	for _, result := range collectImages(ctx, api, scope, hostID, containers, c.concurrency(), c.containerTimeout(), c.RegistryChecks, registryTimeout) {
		monitoring.Merge(&batch, result.batch)
	}
	return batch, errors.Join(versionErr, ctx.Err())
}

// Stop dispatching work when the phase budget is exhausted. In-flight requests
// use the same context, so no worker outlives collection.
func collectContainers(ctx context.Context, containers []containerSummary, concurrency int, collect func(containerSummary) pulse.Batch) []pulse.Batch {
	jobs := make(chan containerSummary)
	results := make(chan pulse.Batch, len(containers))
	var wg sync.WaitGroup
	for i := 0; i < min(concurrency, len(containers)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for container := range jobs {
				if ctx.Err() != nil {
					return
				}
				results <- collect(container)
			}
		}()
	}
dispatch:
	for _, container := range containers {
		select {
		case jobs <- container:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	close(results)
	out := make([]pulse.Batch, 0, len(containers))
	for batch := range results {
		out = append(out, batch)
	}
	return out
}

type containerResult struct{ batch pulse.Batch }

type imageJob struct {
	ID         string
	References []string
}

func collectImages(ctx context.Context, api apiClient, scope monitoring.Scope, stableHostID string, containers []containerSummary, concurrency int, timeout time.Duration, registryChecks bool, registryTimeout time.Duration) []containerResult {
	images := uniqueImageJobs(containers)
	if len(images) == 0 {
		return nil
	}
	jobs := make(chan imageJob)
	results := make(chan containerResult, len(images))
	workers := concurrency
	if workers > len(images) {
		workers = len(images)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for image := range jobs {
				if ctx.Err() != nil {
					return
				}
				results <- collectImage(ctx, api, scope, stableHostID, image, timeout, registryChecks, registryTimeout)
			}
		}()
	}
dispatch:
	for _, image := range images {
		select {
		case jobs <- image:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]containerResult, 0, len(images))
	for result := range results {
		out = append(out, result)
	}
	return out
}

func collectImage(ctx context.Context, api apiClient, scope monitoring.Scope, stableHostID string, image imageJob, timeout time.Duration, registryChecks bool, registryTimeout time.Duration) containerResult {
	imageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	inspect, err := api.imageInspect(imageCtx, image.ID)
	if err != nil {
		identity := imageIdentityFrom(stableHostID, image.ID, image.References, nil, nil)
		b := monitoring.NewBuilder(imageScope(scope, identity))
		dims := imageDims(identity)
		b.State("docker.image.inspect.available", false, dims)
		b.EventDetails("docker.image.collect.failed", map[string]string{"operation": "inspect"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return containerResult{batch: b.Batch()}
	}
	var batch pulse.Batch
	identities := imageIdentitiesFrom(stableHostID, image.ID, image.References, nil, inspect.RepoDigests)
	for _, identity := range identities {
		b := monitoring.NewBuilder(imageScope(scope, identity))
		dims := imageDims(identity)
		emitImageInspect(b, dims, inspect, scope.Timestamp)
		if registryChecks {
			refs := imageReferences([]string{identity.Label})
			emitRegistryChecks(ctx, b, dims, refs, inspect.RepoDigests, registryTimeout)
		}
		monitoring.Merge(&batch, b.Batch())
	}
	return containerResult{batch: batch}
}

func newClient(socket string, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

type apiClient struct {
	client *http.Client
}

func (a apiClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("docker API %s returned HTTP %d: %s", path, resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a apiClient) version(ctx context.Context) (versionResponse, error) {
	var out versionResponse
	err := a.get(ctx, "/version", &out)
	return out, err
}

func (a apiClient) info(ctx context.Context) (infoResponse, error) {
	var out infoResponse
	err := a.get(ctx, "/info", &out)
	return out, err
}

func (a apiClient) containers(ctx context.Context) ([]containerSummary, error) {
	var out []containerSummary
	err := a.get(ctx, "/containers/json?all=1", &out)
	return out, err
}

func (a apiClient) stats(ctx context.Context, id string) (statsResponse, error) {
	var out statsResponse
	err := a.get(ctx, "/containers/"+id+"/stats?stream=false", &out)
	return out, err
}

func (a apiClient) inspect(ctx context.Context, id string) (inspectResponse, error) {
	var out inspectResponse
	err := a.get(ctx, "/containers/"+id+"/json", &out)
	return out, err
}

func (a apiClient) imageInspect(ctx context.Context, id string) (imageInspectResponse, error) {
	var out imageInspectResponse
	err := a.get(ctx, "/images/"+url.PathEscape(id)+"/json", &out)
	return out, err
}

type versionResponse struct {
	Version string `json:"Version"`
}

type infoResponse struct {
	ID   string `json:"ID"`
	Name string `json:"Name"`
}

type containerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	State   string            `json:"State"`
	Status  string            `json:"Status"`
	Labels  map[string]string `json:"Labels"`
}

type inspectResponse struct {
	RestartCount int `json:"RestartCount"`
	State        struct {
		Health *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
}

type imageInspectResponse struct {
	ID           string   `json:"Id"`
	Created      string   `json:"Created"`
	RepoTags     []string `json:"RepoTags"`
	RepoDigests  []string `json:"RepoDigests"`
	Size         int64    `json:"Size"`
	VirtualSize  int64    `json:"VirtualSize"`
	Architecture string   `json:"Architecture"`
	OS           string   `json:"Os"`
}

type statsResponse struct {
	CPUStats struct {
		OnlineCPUs uint64 `json:"online_cpus"`
		CPUUsage   struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IoServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

func containerDims(c containerSummary) map[string]string {
	dims := map[string]string{
		"container": containerLabel(c),
	}
	addComposeDims(dims, c.Labels)
	return dims
}

func DaemonStableID(ctx context.Context, socket string, timeout time.Duration) (string, error) {
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	api := apiClient{client: newClient(socket, timeout)}
	info, err := api.info(ctx)
	if err != nil {
		return "", err
	}
	if name := NormalizeStableHostID(info.Name); name != "" {
		return name, nil
	}
	if id := NormalizeStableHostID(info.ID); id != "" {
		return id, nil
	}
	return "", fmt.Errorf("docker daemon did not report an ID or name")
}

func NormalizeStableHostID(value string) string { return strings.TrimSpace(value) }

func stableHostIDFromScope(scope monitoring.Scope) string {
	if scope.Dimensions != nil {
		if host := NormalizeStableHostID(scope.Dimensions["host"]); host != "" {
			return host
		}
	}
	return NormalizeStableHostID(scope.Resource.ID)
}

func dockerDaemonResourceID(stableHostID string) string {
	return entityComponent(stableHostID)
}

func containerResourceID(stableHostID string, container containerSummary) string {
	project := container.Labels["com.docker.compose.project"]
	service := container.Labels["com.docker.compose.service"]
	number := container.Labels["com.docker.compose.container-number"]
	if project != "" && service != "" && number != "" {
		return entityComponent(stableHostID) + ":compose:" + entityComponent(project) + ":" + entityComponent(service) + ":" + entityComponent(number)
	}
	if name := containerName(container); name != "" {
		if project != "" && service != "" {
			return entityComponent(stableHostID) + ":compose:" + entityComponent(project) + ":" + entityComponent(service) + ":name:" + entityComponent(name)
		}
		return entityComponent(stableHostID) + ":name:" + entityComponent(name)
	}
	return entityComponent(stableHostID) + ":id:" + entityComponent(shortID(container.ID))
}

func composeServiceResourceID(stableHostID, project, service string) string {
	return entityComponent(stableHostID) + ":" + entityComponent(project) + ":" + entityComponent(service)
}

func composeProjectResourceID(stableHostID, project string) string {
	return entityComponent(stableHostID) + ":" + entityComponent(project)
}

func imageResourceID(identity imageIdentity) string {
	return identity.ResourceID
}

func entityComponent(value string) string { return entity.ID(value) }

func withResource(scope monitoring.Scope, resourceType, resourceID string) monitoring.Scope {
	scope.Resource.Type = resourceType
	scope.Resource.ID = resourceID
	return scope
}

func withResourceLabel(scope monitoring.Scope, resourceType, resourceID, label string) monitoring.Scope {
	scope = withResource(scope, resourceType, resourceID)
	scope.Resource.Label = label
	return scope
}

func containerScope(scope monitoring.Scope, stableHostID string, container containerSummary) monitoring.Scope {
	label := containerLabel(container)
	return withResourceLabel(scope, "docker-container", containerResourceID(stableHostID, container), label)
}

func composeServiceScope(scope monitoring.Scope, stableHostID, project, service string) monitoring.Scope {
	return withResourceLabel(scope, "docker-compose-service", composeServiceResourceID(stableHostID, project, service), project+"/"+service)
}

func composeProjectScope(scope monitoring.Scope, stableHostID, project string) monitoring.Scope {
	return withResourceLabel(scope, "docker-compose-project", composeProjectResourceID(stableHostID, project), project)
}

func imageScope(scope monitoring.Scope, identity imageIdentity) monitoring.Scope {
	return withResourceLabel(scope, "docker-image", imageResourceID(identity), identity.Label)
}

func addComposeDims(dims map[string]string, labels map[string]string) {
	if labels == nil {
		return
	}
	if v := labels["com.docker.compose.project"]; v != "" {
		dims["compose_project"] = v
	}
	if v := labels["com.docker.compose.service"]; v != "" {
		dims["compose_service"] = v
	}
	if v := labels["com.docker.compose.container-number"]; v != "" {
		dims["compose_container_number"] = v
	}
}

func containerName(c containerSummary) string {
	for _, name := range c.Names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name != "" {
			return name
		}
	}
	return ""
}

func containerLabel(c containerSummary) string {
	if name := containerName(c); name != "" {
		return name
	}
	if service := c.Labels["com.docker.compose.service"]; service != "" {
		return service
	}
	return shortID(c.ID)
}

func emitStats(b *monitoring.Builder, dims map[string]string, stats statsResponse) {
	online := stats.CPUStats.OnlineCPUs
	if online == 0 {
		online = uint64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	// Counter resets must not underflow into enormous CPU percentages.
	if stats.CPUStats.CPUUsage.TotalUsage >= stats.PreCPUStats.CPUUsage.TotalUsage && stats.CPUStats.SystemUsage > stats.PreCPUStats.SystemUsage && online > 0 {
		cpuDelta := stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage
		systemDelta := stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage
		b.Metric("docker.container.cpu.usage", "gauge", float64(cpuDelta)/float64(systemDelta)*float64(online)*100, "percent", dims)
	}
	usage := dockerMemoryUsage(stats.MemoryStats.Usage, stats.MemoryStats.Stats)
	b.Metric("docker.container.memory.used", "gauge", float64(usage), "bytes", dims)
	b.Metric("docker.container.memory.limit", "gauge", float64(stats.MemoryStats.Limit), "bytes", dims)
	var rx, tx uint64
	for _, network := range stats.Networks {
		rx += network.RxBytes
		tx += network.TxBytes
	}
	b.Metric("docker.container.network.rx", "counter", float64(rx), "bytes", dims)
	b.Metric("docker.container.network.tx", "counter", float64(tx), "bytes", dims)
	read, write := dockerBlockIO(stats)
	b.Metric("docker.container.blockio.read", "counter", float64(read), "bytes", dims)
	b.Metric("docker.container.blockio.write", "counter", float64(write), "bytes", dims)
}

func emitContainerSummary(batch *pulse.Batch, scope monitoring.Scope, stableHostID string, containers []containerSummary) {
	type counts struct{ total, running int }
	projects := map[string]counts{}
	services := map[string]counts{}
	for _, container := range containers {
		project := container.Labels["com.docker.compose.project"]
		service := container.Labels["com.docker.compose.service"]
		if project == "" {
			continue
		}
		p := projects[project]
		p.total++
		if container.State == "running" {
			p.running++
		}
		projects[project] = p
		if service != "" {
			key := project + "\x00" + service
			s := services[key]
			s.total++
			if container.State == "running" {
				s.running++
			}
			services[key] = s
		}
	}
	for project, count := range projects {
		b := monitoring.NewBuilder(composeProjectScope(scope, stableHostID, project))
		dims := map[string]string{"compose_project": project}
		b.State("docker.compose.project.containers", fmt.Sprintf("%d/%d", count.running, count.total), dims)
		monitoring.Merge(batch, b.Batch())
	}
	for key, count := range services {
		project, service, _ := strings.Cut(key, "\x00")
		b := monitoring.NewBuilder(composeServiceScope(scope, stableHostID, project, service))
		dims := map[string]string{"compose_project": project, "compose_service": service}
		b.State("docker.compose.service.containers", fmt.Sprintf("%d/%d", count.running, count.total), dims)
		monitoring.Merge(batch, b.Batch())
	}
}

func dockerMemoryUsage(usage uint64, stats map[string]uint64) uint64 {
	cache := stats["inactive_file"]
	if cache == 0 {
		cache = stats["cache"]
	}
	if cache > 0 && usage >= cache {
		usage -= cache
	}
	return usage
}

func dockerBlockIO(stats statsResponse) (read, write uint64) {
	for _, row := range stats.BlkioStats.IoServiceBytesRecursive {
		op := strings.ToLower(row.Op)
		switch op {
		case "read":
			read += row.Value
		case "write":
			write += row.Value
		}
	}
	return read, write
}

func emitInspect(b *monitoring.Builder, dims map[string]string, inspect inspectResponse) {
	b.Metric("docker.container.restarts", "counter", float64(inspect.RestartCount), "", dims)
	health := "none"
	if inspect.State.Health != nil {
		switch inspect.State.Health.Status {
		case "starting", "healthy", "unhealthy":
			health = inspect.State.Health.Status
		}
	}
	b.State("docker.container.health", health, dims)
}

func emitImageInspect(b *monitoring.Builder, dims map[string]string, inspect imageInspectResponse, now time.Time) {
	b.State("docker.image.inspect.available", true, dims)
	if inspect.Created != "" {
		b.State("docker.image.created_at", inspect.Created, dims)
		if createdAt, ok := parseDockerTime(inspect.Created); ok {
			b.Metric("docker.image.age", "gauge", max(0, now.Sub(createdAt).Seconds()), "seconds", dims)
		}
	}
	if len(inspect.RepoTags) > 0 {
		b.State("docker.image.repo_tags", strings.Join(inspect.RepoTags, ","), dims)
		b.Metric("docker.image.repo_tags.count", "gauge", float64(len(inspect.RepoTags)), "", dims)
	}
	if len(inspect.RepoDigests) > 0 {
		b.State("docker.image.repo_digests", strings.Join(inspect.RepoDigests, ","), dims)
		b.Metric("docker.image.repo_digests.count", "gauge", float64(len(inspect.RepoDigests)), "", dims)
	}
	if inspect.Size > 0 {
		b.Metric("docker.image.size", "gauge", float64(inspect.Size), "bytes", dims)
	}
	if inspect.VirtualSize > 0 {
		b.Metric("docker.image.virtual_size", "gauge", float64(inspect.VirtualSize), "bytes", dims)
	}
	if inspect.Architecture != "" {
		b.State("docker.image.arch", inspect.Architecture, dims)
	}
	if inspect.OS != "" {
		b.State("docker.image.os", inspect.OS, dims)
	}
}

func uniqueImageJobs(containers []containerSummary) []imageJob {
	// A tag can refer to an old stopped container and its replacement. Emit one
	// logical image series, preferring running containers, with a stable tie break.
	containers = append([]containerSummary(nil), containers...)
	sort.Slice(containers, func(i, j int) bool {
		runningI, runningJ := containers[i].State == "running", containers[j].State == "running"
		if runningI != runningJ {
			return runningI
		}
		return containers[i].ImageID < containers[j].ImageID
	})
	seen := map[string]map[string]bool{}
	references := map[string]bool{}
	for _, container := range containers {
		id := container.ImageID
		if id == "" {
			continue
		}
		key := container.Image
		if ref, ok := parseImageReference(key); ok {
			key = ref.Registry + "/" + ref.Repository + ":" + ref.Tag
		}
		if key == "" {
			key = id
		}
		if references[key] {
			continue
		}
		references[key] = true
		if seen[id] == nil {
			seen[id] = map[string]bool{}
		}
		if container.Image != "" {
			seen[id][container.Image] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]imageJob, 0, len(ids))
	for _, id := range ids {
		refs := make([]string, 0, len(seen[id]))
		for ref := range seen[id] {
			refs = append(refs, ref)
		}
		sort.Strings(refs)
		out = append(out, imageJob{ID: id, References: refs})
	}
	return out
}

type imageIdentity struct {
	ResourceID string
	Label      string
	ImageID    string
	Repository string
	Tag        string
	Digest     string
}

func imageIdentityFrom(stableHostID, imageID string, references, repoTags, repoDigests []string) imageIdentity {
	for _, ref := range imageReferences(references, repoTags) {
		repository := ref.Repository
		if ref.Registry != "registry-1.docker.io" {
			repository = ref.Registry + "/" + ref.Repository
		}
		return imageIdentity{
			ResourceID: entityComponent(stableHostID) + ":" + entityComponent(repository) + ":" + entityComponent(ref.Tag),
			Label:      ref.Original,
			ImageID:    shortImageID(imageID),
			Repository: repository,
			Tag:        ref.Tag,
			Digest:     matchingLocalDigest(ref, repoDigests),
		}
	}
	for _, value := range append(copyStrings(references), repoDigests...) {
		repository, digest, ok := parseRepositoryDigest(value)
		if !ok {
			continue
		}
		shortDigest := shortDigest(digest)
		return imageIdentity{
			ResourceID: entityComponent(stableHostID) + ":" + entityComponent(repository) + ":" + entityComponent(shortDigest),
			Label:      repository + "@" + shortDigest,
			ImageID:    shortImageID(imageID),
			Repository: repository,
			Digest:     digest,
		}
	}
	short := shortImageID(imageID)
	if short == "" {
		short = "unknown"
	}
	return imageIdentity{
		ResourceID: entityComponent(stableHostID) + ":" + entityComponent(short),
		Label:      short,
		ImageID:    shortImageID(imageID),
	}
}

func imageIdentitiesFrom(stableHostID, imageID string, references, repoTags, repoDigests []string) []imageIdentity {
	refs := imageReferences(references, repoTags)
	if len(refs) == 0 {
		return []imageIdentity{imageIdentityFrom(stableHostID, imageID, references, repoTags, repoDigests)}
	}
	out := make([]imageIdentity, 0, len(refs))
	for _, ref := range refs {
		out = append(out, imageIdentityFrom(stableHostID, imageID, []string{ref.Original}, nil, repoDigests))
	}
	return out
}

func imageDims(identity imageIdentity) map[string]string {
	dims := map[string]string{
		"image":      identity.Label,
		"repository": identity.Repository,
		"tag":        identity.Tag,
	}
	return compactDims(dims)
}

func parseRepositoryDigest(value string) (string, string, bool) {
	value = strings.TrimSpace(value)
	repository, digest, ok := strings.Cut(value, "@")
	if !ok || repository == "" || digest == "" {
		return "", "", false
	}
	return repository, digest, true
}

func shortDigest(digest string) string {
	if alg, value, ok := strings.Cut(digest, ":"); ok {
		if len(value) > 12 {
			value = value[:12]
		}
		return alg + ":" + value
	}
	if len(digest) > 12 {
		return digest[:12]
	}
	return digest
}

func copyStrings(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func parseDockerTime(value string) (time.Time, bool) {
	if value == "" || strings.HasPrefix(value, "0001-01-01") {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

type imageReference struct {
	Original   string
	Registry   string
	Repository string
	Tag        string
}

func imageReferences(values ...[]string) []imageReference {
	seen := map[string]bool{}
	var out []imageReference
	for _, list := range values {
		for _, value := range list {
			ref, ok := parseImageReference(value)
			if !ok {
				continue
			}
			key := ref.Registry + "/" + ref.Repository + ":" + ref.Tag
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ref)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Original < out[j].Original
	})
	return out
}

func parseImageReference(value string) (imageReference, bool) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "@") || strings.HasPrefix(value, "sha256:") || strings.HasPrefix(value, "<none>") {
		return imageReference{}, false
	}
	lastSlash := strings.LastIndex(value, "/")
	lastColon := strings.LastIndex(value, ":")
	if lastColon <= lastSlash || lastColon == len(value)-1 {
		return imageReference{}, false
	}
	name := value[:lastColon]
	tag := value[lastColon+1:]
	parts := strings.Split(name, "/")
	registry := "registry-1.docker.io"
	repository := name
	if len(parts) > 1 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost") {
		registry = parts[0]
		repository = strings.Join(parts[1:], "/")
	} else if len(parts) == 1 {
		repository = "library/" + name
	}
	if registry == "docker.io" {
		registry = "registry-1.docker.io"
	}
	if repository == "" {
		return imageReference{}, false
	}
	return imageReference{Original: value, Registry: registry, Repository: repository, Tag: tag}, true
}

func emitRegistryChecks(ctx context.Context, b *monitoring.Builder, baseDims map[string]string, refs []imageReference, localDigests []string, timeout time.Duration) {
	refs = registryCheckableRefs(refs, localDigests)
	if len(refs) == 0 {
		b.State("docker.image.registry.checkable", false, baseDims)
		return
	}
	client := registryClient{client: &http.Client{Timeout: timeout}}
	for _, ref := range refs {
		dims := copyDims(baseDims)
		dims["registry"] = ref.Registry
		dims["repository"] = ref.Repository
		dims["tag"] = ref.Tag
		b.State("docker.image.registry.checkable", true, dims)
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		remoteDigest, err := client.manifestDigest(runCtx, ref)
		cancel()
		if err != nil {
			b.State("docker.image.registry.checked", false, dims)
			b.EventDetails("docker.image.registry.check.failed", map[string]string{
				"operation":  "manifest",
				"registry":   ref.Registry,
				"repository": ref.Repository,
				"tag":        ref.Tag,
			}, monitoring.EventDetails{Attributes: map[string]any{"error": err.Error()}})
			continue
		}
		localDigest := matchingLocalDigest(ref, localDigests)
		b.State("docker.image.registry.checked", true, dims)
		b.State("docker.image.registry.remote_digest", remoteDigest, dims)
		b.State("docker.image.registry.local_digest_available", localDigest != "", dims)
		if localDigest != "" {
			b.State("docker.image.registry.local_digest", localDigest, dims)
			b.State("docker.image.update_available", localDigest != remoteDigest, dims)
		}
	}
}

func registryCheckableRefs(refs []imageReference, localDigests []string) []imageReference {
	out := make([]imageReference, 0, len(refs))
	for _, ref := range refs {
		if isImplicitDockerHubReference(ref) {
			continue
		}
		if hasExplicitRegistry(ref.Original) || matchingLocalDigest(ref, localDigests) != "" {
			out = append(out, ref)
		}
	}
	return out
}

func isImplicitDockerHubReference(ref imageReference) bool {
	return ref.Registry == "registry-1.docker.io" && !strings.Contains(ref.Original, "/")
}

func hasExplicitRegistry(value string) bool {
	name := value
	if i := strings.LastIndex(value, ":"); i > strings.LastIndex(value, "/") {
		name = value[:i]
	}
	first, _, ok := strings.Cut(name, "/")
	return ok && (strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost")
}

func matchingLocalDigest(ref imageReference, localDigests []string) string {
	for _, value := range localDigests {
		repo, digest, ok := strings.Cut(value, "@")
		if !ok || digest == "" {
			continue
		}
		if sameRepository(ref, repo) {
			return digest
		}
	}
	return ""
}

func sameRepository(ref imageReference, repo string) bool {
	repo = strings.TrimPrefix(repo, "https://")
	repo = strings.TrimPrefix(repo, "http://")
	if strings.HasPrefix(repo, "docker.io/") {
		repo = strings.TrimPrefix(repo, "docker.io/")
	}
	if strings.HasPrefix(repo, "registry-1.docker.io/") {
		repo = strings.TrimPrefix(repo, "registry-1.docker.io/")
	}
	if ref.Registry == "registry-1.docker.io" {
		if repo == ref.Repository {
			return true
		}
		return repo == strings.TrimPrefix(ref.Repository, "library/")
	}
	return repo == ref.Registry+"/"+ref.Repository || repo == ref.Repository
}

type registryClient struct {
	client *http.Client
}

func (c registryClient) manifestDigest(ctx context.Context, ref imageReference) (string, error) {
	req, err := registryRequest(ctx, ref, "")
	if err != nil {
		return "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		token, err := c.bearerToken(ctx, resp.Header.Get("WWW-Authenticate"))
		if err != nil {
			return "", err
		}
		req, err := registryRequest(ctx, ref, token)
		if err != nil {
			return "", err
		}
		resp, err = c.client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("registry manifest returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if digest := resp.Header.Get("Docker-Content-Digest"); digest != "" {
		return digest, nil
	}
	return "", fmt.Errorf("registry manifest missing Docker-Content-Digest")
}

func registryRequest(ctx context.Context, ref imageReference, token string) (*http.Request, error) {
	u := url.URL{
		Scheme: "https",
		Host:   ref.Registry,
		Path:   "/v2/" + ref.Repository + "/manifests/" + ref.Tag,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ", "))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req, nil
}

func (c registryClient) bearerToken(ctx context.Context, challenge string) (string, error) {
	params, ok := parseBearerChallenge(challenge)
	if !ok {
		return "", fmt.Errorf("registry auth challenge is not bearer")
	}
	realm := params["realm"]
	if realm == "" {
		return "", fmt.Errorf("registry auth challenge missing realm")
	}
	u, err := url.Parse(realm)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for _, key := range []string{"service", "scope"} {
		if params[key] != "" {
			q.Set(key, params[key])
		}
	}
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("registry token returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var parsed struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return "", err
	}
	if parsed.Token != "" {
		return parsed.Token, nil
	}
	if parsed.AccessToken != "" {
		return parsed.AccessToken, nil
	}
	return "", fmt.Errorf("registry token response missing token")
}

func parseBearerChallenge(challenge string) (map[string]string, bool) {
	challenge = strings.TrimSpace(challenge)
	if !strings.HasPrefix(strings.ToLower(challenge), "bearer ") {
		return nil, false
	}
	rest := strings.TrimSpace(challenge[len("Bearer "):])
	out := map[string]string{}
	for _, part := range splitAuthParts(rest) {
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return out, true
}

func splitAuthParts(value string) []string {
	var parts []string
	start := 0
	inQuote := false
	for i, r := range value {
		switch r {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				parts = append(parts, value[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, value[start:])
	return parts
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func shortImageID(id string) string {
	return shortID(strings.TrimPrefix(id, "sha256:"))
}

func copyDims(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func compactDims(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		if value != "" {
			out[key] = value
		}
	}
	return out
}
