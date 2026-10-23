package pbs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/localexec"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var ErrCommandNotFound = errors.New("pbs command not found")

type Collector struct {
	ListTimeout time.Duration
	seenTasks   map[string]bool
	seenJobs    map[string]bool
	CommandPath string
	Timeout     time.Duration
	Runner      CommandRunner
	Part        string
}

func (c Collector) Name() string {
	if c.Part == "" {
		return "pbs"
	}
	return "pbs." + c.Part
}

func (c Collector) Collectors() []monitoring.Collector {
	var out []monitoring.Collector
	for _, phase := range []struct {
		name  string
		every time.Duration
	}{
		{"datastores", 300 * time.Second}, {"jobs", 900 * time.Second}, {"groups", 900 * time.Second}, {"version", 24 * time.Hour},
	} {
		part := c
		part.Part = phase.name
		out = append(out, monitoring.WithCadence{Collector: &part, Every: phase.every})
	}
	return out
}

func (c *Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	client, err := c.client()
	if err != nil {
		b.State("pbs.available", false, nil)
		if !errors.Is(err, ErrCommandNotFound) {
			emitError(b, "pbs.config.failed", "configure", nil, err)
		}
		return b.Batch(), nil
	}
	phase := func(name string) bool { return c.Part == "" || c.Part == name }
	if phase("version") {
		if err := collectVersion(ctx, b, client); err != nil {
			b.State("pbs.available", false, nil)
			emitError(b, "pbs.version.failed", "version", nil, err)
		} else {
			b.State("pbs.available", true, nil)
		}
	}
	if phase("datastores") {
		if _, err := collectDatastoreUsage(ctx, b, client, scope); err != nil {
			b.State("pbs.available", false, nil)
			emitError(b, "pbs.datastore.usage.failed", "datastore_usage", nil, err)
		} else {
			b.State("pbs.available", true, nil)
		}
	}
	if phase("groups") {
		var stores []struct {
			Store string `json:"store"`
		}
		if err := client.get(ctx, "/admin/datastore", &stores); err != nil {
			emitError(b, "pbs.datastore.list.failed", "datastore_list", nil, err)
		} else {
			sort.Slice(stores, func(i, j int) bool { return stores[i].Store < stores[j].Store })
			remaining := 2000
			truncated := false
			for _, store := range stores {
				if err := collectSnapshots(ctx, b, client, store.Store, scope, &remaining, &truncated); err != nil {
					db := monitoring.NewBuilder(datastoreScope(scope, store.Store))
					emitError(db, "pbs.datastore.snapshots.failed", "snapshots", map[string]string{"datastore": store.Store}, err)
					b.Merge(db.Batch())
				}
			}
			b.State("pbs.groups.truncated", truncated, nil)
		}
	}
	if phase("jobs") {
		currentJobs, jobsOK := collectJobs(ctx, b, client, scope)
		currentTasks := collectTasks(ctx, b, client)
		batch := b.Batch()
		c.deduplicate(&batch, currentJobs, jobsOK, currentTasks)
		return batch, nil
	}

	return b.Batch(), nil
}

func (c Collector) client() (Client, error) {
	path := strings.TrimSpace(c.CommandPath)
	if path == "" {
		path = "proxmox-backup-debug"
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
	return Client{CommandPath: path, Timeout: timeout, ListTimeout: listTimeout, Runner: runner}, nil
}

type Client struct {
	ListTimeout time.Duration
	CommandPath string
	Timeout     time.Duration
	Runner      CommandRunner
	Part        string
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
	args, err := debugAPIArgs(path)
	if err != nil {
		return err
	}
	timeout := c.Timeout
	apiPath, _, _ := strings.Cut(path, "?")
	if strings.HasPrefix(apiPath, "/admin/datastore/") && (strings.HasSuffix(apiPath, "/snapshots") || strings.HasSuffix(apiPath, "/groups") || strings.HasSuffix(apiPath, "/namespace")) {
		timeout = c.ListTimeout
		if timeout <= 0 {
			timeout = 60 * time.Second
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	out, err := c.Runner.Run(runCtx, c.CommandPath, args...)
	if err != nil {
		return err
	}
	out = []byte(strings.TrimSpace(string(out)))
	if len(out) == 0 || string(out) == "null" {
		return nil
	}
	return json.Unmarshal(out, target)
}

func debugAPIArgs(path string) ([]string, error) {
	cleanPath, query, err := splitAPIPath(path)
	if err != nil {
		return nil, err
	}
	args := []string{"api", "get", cleanPath}
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
		b.State("pbs.version", boundedText(version.Version), nil)
	}
	if version.Release != "" {
		b.State("pbs.release", boundedText(version.Release), nil)
	}
	if version.RepoID != "" {
		b.State("pbs.repoid", boundedText(version.RepoID), nil)
	}
	return nil
}

func collectDatastoreUsage(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope) ([]string, error) {
	var rows []DatastoreUsage
	if err := client.get(ctx, "/status/datastore-usage", &rows); err != nil {
		return nil, err
	}
	stores := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Store == "" {
			continue
		}
		stores = append(stores, row.Store)
		dims := map[string]string{"datastore": row.Store}
		db := monitoring.NewBuilder(datastoreScope(scope, row.Store))
		db.State("pbs.datastore.present", true, dims)
		if row.Error != "" {
			emitError(db, "pbs.datastore.usage.failed", "datastore_usage", dims, errors.New(row.Error))
			b.Merge(db.Batch())
			continue
		}
		emitUsage(db, "pbs.datastore.bytes", row.Used, row.Total, dims)
		if row.Available >= 0 {
			db.Metric("pbs.datastore.bytes.available", "gauge", row.Available, "bytes", dims)
		}
		if row.EstimatedFullDate > 0 {
			ts := time.Unix(row.EstimatedFullDate, 0).UTC()
			db.State("pbs.datastore.estimated_full.time", ts.Format(time.RFC3339), dims)
			db.Metric("pbs.datastore.estimated_full.seconds_until", "gauge", ts.Sub(scope.Timestamp).Seconds(), "seconds", dims)
		}
		b.Merge(db.Batch())
	}
	b.Metric("pbs.datastores.total", "gauge", float64(len(stores)), "", nil)
	return stores, nil
}

func collectSnapshots(ctx context.Context, b *monitoring.Builder, client Client, store string, scope monitoring.Scope, remaining *int, truncated *bool) error {
	base := "/admin/datastore/" + url.PathEscape(store)
	var namespaces []struct {
		NS string `json:"ns"`
	}
	// PBS max-depth=7 walks all supported namespace levels, starting at root.
	if err := client.get(ctx, base+"/namespace?max-depth=7", &namespaces); err != nil {
		return err
	}
	names := map[string]bool{"": true}
	for _, ns := range namespaces {
		names[ns.NS] = true
	}
	ordered := make([]string, 0, len(names))
	for ns := range names {
		ordered = append(ordered, ns)
	}
	sort.Strings(ordered)
	byType := map[string]int{}
	failedVerify, snapshots := 0, 0
	var newest int64
	for _, ns := range ordered {
		query := ""
		if ns != "" {
			query = "?" + url.Values{"ns": {ns}}.Encode()
		}
		var groups []struct {
			BackupType string `json:"backup-type"`
			BackupID   string `json:"backup-id"`
			LastBackup int64  `json:"last-backup"`
		}
		if err := client.get(ctx, base+"/groups"+query, &groups); err != nil {
			return err
		}
		sort.Slice(groups, func(i, j int) bool {
			return groups[i].BackupType+"/"+groups[i].BackupID < groups[j].BackupType+"/"+groups[j].BackupID
		})
		for _, group := range groups {
			if group.BackupType == "" || group.BackupID == "" {
				continue
			}
			if group.LastBackup > newest {
				newest = group.LastBackup
			}
			if *remaining <= 0 {
				*truncated = true
				continue
			}
			*remaining--
			name := group.BackupType + "/" + group.BackupID
			gs := scope
			gs.Resource = pulse.ResourceRef{Type: "pbs-backup-group", ID: entity.ID(stableHostFromScope(scope), store, name), Label: name + " on " + store}
			dims := map[string]string{"datastore": store}
			if ns != "" {
				gs.Resource.ID = entity.ID(stableHostFromScope(scope), store, ns, name)
				gs.Resource.Label = name + " in " + ns + " on " + store
				dims["namespace"] = ns
			}
			gb := monitoring.NewBuilder(gs)
			value := ""
			if group.LastBackup > 0 {
				ts := time.Unix(group.LastBackup, 0).UTC()
				value = ts.Format(time.RFC3339)
				gb.Metric("pbs.group.last_snapshot.age", "gauge", max(0, scope.Timestamp.Sub(ts).Seconds()), "seconds", dims)
			}
			gb.State("pbs.group.last_snapshot.time", value, dims)
			b.Merge(gb.Batch())
		}
		var rows []Snapshot
		if err := client.get(ctx, base+"/snapshots"+query, &rows); err != nil {
			return err
		}
		snapshots += len(rows)
		for _, row := range rows {
			if row.Verification.State == "failed" {
				failedVerify++
			}
			typ := row.BackupType
			if typ == "" {
				typ = "unknown"
			}
			byType[typ]++
		}
	}
	dims := map[string]string{"datastore": store}
	db := monitoring.NewBuilder(datastoreScope(scope, store))
	db.Metric("pbs.datastore.verify.failed_snapshots", "gauge", float64(failedVerify), "", dims)
	db.Metric("pbs.datastore.snapshots.total", "gauge", float64(snapshots), "", dims)
	for typ, count := range byType {
		db.Metric("pbs.datastore.snapshots.by_type", "gauge", float64(count), "", map[string]string{"datastore": store, "type": typ})
	}
	if newest > 0 {
		ts := time.Unix(newest, 0).UTC()
		db.State("pbs.datastore.snapshot.latest.time", ts.Format(time.RFC3339), dims)
		db.Metric("pbs.datastore.snapshot.latest.age", "gauge", max(0, scope.Timestamp.Sub(ts).Seconds()), "seconds", dims)
	}
	b.Merge(db.Batch())
	return nil
}

func collectJobs(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope) (map[string]bool, bool) {
	current := map[string]bool{}
	complete := true
	for _, spec := range []struct {
		Kind string
		Path string
	}{
		{Kind: "gc", Path: "/admin/gc"},
		{Kind: "prune", Path: "/admin/prune"},
		{Kind: "sync", Path: "/admin/sync"},
		{Kind: "verify", Path: "/admin/verify"},
		{Kind: "tape", Path: "/tape/backup"},
	} {
		var rows []map[string]any
		if err := client.get(ctx, spec.Path, &rows); err != nil {
			complete = false
			emitError(b, "pbs.job.list.failed", "job_list", map[string]string{"kind": spec.Kind}, err)
			continue
		}
		jobs := parseJobs(rows)
		for _, job := range jobs {
			current[job.LastRunUPID] = true
		}
		if spec.Kind == "gc" {
			for _, job := range jobs {
				db := monitoring.NewBuilder(datastoreScope(scope, job.Store))
				dims := map[string]string{"datastore": job.Store}
				db.State("pbs.datastore.gc.last_run.state", boundedText(job.LastRunState), dims)
				db.State("pbs.datastore.gc.last_run.result", jobResult(job.LastRunState), dims)
				value := ""
				if job.LastRunEndTime > 0 {
					ts := time.Unix(job.LastRunEndTime, 0).UTC()
					value = ts.Format(time.RFC3339)
					db.Metric("pbs.datastore.gc.last_run.age", "gauge", max(0, scope.Timestamp.Sub(ts).Seconds()), "seconds", dims)
				}
				db.State("pbs.datastore.gc.last_run.time", value, dims)
				b.Merge(db.Batch())
			}
		} else {
			emitJobs(b, scope, spec.Kind, jobs)
		}
	}
	return current, complete
}

func emitJobs(b *monitoring.Builder, scope monitoring.Scope, kind string, jobs []Job) {
	enabled := 0
	for _, job := range jobs {
		dims := map[string]string{"kind": kind, "job": job.ID}
		addDim(dims, "datastore", job.Store)
		jb := monitoring.NewBuilder(jobScope(scope, kind, job.ID))
		jb.State("pbs.job.present", true, dims)
		jb.State("pbs.job.enabled", !job.Disabled, dims)
		if !job.Disabled {
			enabled++
		}
		if job.Schedule != "" {
			jb.State("pbs.job.schedule", boundedText(job.Schedule), dims)
		}
		jb.State("pbs.job.last_run.result", jobResult(job.LastRunState), dims)
		if job.LastRunState != "" {
			jb.State("pbs.job.last_run.state", boundedText(job.LastRunState), dims)
			if job.LastRunEndTime > 0 && jobResult(job.LastRunState) == "error" {
				jb.EventDetails("pbs.job.failed", monitoring.MergeDimensions(dims, map[string]string{"status": "error"}), monitoring.EventDetails{
					CorrelationID: job.LastRunUPID,
					Attributes:    map[string]any{"state": boundedText(job.LastRunState)},
				})
			}
		}
		if job.LastRunEndTime > 0 {
			ts := time.Unix(job.LastRunEndTime, 0).UTC()
			jb.State("pbs.job.last_run.time", ts.Format(time.RFC3339), dims)
			jb.Metric("pbs.job.last_run.age", "gauge", max(0, scope.Timestamp.Sub(ts).Seconds()), "seconds", dims)
		}
		if job.NextRun > 0 {
			ts := time.Unix(job.NextRun, 0).UTC()
			jb.State("pbs.job.next_run.time", ts.Format(time.RFC3339), dims)
			jb.Metric("pbs.job.next_run.seconds_until", "gauge", ts.Sub(scope.Timestamp).Seconds(), "seconds", dims)
		}
		b.Merge(jb.Batch())
	}
	b.Metric("pbs.jobs.total", "gauge", float64(len(jobs)), "", map[string]string{"kind": kind})
	b.Metric("pbs.jobs.enabled", "gauge", float64(enabled), "", map[string]string{"kind": kind})
}

func collectTasks(ctx context.Context, b *monitoring.Builder, client Client) map[string]bool {
	var rows []Task
	if err := client.get(ctx, "/nodes/localhost/tasks?limit=50", &rows); err != nil {
		emitError(b, "pbs.tasks.failed", "tasks", nil, err)
		return nil
	}
	byStatus := map[string]int{}
	byTypeStatus := map[string]int{}
	current := map[string]bool{}
	b.Metric("pbs.tasks.recent", "gauge", float64(len(rows)), "", nil)
	for _, task := range rows {
		current[task.UPID] = true
		status := taskStatusClass(task)
		typ := task.WorkerType
		if typ == "" {
			typ = "unknown"
		}
		byStatus[status]++
		byTypeStatus[typ+":"+status]++
		if status == "error" {
			emitTaskEvent(b, "pbs.task.failed", task)
		}
	}
	for status, count := range byStatus {
		b.Metric("pbs.tasks.by_status", "gauge", float64(count), "", map[string]string{"status": status})
	}
	for key, count := range byTypeStatus {
		typ, status, _ := strings.Cut(key, ":")
		b.Metric("pbs.tasks.by_type_status", "gauge", float64(count), "", map[string]string{"type": typ, "status": status})
	}
	return current
}

type DatastoreUsage struct {
	Store             string  `json:"store"`
	Error             string  `json:"error"`
	Used              float64 `json:"used"`
	Total             float64 `json:"total"`
	Available         float64 `json:"avail"`
	EstimatedFullDate int64   `json:"estimated-full-date"`
}

type Snapshot struct {
	BackupType   string `json:"backup-type"`
	BackupID     string `json:"backup-id"`
	BackupTime   int64  `json:"backup-time"`
	Verification struct {
		State string `json:"state"`
	} `json:"verification"`
}

type Job struct {
	ID             string
	Store          string
	Disabled       bool
	Schedule       string
	LastRunState   string
	LastRunUPID    string
	LastRunEndTime int64
	NextRun        int64
}

func parseJobs(rows []map[string]any) []Job {
	jobs := make([]Job, 0, len(rows))
	for _, row := range rows {
		id := firstString(row, "id", "store")
		if id == "" {
			continue
		}
		jobs = append(jobs, Job{
			ID:             id,
			Store:          firstString(row, "store", "remote-store"),
			Disabled:       firstBool(row, false, "disable"),
			Schedule:       firstString(row, "schedule"),
			LastRunState:   firstString(row, "last-run-state"),
			LastRunUPID:    firstString(row, "last-run-upid"),
			LastRunEndTime: int64(firstFloat(row, "last-run-endtime")),
			NextRun:        int64(firstFloat(row, "next-run")),
		})
	}
	return jobs
}

type Task struct {
	UPID       string `json:"upid"`
	Node       string `json:"node"`
	WorkerType string `json:"worker_type"`
	WorkerID   string `json:"worker_id"`
	User       string `json:"user"`
	Status     string `json:"status"`
	StartTime  int64  `json:"starttime"`
	EndTime    int64  `json:"endtime"`
}

func taskStatusClass(task Task) string {
	if task.EndTime == 0 && task.Status == "" {
		return "running"
	}
	if task.Status == "OK" || task.Status == "ok" {
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
	addDim(dims, "type", task.WorkerType)
	addDim(dims, "status", taskStatusClass(task))
	return dims
}

func taskSubject(prefix string, task Task) string {
	parts := []string{prefix}
	if task.WorkerType != "" {
		parts = append(parts, task.WorkerType)
	}
	if task.WorkerID != "" {
		parts = append(parts, task.WorkerID)
	} else if task.Node != "" {
		parts = append(parts, task.Node)
	}
	return strings.Join(parts, ":")
}

func taskPayload(task Task) map[string]any {
	attributes := map[string]any{}
	if task.Status != "" {
		attributes["status"] = boundedText(task.Status)
	}
	if task.WorkerID != "" {
		attributes["workerId"] = task.WorkerID
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

func emitUsage(b *monitoring.Builder, prefix string, used, max float64, dims map[string]string) {
	if used >= 0 {
		b.Metric(prefix+".used", "gauge", used, "bytes", dims)
	}
	if max > 0 {
		b.Metric(prefix+".total", "gauge", max, "bytes", dims)
		if used >= 0 {
			b.Metric(prefix+".usage", "gauge", (used/max)*100, "percent", dims)
		}
	}
}

func datastoreScope(scope monitoring.Scope, datastore string) monitoring.Scope {
	scope.Resource.Type = "pbs-datastore"
	scope.Resource.ID = entity.ID(stableHostFromScope(scope), datastore)
	scope.Resource.Label = datastore
	return scope
}

func jobScope(scope monitoring.Scope, kind, job string) monitoring.Scope {
	scope.Resource.Type = "pbs-job"
	scope.Resource.ID = entity.ID(stableHostFromScope(scope), entity.Key(kind, "unknown"), entity.Key(job, "unknown"))
	scope.Resource.Label = labelValue(job, "unknown")
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

func stableHostFromScope(scope monitoring.Scope) string {
	return entity.StableHostIDFromScope(scope.Resource.ID, scope.Dimensions)
}

func emitError(b *monitoring.Builder, kind, operation string, dims map[string]string, err error) {
	b.EventDetails(kind, monitoring.MergeDimensions(dims, map[string]string{"operation": operation}), monitoring.EventDetails{
		Attributes: map[string]any{"error": boundedText(err.Error())},
	})
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

func jobResult(state string) string {
	state = strings.TrimSpace(state)
	switch {
	case state == "":
		return "unknown"
	case strings.EqualFold(state, "OK"):
		return "ok"
	case strings.HasPrefix(state, "WARNINGS:"):
		return "warnings"
	default:
		return "error"
	}
}

func boundedText(value string) string {
	runes := []rune(value)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return value
}

func (c *Collector) deduplicate(batch *pulse.Batch, jobs map[string]bool, jobsOK bool, tasks map[string]bool) {
	if c.seenJobs == nil {
		c.seenJobs = map[string]bool{}
	}
	if c.seenTasks == nil {
		c.seenTasks = map[string]bool{}
	}
	events := batch.Events[:0]
	count := 0
	// Mark seen after the phase batch is built. Subsequent validation failure is
	// the only accepted loss; normal delivery retries operate on the spooled batch.
	for _, event := range batch.Events {
		var seen map[string]bool
		switch event.Kind {
		case "pbs.job.failed":
			seen = c.seenJobs
		case "pbs.task.failed":
			seen = c.seenTasks
		}
		if seen != nil {
			if event.CorrelationID != "" && seen[event.CorrelationID] {
				continue
			}
			if event.Kind == "pbs.task.failed" {
				if count >= 10 {
					continue
				}
				count++
			}
			if event.CorrelationID != "" {
				seen[event.CorrelationID] = true
			}
		}
		events = append(events, event)
	}
	batch.Events = events
	if tasks != nil {
		for upid := range c.seenTasks {
			if !tasks[upid] {
				delete(c.seenTasks, upid)
			}
		}
	}
	if jobsOK {
		for upid := range c.seenJobs {
			if !jobs[upid] {
				delete(c.seenJobs, upid)
			}
		}
	}
}
