package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	SocketPath string
	HostRoot   string
	Timeout    time.Duration
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
	client := newClient(socket, timeout)
	api := apiClient{client: client}
	b := monitoring.NewBuilder(scope)

	version, err := api.version(ctx)
	if err != nil {
		b.State("docker.available", false, nil)
		return b.Batch(), err
	}
	b.State("docker.available", true, nil)
	b.State("docker.version", version.Version, nil)
	b.State("docker.os", version.OperatingSystem, nil)
	b.State("docker.arch", version.Architecture, nil)

	containers, err := api.containers(ctx)
	if err != nil {
		return b.Batch(), err
	}
	running := 0
	for _, container := range containers {
		if container.State == "running" {
			running++
		}
	}
	b.Metric("docker.containers.total", "gauge", float64(len(containers)), "count", nil)
	b.Metric("docker.containers.running", "gauge", float64(running), "count", nil)

	var errs []error
	for _, container := range containers {
		dims := containerDims(container)
		b.State("docker.container.running", container.State == "running", dims)
		b.State("docker.container.status", container.Status, dims)
		b.State("docker.container.image", container.Image, dims)
		if container.State != "running" {
			continue
		}
		stats, err := api.stats(ctx, container.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s stats: %w", shortID(container.ID), err))
			continue
		}
		emitStats(b, dims, stats)
		inspect, err := api.inspect(ctx, container.ID)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s inspect: %w", shortID(container.ID), err))
			continue
		}
		emitInspect(b, dims, hostRoot, inspect)
	}
	return b.Batch(), errors.Join(errs...)
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
	RestartCount int `json:"RestartCount"`
	Mounts       []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	} `json:"Mounts"`
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
	if c.Labels != nil {
		if v := c.Labels["com.docker.compose.project"]; v != "" {
			dims["compose_project"] = v
		}
		if v := c.Labels["com.docker.compose.service"]; v != "" {
			dims["compose_service"] = v
		}
	}
	return dims
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
	for _, mount := range inspect.Mounts {
		md := copyDims(dims)
		md["mount_type"] = mount.Type
		md["mount_destination"] = mount.Destination
		if mount.Name != "" {
			md["volume"] = mount.Name
		}
		b.State("docker.container.mount.rw", mount.RW, md)
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
