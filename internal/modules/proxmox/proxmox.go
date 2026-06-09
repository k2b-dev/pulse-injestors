package proxmox

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

type Collector struct {
	BaseURL            string
	APIToken           string
	Timeout            time.Duration
	InsecureSkipVerify bool
	HTTPClient         *http.Client
}

func (c Collector) Name() string { return "proxmox" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	client, err := c.client()
	if err != nil {
		b.State("proxmox.available", false, nil)
		b.Event("proxmox.config.failed", nil, map[string]any{"error": err.Error()})
		return b.Batch(), nil
	}
	if err := collectVersion(ctx, b, client); err != nil {
		b.State("proxmox.available", false, nil)
		b.Event("proxmox.version.failed", nil, map[string]any{"error": err.Error()})
		return b.Batch(), nil
	}
	b.State("proxmox.available", true, nil)
	if err := collectClusterStatus(ctx, b, client); err != nil {
		b.Event("proxmox.cluster.status.failed", nil, map[string]any{"error": err.Error()})
	}
	if err := collectResources(ctx, b, client); err != nil {
		b.Event("proxmox.cluster.resources.failed", nil, map[string]any{"error": err.Error()})
	}
	return b.Batch(), nil
}

func (c Collector) client() (Client, error) {
	if c.BaseURL == "" {
		return Client{}, fmt.Errorf("proxmox api url is required")
	}
	if c.APIToken == "" {
		return Client{}, fmt.Errorf("proxmox api token is required")
	}
	base, err := normalizeBaseURL(c.BaseURL)
	if err != nil {
		return Client{}, err
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		timeout := c.Timeout
		if timeout <= 0 {
			timeout = 10 * time.Second
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		if c.InsecureSkipVerify {
			transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		}
		httpClient = &http.Client{Timeout: timeout, Transport: transport}
	}
	return Client{BaseURL: base, APIToken: authHeader(c.APIToken), HTTPClient: httpClient}, nil
}

type Client struct {
	BaseURL    string
	APIToken   string
	HTTPClient *http.Client
}

func (c Client) get(ctx context.Context, path string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.APIToken)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("proxmox API HTTP %d", resp.StatusCode)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	return json.Unmarshal(envelope.Data, target)
}

func collectVersion(ctx context.Context, b *monitoring.Builder, client Client) error {
	var version struct {
		Version string `json:"version"`
		Release string `json:"release"`
		RepoID  string `json:"repoid"`
	}
	if err := client.get(ctx, "/version", &version); err != nil {
		return err
	}
	if version.Version != "" {
		b.State("proxmox.version", version.Version, nil)
	}
	if version.Release != "" {
		b.State("proxmox.release", version.Release, nil)
	}
	if version.RepoID != "" {
		b.State("proxmox.repoid", version.RepoID, nil)
	}
	return nil
}

func collectClusterStatus(ctx context.Context, b *monitoring.Builder, client Client) error {
	var rows []ClusterStatus
	if err := client.get(ctx, "/cluster/status", &rows); err != nil {
		return err
	}
	nodes := 0
	online := 0
	for _, row := range rows {
		switch row.Type {
		case "cluster":
			if row.Name != "" {
				b.State("proxmox.cluster.name", row.Name, nil)
			}
			b.State("proxmox.cluster.quorate", row.Quorate == 1, nil)
		case "node":
			if row.Name == "" {
				continue
			}
			nodes++
			if row.Online == 1 {
				online++
			}
			dims := map[string]string{"node": row.Name}
			b.State("proxmox.node.online", row.Online == 1, dims)
			if row.IP != "" {
				b.State("proxmox.node.ip", row.IP, dims)
			}
		}
	}
	b.Metric("proxmox.nodes.total", "gauge", float64(nodes), "count", nil)
	b.Metric("proxmox.nodes.online", "gauge", float64(online), "count", nil)
	return nil
}

func collectResources(ctx context.Context, b *monitoring.Builder, client Client) error {
	var rows []Resource
	if err := client.get(ctx, "/cluster/resources", &rows); err != nil {
		return err
	}
	byType := map[string]int{}
	byStatus := map[string]int{}
	for _, row := range rows {
		if row.Type == "" || row.ID == "" {
			continue
		}
		byType[row.Type]++
		if row.Status != "" {
			byStatus[row.Type+":"+row.Status]++
		}
		dims := map[string]string{"type": row.Type, "resource": row.ID}
		if row.Node != "" {
			dims["node"] = row.Node
		}
		b.State("proxmox.resource.present", true, dims)
		if row.Status != "" {
			b.State("proxmox.resource.status", row.Status, dims)
		}
		if row.Name != "" {
			b.State("proxmox.resource.name", row.Name, dims)
		}
		if row.CPU > 0 {
			b.Metric("proxmox.resource.cpu.usage", "gauge", row.CPU*100, "percent", dims)
		}
		if row.MaxCPU > 0 {
			b.Metric("proxmox.resource.cpu.cores", "gauge", row.MaxCPU, "count", dims)
		}
		emitUsage(b, "proxmox.resource.memory", row.Mem, row.MaxMem, dims)
		emitUsage(b, "proxmox.resource.disk", row.Disk, row.MaxDisk, dims)
		if row.Uptime > 0 {
			b.Metric("proxmox.resource.uptime", "gauge", row.Uptime, "seconds", dims)
		}
	}
	for typ, count := range byType {
		b.Metric("proxmox.resources.by_type", "gauge", float64(count), "count", map[string]string{"type": typ})
	}
	for key, count := range byStatus {
		typ, status, _ := strings.Cut(key, ":")
		b.Metric("proxmox.resources.by_status", "gauge", float64(count), "count", map[string]string{"type": typ, "status": status})
	}
	return nil
}

func emitUsage(b *monitoring.Builder, prefix string, used, max float64, dims map[string]string) {
	if used > 0 {
		b.Metric(prefix+".used", "gauge", used, "bytes", dims)
	}
	if max > 0 {
		b.Metric(prefix+".total", "gauge", max, "bytes", dims)
		if used >= 0 {
			b.Metric(prefix+".usage", "gauge", (used/max)*100, "percent", dims)
		}
	}
}

type ClusterStatus struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Online  int    `json:"online"`
	Quorate int    `json:"quorate"`
}

type Resource struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Node    string  `json:"node"`
	Name    string  `json:"name"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu"`
	MaxCPU  float64 `json:"maxcpu"`
	Mem     float64 `json:"mem"`
	MaxMem  float64 `json:"maxmem"`
	Disk    float64 `json:"disk"`
	MaxDisk float64 `json:"maxdisk"`
	Uptime  float64 `json:"uptime"`
}

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("proxmox api url must use http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("proxmox api url must include a host")
	}
	if !strings.HasSuffix(u.Path, "/api2/json") {
		u.Path = strings.TrimRight(u.Path, "/") + "/api2/json"
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func authHeader(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "PVEAPIToken=") {
		return token
	}
	return "PVEAPIToken=" + token
}
