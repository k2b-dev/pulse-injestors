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
	Timeout          time.Duration
	ContainerTimeout time.Duration
	Concurrency      int
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
	client := newClient(socket, timeout)
	api := apiClient{client: client}
	b := monitoring.NewBuilder(scope)

	version, err := api.version(ctx)
	if err != nil {
		b.State("docker.available", false, nil)
		b.Event("docker.unavailable", nil, map[string]any{"error": err.Error()})
		return b.Batch(), nil
	}
	b.State("docker.available", true, nil)
	b.State("docker.version", version.Version, nil)
	b.State("docker.os", version.OperatingSystem, nil)
	b.State("docker.arch", version.Architecture, nil)

	containers, err := api.containers(ctx)
	if err != nil {
		b.Event("docker.collect.failed", nil, map[string]any{"error": err.Error(), "step": "containers"})
		return b.Batch(), nil
	}
	running := 0
	for _, container := range containers {
		if container.State == "running" {
			running++
		}
	}
	b.Metric("docker.containers.total", "gauge", float64(len(containers)), "count", nil)
	b.Metric("docker.containers.running", "gauge", float64(running), "count", nil)
	emitContainerSummary(b, containers)

	var jobs []containerSummary
	for _, container := range containers {
		dims := containerDims(container)
		b.State("docker.container.running", container.State == "running", dims)
		b.State("docker.container.status", container.Status, dims)
		b.State("docker.container.image", container.Image, dims)
		if container.State != "running" {
			continue
		}
		jobs = append(jobs, container)
	}
	results := collectContainers(ctx, api, scope, hostRoot, jobs, concurrency, containerTimeout)
	final := b.Batch()
	for _, result := range results {
		monitoring.Merge(&final, result.batch)
	}
	imageResults := collectImages(ctx, api, scope, containers, concurrency, containerTimeout)
	for _, result := range imageResults {
		monitoring.Merge(&final, result.batch)
	}
	return final, nil
}

type containerResult struct {
	batch pulse.Batch
}

func collectContainers(ctx context.Context, api apiClient, scope monitoring.Scope, hostRoot string, containers []containerSummary, concurrency int, timeout time.Duration) []containerResult {
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
				results <- collectContainer(ctx, api, scope, hostRoot, container, timeout)
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

func collectContainer(ctx context.Context, api apiClient, scope monitoring.Scope, hostRoot string, container containerSummary, timeout time.Duration) containerResult {
	b := monitoring.NewBuilder(scope)
	dims := containerDims(container)
	containerCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	stats, err := api.stats(containerCtx, container.ID)
	if err != nil {
		b.State("docker.container.stats.available", false, dims)
		b.Event("docker.container.collect.failed", dims, map[string]any{"error": err.Error(), "step": "stats"})
		return containerResult{batch: b.Batch()}
	}
	b.State("docker.container.stats.available", true, dims)
	emitStats(b, dims, stats)
	inspect, err := api.inspect(containerCtx, container.ID)
	if err != nil {
		b.State("docker.container.inspect.available", false, dims)
		b.Event("docker.container.collect.failed", dims, map[string]any{"error": err.Error(), "step": "inspect"})
		return containerResult{batch: b.Batch()}
	}
	b.State("docker.container.inspect.available", true, dims)
	emitInspect(b, dims, hostRoot, inspect)
	return containerResult{batch: b.Batch()}
}

func collectImages(ctx context.Context, api apiClient, scope monitoring.Scope, containers []containerSummary, concurrency int, timeout time.Duration) []containerResult {
	imageIDs := uniqueImageIDs(containers)
	if len(imageIDs) == 0 {
		return nil
	}
	jobs := make(chan string)
	results := make(chan containerResult, len(imageIDs))
	workers := concurrency
	if workers > len(imageIDs) {
		workers = len(imageIDs)
	}
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for imageID := range jobs {
				results <- collectImage(ctx, api, scope, imageID, timeout)
			}
		}()
	}
	for _, imageID := range imageIDs {
		jobs <- imageID
	}
	close(jobs)
	wg.Wait()
	close(results)

	out := make([]containerResult, 0, len(imageIDs))
	for result := range results {
		out = append(out, result)
	}
	return out
}

func collectImage(ctx context.Context, api apiClient, scope monitoring.Scope, imageID string, timeout time.Duration) containerResult {
	b := monitoring.NewBuilder(scope)
	dims := imageDims(imageID)
	imageCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	inspect, err := api.imageInspect(imageCtx, imageID)
	if err != nil {
		b.State("docker.image.inspect.available", false, dims)
		b.Event("docker.image.collect.failed", dims, map[string]any{"error": err.Error(), "step": "inspect"})
		return containerResult{batch: b.Batch()}
	}
	emitImageInspect(b, dims, inspect)
	return containerResult{batch: b.Batch()}
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
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Driver      string `json:"Driver"`
		Mode        string `json:"Mode"`
		Propagation string `json:"Propagation"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
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
	name := shortID(c.ID)
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}
	dims := map[string]string{
		"container":    name,
		"container_id": shortID(c.ID),
		"image":        c.Image,
	}
	addComposeDims(dims, c.Labels)
	return dims
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
	b.Metric("docker.container.pids.current", "gauge", float64(stats.PidsStats.Current), "count", dims)
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

func emitContainerSummary(b *monitoring.Builder, containers []containerSummary) {
	projects := map[string]int{}
	services := map[string]int{}
	for _, container := range containers {
		project := container.Labels["com.docker.compose.project"]
		service := container.Labels["com.docker.compose.service"]
		if project != "" {
			projects[project]++
			if service != "" {
				services[project+"\x00"+service]++
			}
		}
	}
	for project, count := range projects {
		b.Metric("docker.compose.project.containers", "gauge", float64(count), "count", map[string]string{"compose_project": project})
	}
	for key, count := range services {
		parts := strings.SplitN(key, "\x00", 2)
		b.Metric("docker.compose.service.containers", "gauge", float64(count), "count", map[string]string{"compose_project": parts[0], "compose_service": parts[1]})
	}
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

func emitInspect(b *monitoring.Builder, dims map[string]string, hostRoot string, inspect inspectResponse) {
	b.Metric("docker.container.restart_count", "gauge", float64(inspect.RestartCount), "count", dims)
	b.Metric("docker.container.exit_code", "gauge", float64(inspect.State.ExitCode), "code", dims)
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
		b.Metric("docker.container.restart_policy.maximum_retry_count", "gauge", float64(inspect.HostConfig.RestartPolicy.MaximumRetryCount), "count", dims)
	}
	if startedAt, ok := parseDockerTime(inspect.State.StartedAt); ok && inspect.State.Running {
		b.Metric("docker.container.uptime", "gauge", time.Since(startedAt).Seconds(), "seconds", dims)
	}
	emitHealth(b, dims, inspect)
	emitComposeLabels(b, dims, inspect.Config.Labels)
	emitNetworks(b, dims, inspect)
	emitMounts(b, dims, hostRoot, inspect.Mounts)
}

func emitHealth(b *monitoring.Builder, dims map[string]string, inspect inspectResponse) {
	if inspect.State.Health == nil {
		b.State("docker.container.health.available", false, dims)
		return
	}
	b.State("docker.container.health.available", true, dims)
	b.State("docker.container.health.status", inspect.State.Health.Status, dims)
	b.State("docker.container.health.healthy", inspect.State.Health.Status == "healthy", dims)
	b.Metric("docker.container.health.failing_streak", "gauge", float64(inspect.State.Health.FailingStreak), "count", dims)
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

func emitNetworks(b *monitoring.Builder, dims map[string]string, inspect inspectResponse) {
	for network, data := range inspect.NetworkSettings.Networks {
		nd := copyDims(dims)
		nd["network"] = network
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
			b.State("docker.container.network.aliases", data.Aliases, nd)
		}
	}
}

func emitMounts(b *monitoring.Builder, dims map[string]string, hostRoot string, mounts []struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Driver      string `json:"Driver"`
	Mode        string `json:"Mode"`
	Propagation string `json:"Propagation"`
	RW          bool   `json:"RW"`
}) {
	b.Metric("docker.container.mounts.total", "gauge", float64(len(mounts)), "count", dims)
	counts := map[string]int{}
	for _, mount := range mounts {
		counts[mount.Type]++
		md := copyDims(dims)
		md["mount_type"] = mount.Type
		md["mount_destination"] = mount.Destination
		if mount.Name != "" {
			md["volume"] = mount.Name
		}
		if mount.Driver != "" {
			md["volume_driver"] = mount.Driver
		}
		b.State("docker.container.mount.rw", mount.RW, md)
		if mount.Source != "" {
			b.State("docker.container.mount.source", mount.Source, md)
		}
		if mount.Driver != "" {
			b.State("docker.container.mount.driver", mount.Driver, md)
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
		md["mount_source"] = mount.Source
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
		b.Metric("docker.container.mounts.by_type", "gauge", float64(count), "count", mergeMountType(dims, typ))
	}
}

func emitImageInspect(b *monitoring.Builder, dims map[string]string, inspect imageInspectResponse) {
	if inspect.ID != "" {
		dims = copyDims(dims)
		dims["image_id"] = shortID(strings.TrimPrefix(inspect.ID, "sha256:"))
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
		b.State("docker.image.repo_tags", inspect.RepoTags, dims)
	}
	if len(inspect.RepoDigests) > 0 {
		b.State("docker.image.repo_digests", inspect.RepoDigests, dims)
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

func uniqueImageIDs(containers []containerSummary) []string {
	seen := map[string]bool{}
	for _, container := range containers {
		id := container.ImageID
		if id == "" {
			continue
		}
		seen[id] = true
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func imageDims(imageID string) map[string]string {
	return map[string]string{"image_id": shortID(strings.TrimPrefix(imageID, "sha256:"))}
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

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func copyDims(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
