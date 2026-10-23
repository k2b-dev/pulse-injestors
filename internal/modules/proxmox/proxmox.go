package proxmox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var ErrCommandNotFound = errors.New("proxmox command not found")

type Collector struct {
	PveshPath     string
	Timeout       time.Duration
	EnableCephAPI bool
	Runner        CommandRunner
	Part          string
	seenTasks     map[string]bool
	ListTimeout   time.Duration
	identity      *collectorIdentity
}

type collectorIdentity struct {
	sync.Mutex
	local, clusterID, cephID string
}

// Collectors separates health, capacity, backup and version sampling. Each phase
// rechecks election, so a newly elected node takes over without a process restart.
func (c Collector) Collectors() []monitoring.Collector {
	c.identity = &collectorIdentity{}
	var out []monitoring.Collector
	for _, phase := range []struct {
		name  string
		every time.Duration
	}{
		{"health", 60 * time.Second}, {"resources", 300 * time.Second},
		{"backups", 900 * time.Second}, {"version", 24 * time.Hour},
	} {
		part := c
		part.Part = phase.name
		out = append(out, monitoring.WithCadence{Collector: &part, Every: phase.every})
	}
	return out
}

func (c *Collector) Name() string {
	if c.Part == "" {
		return "proxmox"
	}
	return "proxmox." + c.Part
}

func (c *Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	if c.identity == nil {
		c.identity = &collectorIdentity{}
	}
	c.identity.Lock()
	local, clusterID := c.identity.local, c.identity.clusterID
	c.identity.Unlock()
	if local == "" {
		local = stableHostFromScope(scope)
	}
	b := monitoring.NewBuilder(nodeScope(scope, local, clusterID))
	client, err := c.client()
	if err != nil {
		b.State("proxmox.available", false, nil)
		if !errors.Is(err, ErrCommandNotFound) {
			emitError(b, "proxmox.config.failed", "configure", nil, err)
		}
		return b.Batch(), nil
	}
	rows, err := getClusterStatus(ctx, client)
	if err != nil {
		b.State("proxmox.available", false, nil)
		emitError(b, "proxmox.cluster.status.failed", "cluster_status", nil, err)
		// An API failure is not proof of a standalone node. Fail closed.
		return b.Batch(), nil
	}
	clusterID = clusterIDFromStatus(rows)
	local = stableHostFromScope(scope)
	for _, row := range rows {
		if row.Type == "node" && row.Local == 1 {
			local = row.Name
		}
	}
	c.identity.Lock()
	c.identity.local, c.identity.clusterID = local, clusterID
	c.identity.Unlock()
	b = monitoring.NewBuilder(nodeScope(scope, local, clusterID))
	b.State("proxmox.available", true, nil)
	quorate := clusterID == ""
	for _, row := range rows {
		if row.Type == "cluster" {
			quorate = row.Quorate == 1
		}
	}
	elected := quorate && electedNode(rows, local) == local
	var currentTasks map[string]bool
	phase := func(name string) bool { return c.Part == "" || c.Part == name }
	if phase("version") {
		if err := collectVersion(ctx, b, client); err != nil {
			emitError(b, "proxmox.version.failed", "version", nil, err)
		}
	}
	if phase("health") {
		online := clusterID == ""
		for _, row := range rows {
			if row.Type == "node" && row.Name == local {
				online = row.Online == 1
			}
		}
		b.State("proxmox.node.online", online, nil)
		b.State("proxmox.node.quorate", quorate, nil)
		status := "offline"
		if online {
			status = "online"
		}
		b.State("proxmox.node.status", status, nil)
		b.State("proxmox.cluster.elected", elected, nil)
	}
	if phase("resources") {
		collectStorage(ctx, b, client, scope, clusterID, local, elected)
	}
	if !elected {
		return b.Batch(), nil
	}
	aggregateScope := nodeScope(scope, local, clusterID)
	if clusterID != "" {
		aggregateScope = proxmoxClusterScope(scope, clusterID)
	}
	ab := monitoring.NewBuilder(aggregateScope)
	if phase("health") {
		emitClusterStatus(ab, scope, rows, clusterID)
		tasks, err := getClusterTasks(ctx, client)
		if err != nil {
			emitError(ab, "proxmox.cluster.tasks.failed", "cluster_tasks", nil, err)
		} else {
			currentTasks = map[string]bool{}
			for _, task := range tasks {
				currentTasks[task.UPID] = true
			}
			emitTaskSignals(ab, tasks)
			emitBackupTaskSignals(ab, tasks)
		}
		if c.EnableCephAPI {
			c.collectCephAPI(ctx, ab, client, scope, clusterID, []string{local})
		} else {
			ab.State("proxmox.ceph.api.enabled", false, nil)
		}
	}
	if phase("health") || phase("resources") || phase("backups") {
		var guests []Resource
		if err := client.get(ctx, "/cluster/resources", &guests); err != nil {
			emitError(ab, "proxmox.cluster.resources.failed", "cluster_resources", nil, err)
		} else {
			if phase("health") || phase("resources") {
				emitGuests(ab, scope, clusterID, guests, phase("resources"))
				if phase("health") {
					collectHA(ctx, ab, client, scope, clusterID, guests)
				}
			}
			if phase("backups") {
				collectBackups(ctx, ab, client, scope, clusterID, local, guests)
			}
		}
	}
	b.Merge(ab.Batch())
	batch := b.Batch()
	if c.seenTasks == nil {
		c.seenTasks = map[string]bool{}
	}
	// Mark seen only after the phase batch is built. The runner spools this batch;
	// loss is accepted only if subsequent validation rejects it.
	events := batch.Events[:0]
	counts := map[string]int{}
	for _, event := range batch.Events {
		if event.Kind == "proxmox.task.failed" || event.Kind == "proxmox.backup.failed" {
			key := event.Kind + ":" + event.CorrelationID
			if c.seenTasks[key] || counts[event.Kind] >= 10 {
				continue
			}
			counts[event.Kind]++
			if event.CorrelationID != "" {
				c.seenTasks[key] = true
			}
		}
		events = append(events, event)
	}
	batch.Events = events
	if currentTasks != nil {
		for key := range c.seenTasks {
			_, upid, _ := strings.Cut(key, ":")
			if !currentTasks[upid] {
				delete(c.seenTasks, key)
			}
		}
	}
	return batch, nil
}

// Missing IDs on any online candidate trigger a name-only election for all
// candidates, keeping the ordering deterministic on mixed/older responses.
func electedNode(rows []ClusterStatus, local string) string {
	var candidates []ClusterStatus
	byID := true
	for _, row := range rows {
		if row.Type == "node" && row.Name != "" && row.Online == 1 {
			candidates = append(candidates, row)
			if row.NodeID <= 0 {
				byID = false
			}
		}
	}
	if len(candidates) == 0 {
		if clusterIDFromStatus(rows) == "" {
			return local
		}
		return ""
	}
	sort.Slice(candidates, func(i, j int) bool {
		if byID && candidates[i].NodeID != candidates[j].NodeID {
			return candidates[i].NodeID < candidates[j].NodeID
		}
		return candidates[i].Name < candidates[j].Name
	})
	return candidates[0].Name
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
	listTimeout := c.ListTimeout
	if listTimeout <= 0 {
		listTimeout = 60 * time.Second
	}
	return Client{PveshPath: path, Timeout: timeout, ListTimeout: listTimeout, Runner: runner}, nil
}

type Client struct {
	ListTimeout time.Duration
	PveshPath   string
	Timeout     time.Duration
	Runner      CommandRunner
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := localexec.Output(ctx, 64<<20, name, args...)
	var exit *exec.ExitError
	if errors.As(err, &exit) && len(exit.Stderr) > 0 {
		stderr := exit.Stderr[:min(len(exit.Stderr), 1024)]
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(stderr)))
	}
	return out, err
}

func (c Client) get(ctx context.Context, path string, target any) error {
	args, err := pveshArgs(path)
	if err != nil {
		return err
	}
	timeout := c.Timeout
	apiPath, _, _ := strings.Cut(path, "?")
	if strings.Contains(apiPath, "/storage/") && strings.HasSuffix(apiPath, "/content") {
		timeout = c.ListTimeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
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
		b.State("proxmox.version", boundedText(version.Version), nil)
	}
	if version.Release != "" {
		b.State("proxmox.release", boundedText(version.Release), nil)
	}
	if version.RepoID != "" {
		b.State("proxmox.repoid", boundedText(version.RepoID), nil)
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

func emitClusterStatus(b *monitoring.Builder, scope monitoring.Scope, rows []ClusterStatus, clusterID string) {
	total, online := 0, 0
	for _, row := range rows {
		if row.Type == "cluster" && clusterID != "" {
			b.State("proxmox.cluster.name", boundedText(row.Name), nil)
			b.State("proxmox.cluster.quorate", row.Quorate == 1, nil)
		}
		if row.Type == "node" {
			total++
			if row.Online == 1 {
				online++
			}
		}
	}
	b.Metric("proxmox.nodes.total", "gauge", float64(total), "", nil)
	b.Metric("proxmox.nodes.online", "gauge", float64(online), "", nil)
}

func emitGuests(b *monitoring.Builder, scope monitoring.Scope, clusterID string, rows []Resource, metrics bool) {
	for _, row := range rows {
		if (row.Type != "qemu" && row.Type != "lxc") || row.Template == 1 {
			continue
		}
		gs, ok := proxmoxResourceScope(scope, row, clusterID)
		if !ok {
			continue
		}
		gb := monitoring.NewBuilder(gs)
		gb.State("proxmox.guest.status", boundedText(row.Status), nil)
		gb.State("proxmox.guest.node", boundedText(row.Node), nil)
		if metrics {
			gb.Metric("proxmox.guest.cpu.usage", "gauge", row.CPU*100, "percent", nil)
			gb.Metric("proxmox.guest.memory.used", "gauge", row.Mem, "bytes", nil)
			gb.Metric("proxmox.guest.memory.max", "gauge", row.MaxMem, "bytes", nil)
			gb.Metric("proxmox.guest.disk.read", "counter", row.DiskRead, "bytes", nil)
			gb.Metric("proxmox.guest.disk.write", "counter", row.DiskWrite, "bytes", nil)
			gb.Metric("proxmox.guest.net.in", "counter", row.NetIn, "bytes", nil)
			gb.Metric("proxmox.guest.net.out", "counter", row.NetOut, "bytes", nil)
			gb.Metric("proxmox.guest.uptime", "gauge", row.Uptime, "seconds", nil)
		}
		b.Merge(gb.Batch())
	}
}

func collectHA(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string, guests []Resource) {
	var rows []struct {
		Type   string `json:"type"`
		SID    string `json:"sid"`
		Status string `json:"status"`
		State  string `json:"state"`
	}
	if err := client.get(ctx, "/cluster/ha/status/current", &rows); err != nil {
		emitError(b, "proxmox.ha.failed", "ha", nil, err)
		return
	}
	for _, row := range rows {
		if row.Type != "service" {
			continue
		}
		typ, id, ok := strings.Cut(row.SID, ":")
		if !ok || (typ != "vm" && typ != "ct") {
			continue
		}
		var gs monitoring.Scope
		found := false
		for _, guest := range guests {
			if guestVMID(guest) == id && guest.Template != 1 {
				gs, found = proxmoxResourceScope(scope, guest, clusterID)
				break
			}
		}
		if !found {
			continue
		}
		gb := monitoring.NewBuilder(gs)
		gb.State("proxmox.guest.ha.state", boundedText(haState(row.State, row.Status)), nil)
		b.Merge(gb.Batch())
	}
}

type Storage struct {
	Storage string  `json:"storage"`
	Type    string  `json:"type"`
	Shared  int     `json:"shared"`
	Enabled int     `json:"enabled"`
	Active  int     `json:"active"`
	Used    float64 `json:"used"`
	Total   float64 `json:"total"`
	Avail   float64 `json:"avail"`
}

func collectStorage(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID, local string, elected bool) {
	var rows []Storage
	if err := client.get(ctx, "/nodes/"+url.PathEscape(local)+"/storage", &rows); err != nil {
		emitError(b, "proxmox.storage.failed", "storage", nil, err)
		return
	}
	for _, row := range rows {
		if row.Storage == "" || (row.Shared == 1 && !elected) {
			continue
		}
		ss := scope
		ss.Resource.Type = "proxmox-storage"
		ss.Resource.ID = entity.ID(clusterNamespace(scope, clusterID), local, row.Storage)
		if row.Shared == 1 {
			ss.Resource.ID = entity.ID(clusterNamespace(scope, clusterID), row.Storage)
		}
		ss.Resource.Label = row.Storage
		sb := monitoring.NewBuilder(ss)
		sb.State("proxmox.storage.active", row.Active == 1, nil)
		sb.State("proxmox.storage.enabled", row.Enabled == 1, nil)
		if row.Active == 1 && row.Enabled == 1 {
			sb.Metric("proxmox.storage.bytes.used", "gauge", row.Used, "bytes", nil)
			sb.Metric("proxmox.storage.bytes.total", "gauge", row.Total, "bytes", nil)
			sb.Metric("proxmox.storage.bytes.available", "gauge", row.Avail, "bytes", nil)
		}
		b.Merge(sb.Batch())
	}
}

func nodeScope(scope monitoring.Scope, node, clusterID string) monitoring.Scope {
	scope.Resource.Type = "proxmox-node"
	scope.Resource.ID = entity.ID(clusterNamespace(scope, clusterID), node)
	scope.Resource.Label = node
	return scope
}

func proxmoxClusterScope(scope monitoring.Scope, clusterID string) monitoring.Scope {
	scope.Resource.Type = "proxmox-cluster"
	scope.Resource.ID = entity.ID(clusterNamespace(scope, clusterID))
	scope.Resource.Label = clusterID
	return scope
}

func proxmoxResourceScope(scope monitoring.Scope, row Resource, clusterID string) (monitoring.Scope, bool) {
	vmid := guestVMID(row)
	if vmid == "" {
		return monitoring.Scope{}, false
	}
	scope.Resource = pulse.ResourceRef{Type: "proxmox-guest", ID: entity.ID(clusterNamespace(scope, clusterID), vmid), Label: labelValue(row.Name, vmid)}
	return scope, true
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
	return entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions)
}

func clusterNamespace(scope monitoring.Scope, clusterID string) string {
	if v := entity.Key(clusterID, ""); v != "" {
		return v
	}
	return stableHostFromScope(scope)
}

func backupJobScope(scope monitoring.Scope, clusterID, job string) monitoring.Scope {
	scope.Resource.Type = "proxmox-backup-job"
	scope.Resource.ID = entity.ID(clusterNamespace(scope, clusterID), entity.Key(job, "unknown"))
	scope.Resource.Label = labelValue(job, "unknown")
	return scope
}

func cephClusterScope(scope monitoring.Scope, clusterID string) monitoring.Scope {
	scope.Resource.Type = "ceph-cluster"
	namespace := clusterNamespace(scope, clusterID)
	scope.Resource.ID = entity.ID(namespace)
	scope.Resource.Label = "Ceph " + namespace
	return scope
}

func cephPoolScope(scope monitoring.Scope, clusterID, pool string) monitoring.Scope {
	scope.Resource.Type = "ceph-pool"
	scope.Resource.ID = entity.ID(clusterNamespace(scope, clusterID), entity.Key(pool, "unknown"))
	scope.Resource.Label = labelValue(pool, "unknown")
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
		Attributes: map[string]any{"error": boundedText(err.Error())},
	})
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
	b.Metric("proxmox.tasks.recent", "gauge", float64(len(tasks)), "", nil)
	for _, task := range tasks {
		status := taskStatusClass(task)
		typ := task.Type
		if typ == "" {
			typ = "unknown"
		}
		byStatus[status]++
		byTypeStatus[typ+":"+status]++
		if status == "error" {
			emitTaskEvent(b, "proxmox.task.failed", task)
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
	for _, task := range tasks {
		if task.Type != "vzdump" {
			continue
		}
		total++
		switch taskStatusClass(task) {
		case "ok":
			success++
		case "error":
			failed++
			emitTaskEvent(b, "proxmox.backup.failed", task)
		}
	}
	b.Metric("proxmox.backup.tasks.recent", "gauge", float64(total), "", nil)
	b.Metric("proxmox.backup.tasks.success", "gauge", float64(success), "", nil)
	b.Metric("proxmox.backup.tasks.failed", "gauge", float64(failed), "", nil)

}

func collectBackups(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID, local string, guests []Resource) {
	var rows []map[string]any
	if err := client.get(ctx, "/cluster/backup", &rows); err != nil {
		emitError(b, "proxmox.backup.jobs.failed", "backup_jobs", nil, err)
	} else {
		jobs := parseBackupJobs(rows)
		for _, guest := range guests {
			if guest.Template == 1 || guestVMID(guest) == "" {
				continue
			}
			ids := []string{}
			for _, job := range jobs {
				if job.selects(guest) {
					ids = append(ids, job.ID)
				}
			}
			sort.Strings(ids)
			gs, _ := proxmoxResourceScope(scope, guest, clusterID)
			gb := monitoring.NewBuilder(gs)
			gb.State("proxmox.backup.guest.covered", len(ids) > 0, nil)
			gb.State("proxmox.backup.guest.jobs", boundedText(strings.Join(ids, ",")), nil)
			b.Merge(gb.Batch())
		}
		for _, job := range jobs {
			jb := monitoring.NewBuilder(backupJobScope(scope, clusterID, job.ID))
			jb.State("proxmox.backup.job.enabled", job.Enabled, nil)
			jb.State("proxmox.backup.job.schedule", boundedText(job.Schedule), nil)
			jb.State("proxmox.backup.job.storage", boundedText(job.Storage), nil)
			b.Merge(jb.Batch())
		}
	}
	var stores []Storage
	if err := client.get(ctx, "/nodes/"+url.PathEscape(local)+"/storage", &stores); err != nil {
		emitError(b, "proxmox.storage.failed", "storage", nil, err)
		return
	}
	for _, store := range stores {
		if store.Type != "pbs" || store.Enabled != 1 || store.Active != 1 {
			continue
		}
		var content []struct {
			VMID  int   `json:"vmid"`
			CTime int64 `json:"ctime"`
		}
		dims := map[string]string{"storage": store.Storage}
		if err := client.get(ctx, "/nodes/"+url.PathEscape(local)+"/storage/"+url.PathEscape(store.Storage)+"/content?content=backup", &content); err != nil {
			emitError(b, "proxmox.backup.content.failed", "backup_content", dims, err)
			continue
		}
		newest := map[string]int64{}
		for _, item := range content {
			id := strconv.Itoa(item.VMID)
			if item.CTime > newest[id] {
				newest[id] = item.CTime
			}
		}
		for _, guest := range guests {
			if guest.Template == 1 || guestVMID(guest) == "" {
				continue
			}
			gs, _ := proxmoxResourceScope(scope, guest, clusterID)
			gb := monitoring.NewBuilder(gs)
			ts := newest[guestVMID(guest)]
			value := ""
			if ts > 0 {
				value = time.Unix(ts, 0).UTC().Format(time.RFC3339)
				gb.Metric("proxmox.backup.last_success.age", "gauge", max(0, scope.Timestamp.Sub(time.Unix(ts, 0)).Seconds()), "seconds", dims)
			}
			gb.State("proxmox.backup.last_success.time", value, dims)
			b.Merge(gb.Batch())
		}
	}
}

func (j BackupJob) selects(g Resource) bool {
	id := guestVMID(g)
	contains := func(list, value string) bool {
		for _, v := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n' }) {
			if v == value {
				return true
			}
		}
		return false
	}
	return j.Enabled && (j.Node == "" || j.Node == g.Node) && !contains(j.Exclude, id) && (j.All || contains(j.VMIDs, id) || (j.Pool != "" && j.Pool == g.Pool))
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

func taskPayload(task Task) map[string]any {
	attributes := map[string]any{}
	if task.Status != "" {
		attributes["status"] = boundedText(task.Status)
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
	ID                         string
	Enabled                    bool
	Schedule                   string
	Storage                    string
	Mode                       string
	All                        bool
	VMIDs, Pool, Exclude, Node string
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
			All:      firstBool(row, false, "all"), VMIDs: firstString(row, "vmid"), Pool: firstString(row, "pool"), Exclude: firstString(row, "exclude"), Node: firstString(row, "node"),
		})
	}
	return jobs
}

func (c *Collector) collectCephAPI(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope, clusterID string, nodes []string) {
	b.State("proxmox.ceph.api.enabled", true, nil)
	status, err := getCephStatus(ctx, client)
	if err != nil {
		c.identity.Lock()
		cephID := c.identity.cephID
		c.identity.Unlock()
		fb := b
		if cephID != "" {
			fb = monitoring.NewBuilder(cephClusterScope(scope, cephID))
		}
		fb.State("system.ceph.available", false, nil)
		if fb != b {
			b.Merge(fb.Batch())
		}
		if isCephUnavailable(err) {
			return
		}
		emitError(b, "proxmox.ceph.status.failed", "ceph_status", nil, err)
		return
	}
	cephID := entity.Key(status.FSID, clusterID)
	c.identity.Lock()
	c.identity.cephID = cephID
	c.identity.Unlock()
	cb := monitoring.NewBuilder(cephClusterScope(scope, cephID))
	summary := status.summary()
	cb.State("system.ceph.available", true, nil)
	cb.State("system.ceph.present", true, nil)
	if status.FSID != "" {
		cb.State("system.ceph.fsid", boundedText(status.FSID), nil)
	}
	if summary.Health != "" {
		cb.State("system.ceph.health.status", boundedText(summary.Health), nil)
		cb.State("system.ceph.health.healthy", summary.Health == "HEALTH_OK", nil)
	}
	cb.Metric("system.ceph.osds.total", "gauge", float64(summary.OSDsTotal), "", nil)
	cb.Metric("system.ceph.osds.up", "gauge", float64(summary.OSDsUp), "", nil)
	cb.Metric("system.ceph.osds.in", "gauge", float64(summary.OSDsIn), "", nil)
	cb.Metric("system.ceph.pgs.total", "gauge", float64(summary.PGsTotal), "", nil)
	cb.Metric("system.ceph.bytes.used", "gauge", summary.BytesUsed, "bytes", nil)
	cb.Metric("system.ceph.bytes.total", "gauge", summary.BytesTotal, "bytes", nil)
	cb.Metric("system.ceph.bytes.available", "gauge", summary.BytesAvailable, "bytes", nil)
	cb.Metric("system.ceph.pgs.not_active_clean", "gauge", float64(summary.PGsTotal-summary.PGsByState["active+clean"]), "", nil)
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
			pb.Metric("system.ceph.pool.objects", "gauge", pool.Objects, "", dims)
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
		NumOSDs   int `json:"num_osds"`
		NumUpOSDs int `json:"num_up_osds"`
		NumInOSDs int `json:"num_in_osds"`
		OSDMap    struct {
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
	if s.OSDMap.NumOSDs > 0 {
		out.OSDsTotal = s.OSDMap.NumOSDs
		out.OSDsUp = s.OSDMap.NumUpOSDs
		out.OSDsIn = s.OSDMap.NumInOSDs
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
	NodeID  int    `json:"nodeid"`
	Local   int    `json:"local"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Online  int    `json:"online"`
	Quorate int    `json:"quorate"`
}

type Resource struct {
	Template  int     `json:"template"`
	Pool      string  `json:"pool"`
	DiskRead  float64 `json:"diskread"`
	DiskWrite float64 `json:"diskwrite"`
	NetIn     float64 `json:"netin"`
	NetOut    float64 `json:"netout"`
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Node      string  `json:"node"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	CPU       float64 `json:"cpu"`
	MaxCPU    float64 `json:"maxcpu"`
	Mem       float64 `json:"mem"`
	MaxMem    float64 `json:"maxmem"`
	Disk      float64 `json:"disk"`
	MaxDisk   float64 `json:"maxdisk"`
	Uptime    float64 `json:"uptime"`
}

func haState(state, status string) string {
	if state != "" {
		return state
	}
	if _, tail, ok := strings.Cut(status, ","); ok && strings.HasSuffix(status, ")") {
		status = strings.TrimSpace(strings.TrimSuffix(tail, ")"))
	}
	if len(strings.Fields(status)) == 1 {
		return status
	}
	return "unknown"
}

func boundedText(value string) string {
	runes := []rune(value)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return value
}
