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
	EnableCephAPI      bool
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
	nodes, err := collectClusterStatus(ctx, b, client)
	if err != nil {
		b.Event("proxmox.cluster.status.failed", nil, map[string]any{"error": err.Error()})
	}
	if err := collectResources(ctx, b, client); err != nil {
		b.Event("proxmox.cluster.resources.failed", nil, map[string]any{"error": err.Error()})
	}
	collectTasksAndBackups(ctx, b, client)
	if c.EnableCephAPI {
		collectCephAPI(ctx, b, client, nodes)
	} else {
		b.State("proxmox.ceph.api.enabled", false, nil)
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

func collectClusterStatus(ctx context.Context, b *monitoring.Builder, client Client) ([]string, error) {
	var rows []ClusterStatus
	if err := client.get(ctx, "/cluster/status", &rows); err != nil {
		return nil, err
	}
	nodes := 0
	online := 0
	nodeNames := []string{}
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
				nodeNames = append(nodeNames, row.Name)
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
	return nodeNames, nil
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

func collectTasksAndBackups(ctx context.Context, b *monitoring.Builder, client Client) {
	tasks, err := getClusterTasks(ctx, client)
	if err != nil {
		b.Event("proxmox.cluster.tasks.failed", nil, map[string]any{"error": err.Error()})
	} else {
		emitTaskSignals(b, tasks)
		emitBackupTaskSignals(b, tasks)
	}
	if err := collectBackupJobs(ctx, b, client); err != nil {
		b.Event("proxmox.backup.jobs.failed", nil, map[string]any{"error": err.Error()})
	}
	if err := collectBackupCoverage(ctx, b, client); err != nil {
		b.Event("proxmox.backup.coverage.failed", nil, map[string]any{"error": err.Error()})
	}
}

func getClusterTasks(ctx context.Context, client Client) ([]Task, error) {
	var rows []Task
	if err := client.get(ctx, "/cluster/tasks", &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func emitTaskSignals(b *monitoring.Builder, tasks []Task) {
	byStatus := map[string]int{}
	byTypeStatus := map[string]int{}
	failedEvents := 0
	b.Metric("proxmox.tasks.recent", "gauge", float64(len(tasks)), "count", nil)
	for _, task := range tasks {
		status := taskStatusClass(task)
		typ := task.Type
		if typ == "" {
			typ = "unknown"
		}
		byStatus[status]++
		byTypeStatus[typ+":"+status]++
		if status == "error" && failedEvents < 10 {
			b.Event("proxmox.task.failed", taskDims(task), taskPayload(task))
			failedEvents++
		}
	}
	for status, count := range byStatus {
		b.Metric("proxmox.tasks.by_status", "gauge", float64(count), "count", map[string]string{"status": status})
	}
	for key, count := range byTypeStatus {
		typ, status, _ := strings.Cut(key, ":")
		b.Metric("proxmox.tasks.by_type_status", "gauge", float64(count), "count", map[string]string{"type": typ, "status": status})
	}
}

func emitBackupTaskSignals(b *monitoring.Builder, tasks []Task) {
	total := 0
	success := 0
	failed := 0
	var lastSuccess int64
	for _, task := range tasks {
		if task.Type != "vzdump" {
			continue
		}
		total++
		switch taskStatusClass(task) {
		case "ok":
			success++
			if task.EndTime > lastSuccess {
				lastSuccess = task.EndTime
			}
		case "error":
			failed++
			b.Event("proxmox.backup.failed", taskDims(task), taskPayload(task))
		}
	}
	b.Metric("proxmox.backup.tasks.recent", "gauge", float64(total), "count", nil)
	b.Metric("proxmox.backup.tasks.success", "gauge", float64(success), "count", nil)
	b.Metric("proxmox.backup.tasks.failed", "gauge", float64(failed), "count", nil)
	if lastSuccess > 0 {
		ts := time.Unix(lastSuccess, 0).UTC()
		b.State("proxmox.backup.last_success.time", ts.Format(time.RFC3339), nil)
		b.Metric("proxmox.backup.last_success.age", "gauge", time.Since(ts).Seconds(), "seconds", nil)
	}
}

func collectBackupJobs(ctx context.Context, b *monitoring.Builder, client Client) error {
	var rows []map[string]any
	if err := client.get(ctx, "/cluster/backup", &rows); err != nil {
		return err
	}
	jobs := parseBackupJobs(rows)
	enabled := 0
	disabled := 0
	for _, job := range jobs {
		dims := map[string]string{"job": job.ID}
		b.State("proxmox.backup.job.present", true, dims)
		b.State("proxmox.backup.job.enabled", job.Enabled, dims)
		if job.Enabled {
			enabled++
		} else {
			disabled++
		}
		if job.Schedule != "" {
			b.State("proxmox.backup.job.schedule", job.Schedule, dims)
		}
		if job.Storage != "" {
			b.State("proxmox.backup.job.storage", job.Storage, dims)
		}
		if job.Mode != "" {
			b.State("proxmox.backup.job.mode", job.Mode, dims)
		}
	}
	b.Metric("proxmox.backup.jobs.total", "gauge", float64(len(jobs)), "count", nil)
	b.Metric("proxmox.backup.jobs.enabled", "gauge", float64(enabled), "count", nil)
	b.Metric("proxmox.backup.jobs.disabled", "gauge", float64(disabled), "count", nil)
	return nil
}

func collectBackupCoverage(ctx context.Context, b *monitoring.Builder, client Client) error {
	var rows []map[string]any
	if err := client.get(ctx, "/cluster/backup-info/not-backed-up", &rows); err != nil {
		return err
	}
	b.Metric("proxmox.backup.guests.not_backed_up", "gauge", float64(len(rows)), "count", nil)
	for _, row := range rows {
		dims := map[string]string{}
		addDim(dims, "vmid", firstString(row, "vmid"))
		addDim(dims, "guest", firstString(row, "name"))
		addDim(dims, "type", firstString(row, "type"))
		addDim(dims, "node", firstString(row, "node"))
		b.State("proxmox.backup.guest.covered", false, dims)
	}
	return nil
}

type Task struct {
	UPID      string `json:"upid"`
	Node      string `json:"node"`
	Type      string `json:"type"`
	ID        string `json:"id"`
	User      string `json:"user"`
	Status    string `json:"status"`
	StartTime int64  `json:"starttime"`
	EndTime   int64  `json:"endtime"`
}

func taskStatusClass(task Task) string {
	if task.EndTime == 0 && task.Status == "" {
		return "running"
	}
	if task.Status == "OK" {
		return "ok"
	}
	if task.Status == "" {
		return "unknown"
	}
	return "error"
}

func taskDims(task Task) map[string]string {
	dims := map[string]string{}
	addDim(dims, "node", task.Node)
	addDim(dims, "type", task.Type)
	addDim(dims, "id", task.ID)
	return dims
}

func taskPayload(task Task) map[string]any {
	payload := map[string]any{
		"status": task.Status,
		"upid":   task.UPID,
	}
	if task.User != "" {
		payload["user"] = task.User
	}
	if task.StartTime > 0 {
		payload["startTime"] = time.Unix(task.StartTime, 0).UTC().Format(time.RFC3339)
	}
	if task.EndTime > 0 {
		payload["endTime"] = time.Unix(task.EndTime, 0).UTC().Format(time.RFC3339)
	}
	return payload
}

type BackupJob struct {
	ID       string
	Enabled  bool
	Schedule string
	Storage  string
	Mode     string
}

func parseBackupJobs(rows []map[string]any) []BackupJob {
	jobs := make([]BackupJob, 0, len(rows))
	for _, row := range rows {
		id := firstString(row, "id")
		if id == "" {
			continue
		}
		jobs = append(jobs, BackupJob{
			ID:       id,
			Enabled:  firstBool(row, true, "enabled"),
			Schedule: firstString(row, "schedule"),
			Storage:  firstString(row, "storage"),
			Mode:     firstString(row, "mode"),
		})
	}
	return jobs
}

func collectCephAPI(ctx context.Context, b *monitoring.Builder, client Client, nodes []string) {
	b.State("proxmox.ceph.api.enabled", true, nil)
	status, err := getCephStatus(ctx, client)
	if err != nil {
		b.State("proxmox.ceph.available", false, nil)
		b.Event("proxmox.ceph.status.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	summary := status.summary()
	b.State("proxmox.ceph.available", true, nil)
	if summary.Health != "" {
		b.State("proxmox.ceph.health.status", summary.Health, nil)
		b.State("proxmox.ceph.health.healthy", summary.Health == "HEALTH_OK", nil)
	}
	b.Metric("proxmox.ceph.osds.total", "gauge", float64(summary.OSDsTotal), "count", nil)
	b.Metric("proxmox.ceph.osds.up", "gauge", float64(summary.OSDsUp), "count", nil)
	b.Metric("proxmox.ceph.osds.in", "gauge", float64(summary.OSDsIn), "count", nil)
	b.Metric("proxmox.ceph.pgs.total", "gauge", float64(summary.PGsTotal), "count", nil)
	b.Metric("proxmox.ceph.bytes.used", "gauge", summary.BytesUsed, "bytes", nil)
	b.Metric("proxmox.ceph.bytes.total", "gauge", summary.BytesTotal, "bytes", nil)
	b.Metric("proxmox.ceph.bytes.available", "gauge", summary.BytesAvailable, "bytes", nil)
	for state, count := range summary.PGsByState {
		b.Metric("proxmox.ceph.pgs.by_state", "gauge", float64(count), "count", map[string]string{"state": state})
	}
	if len(nodes) > 0 {
		collectCephPools(ctx, b, client, nodes)
	}
}

func getCephStatus(ctx context.Context, client Client) (CephStatus, error) {
	var status CephStatus
	if err := client.get(ctx, "/cluster/ceph/status", &status); err != nil {
		return CephStatus{}, err
	}
	return status, nil
}

func collectCephPools(ctx context.Context, b *monitoring.Builder, client Client, nodes []string) {
	var lastErr error
	for _, node := range nodes {
		var rows []map[string]any
		err := client.get(ctx, "/nodes/"+url.PathEscape(node)+"/ceph/pools", &rows)
		if err != nil {
			lastErr = err
			continue
		}
		pools := parseCephPools(rows)
		b.Metric("proxmox.ceph.pools", "gauge", float64(len(pools)), "count", nil)
		for _, pool := range pools {
			dims := map[string]string{"pool": pool.Name, "node": node}
			b.State("proxmox.ceph.pool.present", true, dims)
			b.Metric("proxmox.ceph.pool.bytes.used", "gauge", pool.BytesUsed, "bytes", dims)
			b.Metric("proxmox.ceph.pool.bytes.available", "gauge", pool.BytesAvailable, "bytes", dims)
			b.Metric("proxmox.ceph.pool.objects", "gauge", pool.Objects, "count", dims)
		}
		return
	}
	if lastErr != nil {
		b.Event("proxmox.ceph.pools.failed", nil, map[string]any{"error": lastErr.Error()})
	}
}

type CephStatus struct {
	Health struct {
		Status string `json:"status"`
	} `json:"health"`
	OSDMap struct {
		OSDMap struct {
			NumOSDs   int `json:"num_osds"`
			NumUpOSDs int `json:"num_up_osds"`
			NumInOSDs int `json:"num_in_osds"`
		} `json:"osdmap"`
	} `json:"osdmap"`
	PGMap struct {
		NumPGs     int     `json:"num_pgs"`
		BytesUsed  float64 `json:"bytes_used"`
		BytesTotal float64 `json:"bytes_total"`
		BytesAvail float64 `json:"bytes_avail"`
		PGsByState []struct {
			StateName string `json:"state_name"`
			Count     int    `json:"count"`
		} `json:"pgs_by_state"`
	} `json:"pgmap"`
}

type CephSummary struct {
	Health         string
	OSDsTotal      int
	OSDsUp         int
	OSDsIn         int
	PGsTotal       int
	PGsByState     map[string]int
	BytesUsed      float64
	BytesTotal     float64
	BytesAvailable float64
}

func (s CephStatus) summary() CephSummary {
	out := CephSummary{
		Health:         s.Health.Status,
		OSDsTotal:      s.OSDMap.OSDMap.NumOSDs,
		OSDsUp:         s.OSDMap.OSDMap.NumUpOSDs,
		OSDsIn:         s.OSDMap.OSDMap.NumInOSDs,
		PGsTotal:       s.PGMap.NumPGs,
		PGsByState:     map[string]int{},
		BytesUsed:      s.PGMap.BytesUsed,
		BytesTotal:     s.PGMap.BytesTotal,
		BytesAvailable: s.PGMap.BytesAvail,
	}
	for _, row := range s.PGMap.PGsByState {
		if row.StateName != "" {
			out.PGsByState[row.StateName] += row.Count
		}
	}
	return out
}

type CephPool struct {
	Name           string
	BytesUsed      float64
	BytesAvailable float64
	Objects        float64
}

func parseCephPools(rows []map[string]any) []CephPool {
	pools := make([]CephPool, 0, len(rows))
	for _, row := range rows {
		name := firstString(row, "pool_name", "name")
		if name == "" {
			continue
		}
		pools = append(pools, CephPool{
			Name:           name,
			BytesUsed:      firstFloat(row, "bytes_used", "stored", "used"),
			BytesAvailable: firstFloat(row, "max_avail", "bytes_avail", "available"),
			Objects:        firstFloat(row, "objects"),
		})
	}
	return pools
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := row[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case string:
			if v != "" {
				return v
			}
		case int:
			return fmt.Sprint(v)
		case int64:
			return fmt.Sprint(v)
		case float64:
			return fmt.Sprintf("%.0f", v)
		case json.Number:
			return v.String()
		}
	}
	return ""
}

func firstBool(row map[string]any, fallback bool, keys ...string) bool {
	for _, key := range keys {
		value, ok := row[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v
		case int:
			return v != 0
		case int64:
			return v != 0
		case float64:
			return v != 0
		case json.Number:
			parsed, err := v.Int64()
			if err == nil {
				return parsed != 0
			}
		case string:
			return v != "0" && v != "false" && v != "no"
		}
	}
	return fallback
}

func firstFloat(row map[string]any, keys ...string) float64 {
	for _, key := range keys {
		value, ok := row[key]
		if !ok {
			continue
		}
		switch v := value.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		case json.Number:
			parsed, err := v.Float64()
			if err == nil {
				return parsed
			}
		}
	}
	return 0
}

func addDim(dims map[string]string, key, value string) {
	if value != "" {
		dims[key] = value
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
