package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	SocketPath       string
	HostRoot         string
	StableHostID     string
	Timeout          time.Duration
	ContainerTimeout time.Duration
	Concurrency      int
	RegistryChecks   bool
	RegistryTimeout  time.Duration
}

func (c Collector) Name() string { return "docker" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	socket := c.SocketPath
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	hostRoot := c.HostRoot
	if hostRoot == "" {
		hostRoot = "/host/root"
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	containerTimeout := c.ContainerTimeout
	if containerTimeout <= 0 {
		containerTimeout = timeout
	}
	concurrency := c.Concurrency
	if concurrency <= 0 {
		concurrency = 4
	}
	registryTimeout := c.RegistryTimeout
	if registryTimeout <= 0 {
		registryTimeout = 10 * time.Second
	}
	client := newClient(socket, timeout)
	api := apiClient{client: client}
	stableHostID := NormalizeStableHostID(c.StableHostID)
	if stableHostID == "" {
		stableHostID = stableHostIDFromScope(scope)
	}
	daemonScope := withEntityLabel(scope, "docker-daemon", dockerDaemonEntityID(stableHostID), "Docker on "+stableHostID)
	b := monitoring.NewBuilder(daemonScope)

	version, err := api.version(ctx)
	if err != nil {
		b.State("docker.available", false, nil)
		b.EventDetails("docker.unavailable", map[string]string{"operation": "version"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return b.Batch(), nil
	}
	b.State("docker.available", true, nil)
	b.State("docker.version", version.Version, nil)
	b.State("docker.os", version.OperatingSystem, nil)
	b.State("docker.arch", version.Architecture, nil)

	containers, err := api.containers(ctx)
	if err != nil {
		b.EventDetails("docker.collect.failed", map[string]string{"operation": "containers"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return b.Batch(), nil
	}
	running := 0
	for _, container := range containers {
		if container.State == "running" {
			running++
		}
	}
	b.Metric("docker.containers.total", "gauge", float64(len(containers)), "", nil)
	b.Metric("docker.containers.running", "gauge", float64(running), "", nil)
	final := b.Batch()
	emitContainerSummary(&final, scope, stableHostID, containers)

	var jobs []containerSummary
	for _, container := range containers {
		cb := monitoring.NewBuilder(containerScope(scope, stableHostID, container))
		dims := containerDims(container)
		cb.State("docker.container.running", container.State == "running", dims)
		cb.State("docker.container.status", container.Status, dims)
		cb.State("docker.container.image", container.Image, dims)
		cb.State("docker.container.runtime.id", container.ID, dims)
		monitoring.Merge(&final, cb.Batch())
		if container.State != "running" {
			continue
		}
		jobs = append(jobs, container)
	}
	results := collectContainers(ctx, api, scope, stableHostID, hostRoot, jobs, concurrency, containerTimeout)
	for _, result := range results {
		monitoring.Merge(&final, result.batch)
	}
	emitComposeUsageSummary(&final, scope, stableHostID)
	imageResults := collectImages(ctx, api, scope, stableHostID, containers, concurrency, containerTimeout, c.RegistryChecks, registryTimeout)
	for _, result := range imageResults {
		monitoring.Merge(&final, result.batch)
	}
	emitDockerHealthSummary(&final, daemonScope)
	return final, nil
}

type containerResult struct {
	batch pulse.Batch
}

func collectContainers(ctx context.Context, api apiClient, scope monitoring.Scope, stableHostID string, hostRoot string, containers []containerSummary, concurrency int, timeout time.Duration) []containerResult {
	if len(containers) == 0 {
		return nil
	}
	jobs := make(chan containerSummary)
	results := make(chan containerResult, len(containers))
	workers := concurrency
	if workers > len(containers) {
		workers = len(containers)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for container := range jobs {
				results <- collectContainer(ctx, api, scope, stableHostID, hostRoot, container, timeout)
			}
		}()
	}
	for _, container := range containers {
		jobs <- container
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]containerResult, 0, len(containers))
	for result := range results {
		out = append(out, result)
	}
	return out
}

func collectContainer(ctx context.Context, api apiClient, scope monitoring.Scope, stableHostID string, hostRoot string, container containerSummary, timeout time.Duration) containerResult {
	b := monitoring.NewBuilder(containerScope(scope, stableHostID, container))
	dims := containerDims(container)
	containerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stats, err := api.stats(containerCtx, container.ID)
	if err != nil {
		b.State("docker.container.stats.available", false, dims)
		b.EventDetails("docker.container.collect.failed", map[string]string{"operation": "stats"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return containerResult{batch: b.Batch()}
	}
	b.State("docker.container.stats.available", true, dims)
	emitStats(b, dims, stats)
	inspect, err := api.inspect(containerCtx, container.ID)
	if err != nil {
		b.State("docker.container.inspect.available", false, dims)
		b.EventDetails("docker.container.collect.failed", map[string]string{"operation": "inspect"}, monitoring.EventDetails{
			Attributes: map[string]any{"error": err.Error()},
		})
		return containerResult{batch: b.Batch()}
	}
	b.State("docker.container.inspect.available", true, dims)
	emitInspect(b, scope, stableHostID, container, dims, hostRoot, inspect)
	return containerResult{batch: b.Batch()}
}

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
				results <- collectImage(ctx, api, scope, stableHostID, image, timeout, registryChecks, registryTimeout)
			}
		}()
	}
	for _, image := range images {
		jobs <- image
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
	identities := imageIdentitiesFrom(stableHostID, image.ID, image.References, inspect.RepoTags, inspect.RepoDigests)
	for _, identity := range identities {
		b := monitoring.NewBuilder(imageScope(scope, identity))
		dims := imageDims(identity)
		emitImageInspect(b, dims, inspect)
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
	Version         string `json:"Version"`
	OperatingSystem string `json:"OperatingSystem"`
	Architecture    string `json:"Architecture"`
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
	ID           string `json:"Id"`
	Created      string `json:"Created"`
	Image        string `json:"Image"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status     string `json:"Status"`
		Running    bool   `json:"Running"`
		Paused     bool   `json:"Paused"`
		Restarting bool   `json:"Restarting"`
		OOMKilled  bool   `json:"OOMKilled"`
		Dead       bool   `json:"Dead"`
		ExitCode   int    `json:"ExitCode"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
		Health     *struct {
			Status        string `json:"Status"`
			FailingStreak int    `json:"FailingStreak"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Image    string            `json:"Image"`
		Hostname string            `json:"Hostname"`
		Labels   map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		AutoRemove    bool   `json:"AutoRemove"`
		Privileged    bool   `json:"Privileged"`
		NetworkMode   string `json:"NetworkMode"`
		Runtime       string `json:"Runtime"`
		RestartPolicy struct {
			Name              string `json:"Name"`
			MaximumRetryCount int    `json:"MaximumRetryCount"`
		} `json:"RestartPolicy"`
	} `json:"HostConfig"`
	Mounts          []dockerMount `json:"Mounts"`
	NetworkSettings struct {
		Networks map[string]struct {
			NetworkID   string   `json:"NetworkID"`
			EndpointID  string   `json:"EndpointID"`
			Gateway     string   `json:"Gateway"`
			IPAddress   string   `json:"IPAddress"`
			IPPrefixLen int      `json:"IPPrefixLen"`
			MacAddress  string   `json:"MacAddress"`
			Aliases     []string `json:"Aliases"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

type dockerMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Driver      string `json:"Driver"`
	Mode        string `json:"Mode"`
	Propagation string `json:"Propagation"`
	RW          bool   `json:"RW"`
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
	PidsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
	Networks map[string]struct {
		RxBytes  uint64 `json:"rx_bytes"`
		TxBytes  uint64 `json:"tx_bytes"`
		RxErrors uint64 `json:"rx_errors"`
		TxErrors uint64 `json:"tx_errors"`
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

func NormalizeStableHostID(value string) string {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"host:", "docker:"} {
		value = strings.TrimPrefix(value, prefix)
	}
	return entityComponent(value)
}

func stableHostIDFromScope(scope monitoring.Scope) string {
	if scope.Dimensions != nil {
		if host := NormalizeStableHostID(scope.Dimensions["host"]); host != "" {
			return host
		}
	}
	return NormalizeStableHostID(scope.EntityID)
}

func hostEntityID(stableHostID string) string {
	return "host:" + entityComponent(stableHostID)
}

func dockerDaemonEntityID(stableHostID string) string {
	return "docker:" + entityComponent(stableHostID)
}

func containerEntityID(stableHostID string, container containerSummary) string {
	project := container.Labels["com.docker.compose.project"]
	service := container.Labels["com.docker.compose.service"]
	number := container.Labels["com.docker.compose.container-number"]
	if project != "" && service != "" && number != "" {
		return "container:" + entityComponent(stableHostID) + ":compose:" + entityComponent(project) + ":" + entityComponent(service) + ":" + entityComponent(number)
	}
	if name := containerName(container); name != "" {
		if project != "" && service != "" {
			return "container:" + entityComponent(stableHostID) + ":compose:" + entityComponent(project) + ":" + entityComponent(service) + ":name:" + entityComponent(name)
		}
		return "container:" + entityComponent(stableHostID) + ":name:" + entityComponent(name)
	}
	return "container:" + entityComponent(stableHostID) + ":id:" + entityComponent(shortID(container.ID))
}

func composeServiceEntityID(stableHostID, project, service string) string {
	return "compose:" + entityComponent(stableHostID) + ":" + entityComponent(project) + ":" + entityComponent(service)
}

func composeProjectEntityID(stableHostID, project string) string {
	return "compose-project:" + entityComponent(stableHostID) + ":" + entityComponent(project)
}

func imageEntityID(identity imageIdentity) string {
	return identity.EntityID
}

func entityComponent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "/")
	if value == "" {
		return ""
	}
	replacer := strings.NewReplacer(":", "_", "/", "_", " ", "_", "\t", "_", "\n", "_")
	return replacer.Replace(value)
}

func withEntity(scope monitoring.Scope, entityType, entityID string) monitoring.Scope {
	scope.EntityType = entityType
	scope.EntityID = entityID
	return scope
}

func withEntityLabel(scope monitoring.Scope, entityType, entityID, label string) monitoring.Scope {
	scope = withEntity(scope, entityType, entityID)
	scope.Label = label
	return scope
}

func containerScope(scope monitoring.Scope, stableHostID string, container containerSummary) monitoring.Scope {
	label := containerLabel(container)
	return withEntityLabel(scope, "docker-container", containerEntityID(stableHostID, container), label)
}

func composeServiceScope(scope monitoring.Scope, stableHostID, project, service string) monitoring.Scope {
	return withEntityLabel(scope, "docker-compose-service", composeServiceEntityID(stableHostID, project, service), project+"/"+service)
}

func composeProjectScope(scope monitoring.Scope, stableHostID, project string) monitoring.Scope {
	return withEntityLabel(scope, "docker-compose-project", composeProjectEntityID(stableHostID, project), project)
}

func imageScope(scope monitoring.Scope, identity imageIdentity) monitoring.Scope {
	return withEntityLabel(scope, "docker-image", imageEntityID(identity), identity.Label)
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
	cpuDelta := stats.CPUStats.CPUUsage.TotalUsage - stats.PreCPUStats.CPUUsage.TotalUsage
	systemDelta := stats.CPUStats.SystemUsage - stats.PreCPUStats.SystemUsage
	online := stats.CPUStats.OnlineCPUs
	if online == 0 {
		online = uint64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if systemDelta > 0 && online > 0 {
		b.Metric("docker.container.cpu.usage", "gauge", (float64(cpuDelta)/float64(systemDelta))*float64(online)*100, "percent", dims)
	}
	usage := dockerMemoryUsage(stats.MemoryStats.Usage, stats.MemoryStats.Stats)
	b.Metric("docker.container.memory.used", "gauge", float64(usage), "bytes", dims)
	if stats.MemoryStats.Limit > 0 {
		b.Metric("docker.container.memory.limit", "gauge", float64(stats.MemoryStats.Limit), "bytes", dims)
		b.Metric("docker.container.memory.usage", "gauge", (float64(usage)/float64(stats.MemoryStats.Limit))*100, "percent", dims)
	}
	b.Metric("docker.container.pids.current", "gauge", float64(stats.PidsStats.Current), "", dims)
	for iface, netStats := range stats.Networks {
		nd := copyDims(dims)
		nd["interface"] = iface
		b.Metric("docker.container.network.rx", "counter", float64(netStats.RxBytes), "bytes", nd)
		b.Metric("docker.container.network.tx", "counter", float64(netStats.TxBytes), "bytes", nd)
		b.Metric("docker.container.network.rx_errors", "counter", float64(netStats.RxErrors), "count", nd)
		b.Metric("docker.container.network.tx_errors", "counter", float64(netStats.TxErrors), "count", nd)
	}
	blockRead, blockWrite := dockerBlockIO(stats)
	b.Metric("docker.container.blockio.read", "counter", float64(blockRead), "bytes", dims)
	b.Metric("docker.container.blockio.write", "counter", float64(blockWrite), "bytes", dims)
}

func emitContainerSummary(batch *pulse.Batch, scope monitoring.Scope, stableHostID string, containers []containerSummary) {
	projects := map[string]int{}
	services := map[string]int{}
	servicesRunning := map[string]int{}
	for _, container := range containers {
		project := container.Labels["com.docker.compose.project"]
		service := container.Labels["com.docker.compose.service"]
		if project != "" {
			projects[project]++
			if service != "" {
				key := project + "\x00" + service
				services[key]++
				if container.State == "running" {
					servicesRunning[key]++
				}
			}
		}
	}
	for project, count := range projects {
		projectBuilder := monitoring.NewBuilder(composeProjectScope(scope, stableHostID, project))
		dims := map[string]string{"compose_project": project}
		projectBuilder.State("docker.compose.project.present", true, dims)
		projectBuilder.Metric("docker.compose.project.containers", "gauge", float64(count), "", dims)
		monitoring.Merge(batch, projectBuilder.Batch())
	}
	for key, count := range services {
		parts := strings.SplitN(key, "\x00", 2)
		service := monitoring.NewBuilder(composeServiceScope(scope, stableHostID, parts[0], parts[1]))
		dims := map[string]string{"compose_project": parts[0], "compose_service": parts[1]}
		service.State("docker.compose.service.present", true, dims)
		service.Metric("docker.compose.service.containers", "gauge", float64(count), "", dims)
		service.Metric("docker.compose.service.containers.running", "gauge", float64(servicesRunning[key]), "", dims)
		monitoring.Merge(batch, service.Batch())
	}
}

func emitComposeUsageSummary(batch *pulse.Batch, scope monitoring.Scope, stableHostID string) {
	names := map[string]string{
		"docker.container.cpu.usage":     "docker.compose.service.cpu.usage",
		"docker.container.memory.used":   "docker.compose.service.memory.used",
		"docker.container.memory.limit":  "docker.compose.service.memory.limit",
		"docker.container.pids.current":  "docker.compose.service.pids.current",
		"docker.container.network.rx":    "docker.compose.service.network.rx",
		"docker.container.network.tx":    "docker.compose.service.network.tx",
		"docker.container.blockio.read":  "docker.compose.service.blockio.read",
		"docker.container.blockio.write": "docker.compose.service.blockio.write",
	}
	type key struct {
		project string
		service string
		name    string
		typ     string
		unit    string
	}
	values := map[key]float64{}
	for _, metric := range batch.Metrics {
		name := names[metric.Name]
		project := metric.Dimensions["compose_project"]
		service := metric.Dimensions["compose_service"]
		if name == "" || project == "" || service == "" {
			continue
		}
		values[key{project: project, service: service, name: name, typ: metric.Type, unit: metric.Unit}] += metric.Value
	}
	for item, value := range values {
		builder := monitoring.NewBuilder(composeServiceScope(scope, stableHostID, item.project, item.service))
		dims := map[string]string{"compose_project": item.project, "compose_service": item.service}
		builder.Metric(item.name, item.typ, value, item.unit, dims)
		monitoring.Merge(batch, builder.Batch())
	}
}

func emitDockerHealthSummary(batch *pulse.Batch, daemonScope monitoring.Scope) {
	updates := map[string]bool{}
	healthy := map[string]bool{}
	unhealthy := map[string]bool{}
	for _, state := range batch.States {
		if state.Resource == nil {
			continue
		}
		switch state.Key {
		case "docker.image.update_available":
			if value, ok := state.Value.(bool); ok && value {
				updates[state.Resource.Key()] = true
			}
		case "docker.container.health.healthy":
			if value, ok := state.Value.(bool); ok {
				if value {
					healthy[state.Resource.Key()] = true
				} else {
					unhealthy[state.Resource.Key()] = true
				}
			}
		}
	}
	builder := monitoring.NewBuilder(daemonScope)
	builder.Metric("docker.images.updates_available", "gauge", float64(len(updates)), "", nil)
	builder.Metric("docker.containers.healthy", "gauge", float64(len(healthy)), "", nil)
	builder.Metric("docker.containers.unhealthy", "gauge", float64(len(unhealthy)), "", nil)
	monitoring.Merge(batch, builder.Batch())
}

func dockerMemoryUsage(usage uint64, stats map[string]uint64) uint64 {
	cache := stats["inactive_file"]
	if cache == 0 {
		cache = stats["cache"]
	}
	if cache > 0 && usage > cache {
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

func emitInspect(b *monitoring.Builder, scope monitoring.Scope, stableHostID string, container containerSummary, dims map[string]string, hostRoot string, inspect inspectResponse) {
	b.Metric("docker.container.restart_count", "gauge", float64(inspect.RestartCount), "", dims)
	b.Metric("docker.container.exit_code", "gauge", float64(inspect.State.ExitCode), "", dims)
	b.State("docker.container.lifecycle.status", inspect.State.Status, dims)
	b.State("docker.container.paused", inspect.State.Paused, dims)
	b.State("docker.container.restarting", inspect.State.Restarting, dims)
	b.State("docker.container.oom_killed", inspect.State.OOMKilled, dims)
	b.State("docker.container.dead", inspect.State.Dead, dims)
	b.State("docker.container.autoremove", inspect.HostConfig.AutoRemove, dims)
	b.State("docker.container.privileged", inspect.HostConfig.Privileged, dims)
	if inspect.Created != "" {
		b.State("docker.container.created_at", inspect.Created, dims)
	}
	if inspect.State.StartedAt != "" {
		b.State("docker.container.started_at", inspect.State.StartedAt, dims)
	}
	if inspect.State.FinishedAt != "" {
		b.State("docker.container.finished_at", inspect.State.FinishedAt, dims)
	}
	if inspect.Image != "" {
		b.State("docker.container.image.id", inspect.Image, dims)
	}
	if inspect.ID != "" {
		b.State("docker.container.runtime.id", inspect.ID, dims)
	}
	if inspect.Config.Image != "" {
		b.State("docker.container.image.reference", inspect.Config.Image, dims)
	}
	if inspect.Config.Hostname != "" {
		b.State("docker.container.hostname", inspect.Config.Hostname, dims)
	}
	if inspect.HostConfig.NetworkMode != "" {
		b.State("docker.container.network_mode", inspect.HostConfig.NetworkMode, dims)
	}
	if inspect.HostConfig.Runtime != "" {
		b.State("docker.container.runtime", inspect.HostConfig.Runtime, dims)
	}
	if inspect.HostConfig.RestartPolicy.Name != "" {
		b.State("docker.container.restart_policy", inspect.HostConfig.RestartPolicy.Name, dims)
		b.Metric("docker.container.restart_policy.maximum_retry_count", "gauge", float64(inspect.HostConfig.RestartPolicy.MaximumRetryCount), "", dims)
	}
	if startedAt, ok := parseDockerTime(inspect.State.StartedAt); ok && inspect.State.Running {
		b.Metric("docker.container.uptime", "gauge", time.Since(startedAt).Seconds(), "seconds", dims)
	}
	emitHealth(b, dims, inspect)
	emitComposeLabels(b, dims, inspect.Config.Labels)
	emitNetworks(b, container, inspect)
	emitMounts(b, container, dims, hostRoot, inspect.Mounts)
}

func emitHealth(b *monitoring.Builder, dims map[string]string, inspect inspectResponse) {
	if inspect.State.Health == nil {
		b.State("docker.container.health.available", false, dims)
		return
	}
	b.State("docker.container.health.available", true, dims)
	b.State("docker.container.health.status", inspect.State.Health.Status, dims)
	b.State("docker.container.health.healthy", inspect.State.Health.Status == "healthy", dims)
	b.Metric("docker.container.health.failing_streak", "gauge", float64(inspect.State.Health.FailingStreak), "", dims)
}

func emitComposeLabels(b *monitoring.Builder, dims map[string]string, labels map[string]string) {
	if len(labels) == 0 {
		b.State("docker.container.compose.available", false, dims)
		return
	}
	project := labels["com.docker.compose.project"]
	service := labels["com.docker.compose.service"]
	if project == "" && service == "" {
		b.State("docker.container.compose.available", false, dims)
		return
	}
	b.State("docker.container.compose.available", true, dims)
	emitLabelState(b, dims, labels, "com.docker.compose.project", "docker.container.compose.project")
	emitLabelState(b, dims, labels, "com.docker.compose.service", "docker.container.compose.service")
	emitLabelState(b, dims, labels, "com.docker.compose.container-number", "docker.container.compose.container_number")
	emitLabelState(b, dims, labels, "com.docker.compose.version", "docker.container.compose.version")
	emitLabelState(b, dims, labels, "com.docker.compose.config-hash", "docker.container.compose.config_hash")
	emitLabelState(b, dims, labels, "com.docker.compose.project.working_dir", "docker.container.compose.working_dir")
	emitLabelState(b, dims, labels, "com.docker.compose.project.config_files", "docker.container.compose.config_files")
}

func emitLabelState(b *monitoring.Builder, dims map[string]string, labels map[string]string, label, key string) {
	if value := labels[label]; value != "" {
		b.State(key, value, dims)
	}
}

func emitNetworks(b *monitoring.Builder, container containerSummary, inspect inspectResponse) {
	for network, data := range inspect.NetworkSettings.Networks {
		nd := networkDims(container, network)
		b.State("docker.container.network.connected", true, nd)
		if data.NetworkID != "" {
			b.State("docker.container.network.id", data.NetworkID, nd)
		}
		if data.EndpointID != "" {
			b.State("docker.container.network.endpoint_id", data.EndpointID, nd)
		}
		if data.Gateway != "" {
			b.State("docker.container.network.gateway", data.Gateway, nd)
		}
		if data.IPAddress != "" {
			b.State("docker.container.network.ip_address", data.IPAddress, nd)
		}
		if data.IPPrefixLen > 0 {
			b.Metric("docker.container.network.ip_prefix_len", "gauge", float64(data.IPPrefixLen), "bits", nd)
		}
		if data.MacAddress != "" {
			b.State("docker.container.network.mac_address", data.MacAddress, nd)
		}
		if len(data.Aliases) > 0 {
			b.State("docker.container.network.aliases", strings.Join(data.Aliases, ","), nd)
			b.Metric("docker.container.network.aliases.count", "gauge", float64(len(data.Aliases)), "", nd)
		}
	}
}

func emitMounts(b *monitoring.Builder, container containerSummary, dims map[string]string, hostRoot string, mounts []dockerMount) {
	b.Metric("docker.container.mounts.total", "gauge", float64(len(mounts)), "", dims)
	counts := map[string]int{}
	for _, mount := range mounts {
		counts[mount.Type]++
		md := mountDims(container, mount)
		if mount.Name != "" {
			b.State("docker.container.mount.volume", mount.Name, md)
		}
		if mount.Driver != "" {
			b.State("docker.container.mount.driver", mount.Driver, md)
		}
		b.State("docker.container.mount.rw", mount.RW, md)
		if mount.Source != "" {
			b.State("docker.container.mount.source", mount.Source, md)
		}
		if mount.Mode != "" {
			b.State("docker.container.mount.mode", mount.Mode, md)
		}
		if mount.Propagation != "" {
			b.State("docker.container.mount.propagation", mount.Propagation, md)
		}
		if mount.Source == "" {
			continue
		}
		path := filepath.Join(hostRoot, strings.TrimPrefix(mount.Source, "/"))
		var st syscall.Statfs_t
		if err := syscall.Statfs(path, &st); err != nil {
			continue
		}
		total := st.Blocks * uint64(st.Bsize)
		avail := st.Bavail * uint64(st.Bsize)
		used := total - st.Bfree*uint64(st.Bsize)
		b.Metric("docker.container.mount.filesystem.total", "gauge", float64(total), "bytes", md)
		b.Metric("docker.container.mount.filesystem.available", "gauge", float64(avail), "bytes", md)
		b.Metric("docker.container.mount.filesystem.used", "gauge", float64(used), "bytes", md)
		if total > 0 {
			b.Metric("docker.container.mount.filesystem.usage", "gauge", (float64(used)/float64(total))*100, "percent", md)
		}
	}
	for typ, count := range counts {
		b.Metric("docker.container.mounts.by_type", "gauge", float64(count), "", mergeMountType(dims, typ))
	}
}

func emitImageInspect(b *monitoring.Builder, dims map[string]string, inspect imageInspectResponse) {
	if inspect.ID != "" {
		b.State("docker.image.id", inspect.ID, dims)
	}
	b.State("docker.image.inspect.available", true, dims)
	if inspect.Created != "" {
		b.State("docker.image.created_at", inspect.Created, dims)
		if createdAt, ok := parseDockerTime(inspect.Created); ok {
			b.Metric("docker.image.age", "gauge", time.Since(createdAt).Seconds(), "seconds", dims)
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
	seen := map[string]map[string]bool{}
	for _, container := range containers {
		id := container.ImageID
		if id == "" {
			continue
		}
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
	EntityID   string
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
			EntityID:   "image:" + entityComponent(stableHostID) + ":" + entityComponent(repository) + ":" + entityComponent(ref.Tag),
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
			EntityID:   "image:" + entityComponent(stableHostID) + ":" + entityComponent(repository) + ":" + entityComponent(shortDigest),
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
		EntityID: "image:" + entityComponent(stableHostID) + ":" + entityComponent(short),
		Label:    short,
		ImageID:  shortImageID(imageID),
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

func mountDims(container containerSummary, mount dockerMount) map[string]string {
	dims := containerDims(container)
	dims["mount_type"] = mount.Type
	dims["mount_destination"] = mount.Destination
	if mount.Name != "" {
		dims["volume"] = mount.Name
	}
	return dims
}

func networkDims(container containerSummary, network string) map[string]string {
	dims := containerDims(container)
	dims["network"] = network
	return dims
}

func mergeMountType(dims map[string]string, typ string) map[string]string {
	out := copyDims(dims)
	out["mount_type"] = typ
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
