package proxmox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var ErrCommandNotFound = errors.New("proxmox command not found")

type Collector struct {
	PveshPath     string
	Timeout       time.Duration
	EnableCephAPI bool
	Runner        CommandRunner
}

func (c Collector) Name() string { return "proxmox" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	client, err := c.client()
	if err != nil {
		b := monitoring.NewBuilder(scope)
		b.State("proxmox.available", false, nil)
		if !errors.Is(err, ErrCommandNotFound) {
			emitError(b, "proxmox.config.failed", "configure", nil, err)
		}
		return b.Batch(), nil
	}
	clusterRows, clusterErr := getClusterStatus(ctx, client)
	clusterID := clusterIDFromStatus(clusterRows)
	localNode := stableHostFromScope(scope)
	b := monitoring.NewBuilder(nodeScope(scope, localNode, clusterID))
	if err := collectVersion(ctx, b, client); err != nil {
		b.State("proxmox.available", false, nil)
		emitError(b, "proxmox.version.failed", "version", nil, err)
		return b.Batch(), nil
	}
	b.State("proxmox.available", true, nil)
	var nodes []string
	if clusterErr != nil {
		emitError(b, "proxmox.cluster.status.failed", "cluster_status", nil, clusterErr)
		nodes = collectLocalNodeStatus(ctx, b, client, scope, "")
	} else {
		nodes = emitClusterStatus(b, scope, clusterRows, clusterID)
	}
	if err := collectResources(ctx, b, client, scope, clusterID); err != nil {
		emitError(b, "proxmox.cluster.resources.failed", "cluster_resources", nil, err)
	}
	collectTasksAndBackups(ctx, b, client, scope, clusterID)
	if c.EnableCephAPI {
		collectCephAPI(ctx, b, client, scope, clusterID, nodes)
	} else {
		b.State("proxmox.ceph.api.enabled", false, nil)
	}
	return b.Batch(), nil
}

func (c Collector) client() (Client, error) {
	path := strings.TrimSpace(c.PveshPath)
	if path == "" {
		path = "pvesh"
	}
	runner := c.Runner
	if runner == nil {
		if !strings.Contains(path, "/") {
			resolved, err := exec.LookPath(path)
			if err != nil {
				return Client{}, fmt.Errorf("%w: %s", ErrCommandNotFound, path)
			}
			path = resolved
		} else if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return Client{}, fmt.Errorf("%w: %s", ErrCommandNotFound, path)
			}
			return Client{}, err
		}
		runner = execRunner{}
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return Client{PveshPath: path, Timeout: timeout, Runner: runner}, nil
}

type Client struct {
	PveshPath string
	Timeout   time.Duration
	Runner    CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if len(out) > 0 {
			return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil, err
	}
	return out, nil
}

func (c Client) get(ctx context.Context, path string, target any) error {
	args, err := pveshArgs(path)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, c.Timeout)
	defer cancel()
	out, err := c.Runner.Run(runCtx, c.PveshPath, args...)
	if err != nil {
		return err
	}
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 || string(out) == "null" {
		return nil
	}
	return json.Unmarshal(out, target)
}

func pveshArgs(path string) ([]string, error) {
	cleanPath, query, err := splitAPIPath(path)
	if err != nil {
		return nil, err
	}
	args := []string{"get", cleanPath}
	args = append(args, queryCLIArgs(query)...)
	args = append(args, "--output-format", "json")
	return args, nil
}

func splitAPIPath(path string) (string, url.Values, error) {
	u, err := url.Parse(path)
	if err != nil {
		return "", nil, err
	}
	cleanPath := u.EscapedPath()
	if cleanPath == "" {
		cleanPath = path
	}
	return cleanPath, u.Query(), nil
}

func queryCLIArgs(query url.Values) []string {
	if len(query) == 0 {
		return nil
	}
	args := []string{}
	for key, values := range query {
		for _, value := range values {
			args = append(args, "--"+key)
			if value != "" {
				args = append(args, value)
			}
		}
	}
	return args
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

func getClusterStatus(ctx context.Context, client Client) ([]ClusterStatus, error) {
	var rows []ClusterStatus
	if err := client.get(ctx, "/cluster/status", &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

func clusterIDFromStatus(rows []ClusterStatus) string {
	for _, row := range rows {
		if row.Type == "cluster" && row.Name != "" {
			return entity.Key(row.Name, "")
		}
	}
	return ""
}

func emitClusterStatus(b *monitoring.Builder, scope monitoring.Scope, rows []ClusterStatus, clusterID string) []string {
	nodes := 0
	online := 0
	nodeNames := []string{}
	var clusterBuilder *monitoring.Builder
	if clusterID != "" {
		clusterBuilder = monitoring.NewBuilder(proxmoxClusterScope(scope, clusterID))
	}
	aggregate := b
	if clusterBuilder != nil {
		aggregate = clusterBuilder
	}
	for _, row := range rows {
		switch row.Type {
		case "cluster":
			if clusterBuilder != nil && row.Name != "" {
				aggregate.State("proxmox.cluster.name", row.Name, nil)
			}
			if clusterBuilder != nil {
				aggregate.State("proxmox.cluster.quorate", row.Quorate == 1, nil)
			}
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
			nb := monitoring.NewBuilder(nodeScope(scope, row.Name, clusterID))
			nb.State("proxmox.node.online", row.Online == 1, dims)
			if row.IP != "" {
				nb.State("proxmox.node.ip", row.IP, dims)
			}
			b.Merge(nb.Batch())
		}
	}
	aggregate.Metric("proxmox.nodes.total", "gauge", float64(nodes), "", nil)
	aggregate.Metric("proxmox.nodes.online", "gauge", float64(online), "", nil)
	if clusterBuilder != nil {
		b.Merge(clusterBuilder.Batch())
	}
	return nodeNames
}

func collectLocalNodeStatus(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string) []string {
	nodes, err := collectNodes(ctx, b, client, scope, clusterID)
	if err == nil {
		return nodes
	}
	emitError(b, "proxmox.nodes.failed", "nodes", nil, err)
	host, hostErr := os.Hostname()
	if hostErr != nil || host == "" {
		host = "localhost"
	}
	dims := map[string]string{"node": host}
	nb := monitoring.NewBuilder(nodeScope(scope, host, clusterID))
	nb.State("proxmox.node.online", true, dims)
	b.Merge(nb.Batch())
	b.Metric("proxmox.nodes.total", "gauge", 1, "", nil)
	b.Metric("proxmox.nodes.online", "gauge", 1, "", nil)
	return []string{host}
}

func collectNodes(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string) ([]string, error) {
	var rows []NodeStatus
	if err := client.get(ctx, "/nodes", &rows); err != nil {
		return nil, err
	}
	nodeNames := []string{}
	online := 0
	for _, row := range rows {
		if row.Node == "" {
			continue
		}
		nodeNames = append(nodeNames, row.Node)
		isOnline := row.Status == "" || row.Status == "online"
		if isOnline {
			online++
		}
		dims := map[string]string{"node": row.Node}
		nb := monitoring.NewBuilder(nodeScope(scope, row.Node, clusterID))
		nb.State("proxmox.node.online", isOnline, dims)
		if row.Status != "" {
			nb.State("proxmox.node.status", row.Status, dims)
		}
		if row.CPU > 0 {
			nb.Metric("proxmox.node.cpu.usage", "gauge", row.CPU*100, "percent", dims)
		}
		if row.MaxCPU > 0 {
			nb.Metric("proxmox.node.cpu.cores", "gauge", row.MaxCPU, "", dims)
		}
		emitUsage(nb, "proxmox.node.memory", row.Mem, row.MaxMem, dims)
		emitUsage(nb, "proxmox.node.disk", row.Disk, row.MaxDisk, dims)
		if row.Uptime > 0 {
			nb.Metric("proxmox.node.uptime", "gauge", row.Uptime, "seconds", dims)
		}
		b.Merge(nb.Batch())
	}
	b.Metric("proxmox.nodes.total", "gauge", float64(len(nodeNames)), "", nil)
	b.Metric("proxmox.nodes.online", "gauge", float64(online), "", nil)
	return nodeNames, nil
}

func collectResources(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string) error {
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
		dims := map[string]string{"type": row.Type}
		if row.Node != "" {
			dims["node"] = row.Node
		}
		if vmid := guestVMID(row); vmid != "" {
			dims["vmid"] = vmid
		}
		if resourceScope, ok := proxmoxResourceScope(scope, row, clusterID); ok {
			rb := monitoring.NewBuilder(resourceScope)
			emitResourceSignals(rb, row, dims)
			b.Merge(rb.Batch())
		} else {
			emitResourceSignals(b, row, dims)
		}
	}
	for typ, count := range byType {
		b.Metric("proxmox.resources.by_type", "gauge", float64(count), "", map[string]string{"type": typ})
	}
	for key, count := range byStatus {
		typ, status, _ := strings.Cut(key, ":")
		b.Metric("proxmox.resources.by_status", "gauge", float64(count), "", map[string]string{"type": typ, "status": status})
	}
	return nil
}

func emitResourceSignals(b *monitoring.Builder, row Resource, dims map[string]string) {
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
		b.Metric("proxmox.resource.cpu.cores", "gauge", row.MaxCPU, "", dims)
	}
	emitUsage(b, "proxmox.resource.memory", row.Mem, row.MaxMem, dims)
	emitUsage(b, "proxmox.resource.disk", row.Disk, row.MaxDisk, dims)
	if row.Uptime > 0 {
		b.Metric("proxmox.resource.uptime", "gauge", row.Uptime, "seconds", dims)
	}
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

func nodeScope(scope monitoring.Scope, node, clusterID string) monitoring.Scope {
	scope.EntityType = "proxmox-node"
	scope.EntityID = entity.ID("proxmox-node", clusterNamespace(scope, clusterID), node)
	scope.Label = node
	return scope
}

func proxmoxClusterScope(scope monitoring.Scope, clusterID string) monitoring.Scope {
	scope.EntityType = "proxmox-cluster"
	scope.EntityID = entity.ID("proxmox-cluster", clusterNamespace(scope, clusterID))
	scope.Label = clusterID
	return scope
}

func proxmoxResourceScope(scope monitoring.Scope, row Resource, clusterID string) (monitoring.Scope, bool) {
	vmid := resourceVMID(row.ID)
	if vmid == "" {
		return monitoring.Scope{}, false
	}
	label := row.Name
	if label == "" {
		label = vmid
	}
	switch row.Type {
	case "node":
		return nodeScope(scope, vmid, clusterID), true
	case "qemu":
		scope.EntityType = "proxmox-vm"
		scope.EntityID = entity.ID("proxmox-vm", clusterNamespace(scope, clusterID), vmid)
		scope.Label = label
		return scope, true
	case "lxc":
		scope.EntityType = "proxmox-container"
		scope.EntityID = entity.ID("proxmox-container", clusterNamespace(scope, clusterID), vmid)
		scope.Label = label
		return scope, true
	default:
		return monitoring.Scope{}, false
	}
}

func guestVMID(row Resource) string {
	switch row.Type {
	case "qemu", "lxc":
		return resourceVMID(row.ID)
	default:
		return ""
	}
}

func resourceVMID(id string) string {
	if _, value, ok := strings.Cut(id, "/"); ok {
		return value
	}
	return id
}

func stableHostFromScope(scope monitoring.Scope) string {
	return entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions)
}

func clusterNamespace(scope monitoring.Scope, clusterID string) string {
	if v := entity.Key(clusterID, ""); v != "" {
		return v
	}
	return stableHostFromScope(scope)
}

func backupJobScope(scope monitoring.Scope, clusterID, job string) monitoring.Scope {
	scope.EntityType = "proxmox-backup-job"
	scope.EntityID = entity.ID("proxmox-backup-job", clusterNamespace(scope, clusterID), entity.Key(job, "unknown"))
	scope.Label = labelValue(job, "unknown")
	return scope
}

func backupGuestScope(scope monitoring.Scope, clusterID, typ, vmid, guest string) monitoring.Scope {
	vmid = entity.Key(vmid, "")
	if vmid == "" {
		return scope
	}
	label := labelValue(guest, vmid)
	switch typ {
	case "qemu":
		scope.EntityType = "proxmox-vm"
		scope.EntityID = entity.ID("proxmox-vm", clusterNamespace(scope, clusterID), vmid)
		scope.Label = label
	case "lxc":
		scope.EntityType = "proxmox-container"
		scope.EntityID = entity.ID("proxmox-container", clusterNamespace(scope, clusterID), vmid)
		scope.Label = label
	}
	return scope
}

func cephClusterScope(scope monitoring.Scope, clusterID string) monitoring.Scope {
	scope.EntityType = "ceph-cluster"
	namespace := clusterNamespace(scope, clusterID)
	scope.EntityID = entity.ID("ceph-cluster", namespace)
	scope.Label = "Ceph " + namespace
	return scope
}

func cephPoolScope(scope monitoring.Scope, clusterID, pool string) monitoring.Scope {
	scope.EntityType = "ceph-pool"
	scope.EntityID = entity.ID("ceph-pool", clusterNamespace(scope, clusterID), entity.Key(pool, "unknown"))
	scope.Label = labelValue(pool, "unknown")
	return scope
}

func labelValue(value, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	if fallback = strings.TrimSpace(fallback); fallback != "" {
		return fallback
	}
	return "unknown"
}

func emitError(b *monitoring.Builder, kind, operation string, dims map[string]string, err error) {
	b.EventDetails(kind, monitoring.MergeDimensions(dims, map[string]string{"operation": operation}), monitoring.EventDetails{
		Attributes: map[string]any{"error": err.Error()},
	})
}

func collectTasksAndBackups(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string) {
	tasks, err := getClusterTasks(ctx, client)
	if err != nil {
		emitError(b, "proxmox.cluster.tasks.failed", "cluster_tasks", nil, err)
	} else {
		emitTaskSignals(b, tasks)
		emitBackupTaskSignals(b, tasks)
	}
	if err := collectBackupJobs(ctx, b, client, scope, clusterID); err != nil {
		emitError(b, "proxmox.backup.jobs.failed", "backup_jobs", nil, err)
	}
	if err := collectBackupCoverage(ctx, b, client, scope, clusterID); err != nil {
		emitError(b, "proxmox.backup.coverage.failed", "backup_coverage", nil, err)
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
	b.Metric("proxmox.tasks.recent", "gauge", float64(len(tasks)), "", nil)
	for _, task := range tasks {
		status := taskStatusClass(task)
		typ := task.Type
		if typ == "" {
			typ = "unknown"
		}
		byStatus[status]++
		byTypeStatus[typ+":"+status]++
		if status == "error" && failedEvents < 10 {
			emitTaskEvent(b, "proxmox.task.failed", task)
			failedEvents++
		}
	}
	for status, count := range byStatus {
		b.Metric("proxmox.tasks.by_status", "gauge", float64(count), "", map[string]string{"status": status})
	}
	for key, count := range byTypeStatus {
		typ, status, _ := strings.Cut(key, ":")
		b.Metric("proxmox.tasks.by_type_status", "gauge", float64(count), "", map[string]string{"type": typ, "status": status})
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
			emitTaskEvent(b, "proxmox.backup.failed", task)
		}
	}
	b.Metric("proxmox.backup.tasks.recent", "gauge", float64(total), "", nil)
	b.Metric("proxmox.backup.tasks.success", "gauge", float64(success), "", nil)
	b.Metric("proxmox.backup.tasks.failed", "gauge", float64(failed), "", nil)
	if lastSuccess > 0 {
		ts := time.Unix(lastSuccess, 0).UTC()
		b.State("proxmox.backup.last_success.time", ts.Format(time.RFC3339), nil)
		b.Metric("proxmox.backup.last_success.age", "gauge", time.Since(ts).Seconds(), "seconds", nil)
	}
}

func collectBackupJobs(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string) error {
	var rows []map[string]any
	if err := client.get(ctx, "/cluster/backup", &rows); err != nil {
		return err
	}
	jobs := parseBackupJobs(rows)
	enabled := 0
	disabled := 0
	for _, job := range jobs {
		dims := map[string]string{"job": job.ID}
		jb := monitoring.NewBuilder(backupJobScope(scope, clusterID, job.ID))
		jb.State("proxmox.backup.job.present", true, dims)
		jb.State("proxmox.backup.job.enabled", job.Enabled, dims)
		if job.Enabled {
			enabled++
		} else {
			disabled++
		}
		if job.Schedule != "" {
			jb.State("proxmox.backup.job.schedule", job.Schedule, dims)
		}
		if job.Storage != "" {
			jb.State("proxmox.backup.job.storage", job.Storage, dims)
		}
		if job.Mode != "" {
			jb.State("proxmox.backup.job.mode", job.Mode, dims)
		}
		b.Merge(jb.Batch())
	}
	b.Metric("proxmox.backup.jobs.total", "gauge", float64(len(jobs)), "", nil)
	b.Metric("proxmox.backup.jobs.enabled", "gauge", float64(enabled), "", nil)
	b.Metric("proxmox.backup.jobs.disabled", "gauge", float64(disabled), "", nil)
	return nil
}

func collectBackupCoverage(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string) error {
	var rows []map[string]any
	if err := client.get(ctx, "/cluster/backup-info/not-backed-up", &rows); err != nil {
		return err
	}
	b.Metric("proxmox.backup.guests.not_backed_up", "gauge", float64(len(rows)), "", nil)
	for _, row := range rows {
		dims := map[string]string{}
		addDim(dims, "vmid", firstString(row, "vmid"))
		addDim(dims, "guest", firstString(row, "name"))
		addDim(dims, "type", firstString(row, "type"))
		addDim(dims, "node", firstString(row, "node"))
		gb := monitoring.NewBuilder(backupGuestScope(scope, clusterID, dims["type"], dims["vmid"], dims["guest"]))
		gb.State("proxmox.backup.guest.covered", false, dims)
		b.Merge(gb.Batch())
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
	addDim(dims, "status", taskStatusClass(task))
	return dims
}

func taskSubject(prefix string, task Task) string {
	parts := []string{prefix}
	if task.Type != "" {
		parts = append(parts, task.Type)
	}
	if task.ID != "" {
		parts = append(parts, task.ID)
	} else if task.Node != "" {
		parts = append(parts, task.Node)
	}
	return strings.Join(parts, ":")
}

func taskPayload(task Task) map[string]any {
	attributes := map[string]any{}
	if task.Status != "" {
		attributes["status"] = task.Status
	}
	if task.ID != "" {
		attributes["taskId"] = task.ID
	}
	if task.StartTime > 0 {
		attributes["startTime"] = time.Unix(task.StartTime, 0).UTC().Format(time.RFC3339)
	}
	if task.EndTime > 0 {
		attributes["endTime"] = time.Unix(task.EndTime, 0).UTC().Format(time.RFC3339)
	}
	return attributes
}

func emitTaskEvent(b *monitoring.Builder, kind string, task Task) {
	b.EventDetails(kind, taskDims(task), monitoring.EventDetails{
		Attributes:    taskPayload(task),
		ActorID:       task.User,
		CorrelationID: task.UPID,
	})
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

func collectCephAPI(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string, nodes []string) {
	b.State("proxmox.ceph.api.enabled", true, nil)
	status, err := getCephStatus(ctx, client)
	if err != nil {
		b.State("system.ceph.available", false, nil)
		if isCephUnavailable(err) {
			return
		}
		emitError(b, "proxmox.ceph.status.failed", "ceph_status", nil, err)
		return
	}
	cephID := entity.Key(status.FSID, clusterID)
	cb := monitoring.NewBuilder(cephClusterScope(scope, cephID))
	summary := status.summary()
	cb.State("system.ceph.available", true, nil)
	cb.State("system.ceph.present", true, nil)
	if status.FSID != "" {
		cb.State("system.ceph.fsid", status.FSID, nil)
	}
	if summary.Health != "" {
		cb.State("system.ceph.health.status", summary.Health, nil)
		cb.State("system.ceph.health.healthy", summary.Health == "HEALTH_OK", nil)
	}
	cb.Metric("system.ceph.osds.total", "gauge", float64(summary.OSDsTotal), "", nil)
	cb.Metric("system.ceph.osds.up", "gauge", float64(summary.OSDsUp), "", nil)
	cb.Metric("system.ceph.osds.in", "gauge", float64(summary.OSDsIn), "", nil)
	cb.Metric("system.ceph.pgs.total", "gauge", float64(summary.PGsTotal), "", nil)
	cb.Metric("system.ceph.bytes.used", "gauge", summary.BytesUsed, "bytes", nil)
	cb.Metric("system.ceph.bytes.total", "gauge", summary.BytesTotal, "bytes", nil)
	cb.Metric("system.ceph.bytes.available", "gauge", summary.BytesAvailable, "bytes", nil)
	for state, count := range summary.PGsByState {
		cb.Metric("system.ceph.pgs.by_state", "gauge", float64(count), "", map[string]string{"state": state})
	}
	b.Merge(cb.Batch())
	if len(nodes) > 0 {
		collectCephPools(ctx, b, client, scope, cephID, nodes)
	}
}

func getCephStatus(ctx context.Context, client Client) (CephStatus, error) {
	var status CephStatus
	if err := client.get(ctx, "/cluster/ceph/status", &status); err != nil {
		return CephStatus{}, err
	}
	return status, nil
}

func collectCephPools(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string, nodes []string) {
	var lastErr error
	for _, node := range nodes {
		var rows []map[string]any
		err := client.get(ctx, "/nodes/"+url.PathEscape(node)+"/ceph/pools", &rows)
		if err != nil {
			if isCephUnavailable(err) {
				return
			}
			lastErr = err
			continue
		}
		pools := parseCephPools(rows)
		cb := monitoring.NewBuilder(cephClusterScope(scope, clusterID))
		cb.Metric("system.ceph.pools", "gauge", float64(len(pools)), "", nil)
		b.Merge(cb.Batch())
		for _, pool := range pools {
			dims := map[string]string{"pool": pool.Name}
			pb := monitoring.NewBuilder(cephPoolScope(scope, clusterID, pool.Name))
			pb.State("system.ceph.pool.present", true, dims)
			pb.Metric("system.ceph.pool.bytes.used", "gauge", pool.BytesUsed, "bytes", dims)
			pb.Metric("system.ceph.pool.bytes.available", "gauge", pool.BytesAvailable, "bytes", dims)
			pb.Metric("system.ceph.pool.objects", "gauge", pool.Objects, "count", dims)
			b.Merge(pb.Batch())
		}
		return
	}
	if lastErr != nil {
		emitError(b, "proxmox.ceph.pools.failed", "ceph_pools", nil, lastErr)
	}
}

func isCephUnavailable(err error) bool {
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"ceph is not initialized",
		"ceph not configured",
		"ceph is not configured",
		"ceph.conf",
		"cluster not found",
		"no such file or directory",
		"no monitors specified",
		"not installed",
		"not initialized",
		"objectnotfound",
		"rados object not found",
		"unable to get monitor info",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

type CephStatus struct {
	FSID   string `json:"fsid"`
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
		case int64:
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

type NodeStatus struct {
	Node    string  `json:"node"`
	Status  string  `json:"status"`
	CPU     float64 `json:"cpu"`
	MaxCPU  float64 `json:"maxcpu"`
	Mem     float64 `json:"mem"`
	MaxMem  float64 `json:"maxmem"`
	Disk    float64 `json:"disk"`
	MaxDisk float64 `json:"maxdisk"`
	Uptime  float64 `json:"uptime"`
}
