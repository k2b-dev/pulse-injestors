package pbs

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

	"github.com/valentinkolb/pulse-injestors/internal/entity"
	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

var ErrCommandNotFound = errors.New("pbs command not found")

type Collector struct {
	CommandPath string
	Timeout     time.Duration
	Runner      CommandRunner
}

func (c Collector) Name() string { return "pbs" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	client, err := c.client()
	if err != nil {
		b.State("pbs.available", false, nil)
		if !errors.Is(err, ErrCommandNotFound) {
			emitError(b, "pbs.config.failed", "configure", nil, err)
		}
		return b.Batch(), nil
	}
	if err := collectVersion(ctx, b, client); err != nil {
		b.State("pbs.available", false, nil)
		emitError(b, "pbs.version.failed", "version", nil, err)
		return b.Batch(), nil
	}
	b.State("pbs.available", true, nil)

	stores, err := collectDatastoreUsage(ctx, b, client, scope)
	if err != nil {
		emitError(b, "pbs.datastore.usage.failed", "datastore_usage", nil, err)
	}
	for _, store := range stores {
		if err := collectSnapshots(ctx, b, client, store, scope); err != nil {
			db := monitoring.NewBuilder(datastoreScope(scope, store))
			emitError(db, "pbs.datastore.snapshots.failed", "snapshots", map[string]string{"datastore": store}, err)
			b.Merge(db.Batch())
		}
	}
	collectJobs(ctx, b, client, scope)
	collectTasks(ctx, b, client)
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
	return Client{CommandPath: path, Timeout: timeout, Runner: runner}, nil
}

type Client struct {
	CommandPath string
	Timeout     time.Duration
	Runner      CommandRunner
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
	args, err := debugAPIArgs(path)
	if err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, c.Timeout)
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
		b.State("pbs.version", version.Version, nil)
	}
	if version.Release != "" {
		b.State("pbs.release", version.Release, nil)
	}
	if version.RepoID != "" {
		b.State("pbs.repoid", version.RepoID, nil)
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
		emitUsage(db, "pbs.datastore.bytes", row.Used, row.Total, dims)
		if row.Available > 0 {
			db.Metric("pbs.datastore.bytes.available", "gauge", row.Available, "bytes", dims)
		}
		if row.EstimatedFullDate > 0 {
			ts := time.Unix(row.EstimatedFullDate, 0).UTC()
			db.State("pbs.datastore.estimated_full.time", ts.Format(time.RFC3339), dims)
			db.Metric("pbs.datastore.estimated_full.seconds_until", "gauge", time.Until(ts).Seconds(), "seconds", dims)
		}
		b.Merge(db.Batch())
	}
	b.Metric("pbs.datastores.total", "gauge", float64(len(stores)), "", nil)
	return stores, nil
}

func collectSnapshots(ctx context.Context, b *monitoring.Builder, client Client, store string, scope monitoring.Scope) error {
	var rows []Snapshot
	if err := client.get(ctx, "/admin/datastore/"+url.PathEscape(store)+"/snapshots", &rows); err != nil {
		return err
	}
	byType := map[string]int{}
	var newest int64
	for _, row := range rows {
		typ := row.BackupType
		if typ == "" {
			typ = "unknown"
		}
		byType[typ]++
		if row.BackupTime > newest {
			newest = row.BackupTime
		}
	}
	dims := map[string]string{"datastore": store}
	db := monitoring.NewBuilder(datastoreScope(scope, store))
	db.Metric("pbs.datastore.snapshots.total", "gauge", float64(len(rows)), "", dims)
	for typ, count := range byType {
		db.Metric("pbs.datastore.snapshots.by_type", "gauge", float64(count), "", map[string]string{"datastore": store, "type": typ})
	}
	if newest > 0 {
		ts := time.Unix(newest, 0).UTC()
		db.State("pbs.datastore.snapshot.latest.time", ts.Format(time.RFC3339), dims)
		db.Metric("pbs.datastore.snapshot.latest.age", "gauge", time.Since(ts).Seconds(), "seconds", dims)
	}
	b.Merge(db.Batch())
	return nil
}

func collectJobs(ctx context.Context, b *monitoring.Builder, client Client, scope monitoring.Scope) {
	for _, spec := range []struct {
		Kind string
		Path string
	}{
		{Kind: "gc", Path: "/admin/gc"},
		{Kind: "prune", Path: "/admin/prune"},
		{Kind: "sync", Path: "/admin/sync"},
		{Kind: "verify", Path: "/admin/verify"},
	} {
		var rows []map[string]any
		if err := client.get(ctx, spec.Path, &rows); err != nil {
			emitError(b, "pbs.job.list.failed", "job_list", map[string]string{"kind": spec.Kind}, err)
			continue
		}
		emitJobs(b, scope, spec.Kind, parseJobs(rows))
	}
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
			jb.State("pbs.job.schedule", job.Schedule, dims)
		}
		if job.LastRunState != "" {
			jb.State("pbs.job.last_run.state", job.LastRunState, dims)
			if job.LastRunState != "OK" && job.LastRunState != "ok" {
				jb.EventDetails("pbs.job.failed", monitoring.MergeDimensions(dims, map[string]string{"status": "error"}), monitoring.EventDetails{
					Attributes: map[string]any{"state": job.LastRunState},
				})
			}
		}
		if job.LastRunEndTime > 0 {
			ts := time.Unix(job.LastRunEndTime, 0).UTC()
			jb.State("pbs.job.last_run.time", ts.Format(time.RFC3339), dims)
			jb.Metric("pbs.job.last_run.age", "gauge", time.Since(ts).Seconds(), "seconds", dims)
		}
		if job.NextRun > 0 {
			ts := time.Unix(job.NextRun, 0).UTC()
			jb.State("pbs.job.next_run.time", ts.Format(time.RFC3339), dims)
			jb.Metric("pbs.job.next_run.seconds_until", "gauge", time.Until(ts).Seconds(), "seconds", dims)
		}
		b.Merge(jb.Batch())
	}
	b.Metric("pbs.jobs.total", "gauge", float64(len(jobs)), "", map[string]string{"kind": kind})
	b.Metric("pbs.jobs.enabled", "gauge", float64(enabled), "", map[string]string{"kind": kind})
}

func collectTasks(ctx context.Context, b *monitoring.Builder, client Client) {
	var rows []Task
	if err := client.get(ctx, "/nodes/localhost/tasks?limit=50", &rows); err != nil {
		emitError(b, "pbs.tasks.failed", "tasks", nil, err)
		return
	}
	byStatus := map[string]int{}
	byTypeStatus := map[string]int{}
	failedEvents := 0
	b.Metric("pbs.tasks.recent", "gauge", float64(len(rows)), "", nil)
	for _, task := range rows {
		status := taskStatusClass(task)
		typ := task.WorkerType
		if typ == "" {
			typ = "unknown"
		}
		byStatus[status]++
		byTypeStatus[typ+":"+status]++
		if status == "error" && failedEvents < 10 {
			emitTaskEvent(b, "pbs.task.failed", task)
			failedEvents++
		}
	}
	for status, count := range byStatus {
		b.Metric("pbs.tasks.by_status", "gauge", float64(count), "", map[string]string{"status": status})
	}
	for key, count := range byTypeStatus {
		typ, status, _ := strings.Cut(key, ":")
		b.Metric("pbs.tasks.by_type_status", "gauge", float64(count), "", map[string]string{"type": typ, "status": status})
	}
}

type DatastoreUsage struct {
	Store             string  `json:"store"`
	Used              float64 `json:"used"`
	Total             float64 `json:"total"`
	Available         float64 `json:"avail"`
	EstimatedFullDate int64   `json:"estimated-full-date"`
}

type Snapshot struct {
	BackupType string `json:"backup-type"`
	BackupID   string `json:"backup-id"`
	BackupTime int64  `json:"backup-time"`
}

type Job struct {
	ID             string
	Store          string
	Disabled       bool
	Schedule       string
	LastRunState   string
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
		attributes["status"] = task.Status
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

func datastoreScope(scope monitoring.Scope, datastore string) monitoring.Scope {
	scope.EntityType = "pbs-datastore"
	scope.EntityID = entity.ID("pbs-datastore", stableHostFromScope(scope), datastore)
	scope.Label = datastore
	return scope
}

func jobScope(scope monitoring.Scope, kind, job string) monitoring.Scope {
	scope.EntityType = "pbs-job"
	scope.EntityID = entity.ID("pbs-job", stableHostFromScope(scope), entity.Key(kind, "unknown"), entity.Key(job, "unknown"))
	scope.Label = labelValue(job, "unknown")
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
	return entity.StableHostIDFromScope(scope.EntityID, scope.Dimensions)
}

func emitError(b *monitoring.Builder, kind, operation string, dims map[string]string, err error) {
	b.EventDetails(kind, monitoring.MergeDimensions(dims, map[string]string{"operation": operation}), monitoring.EventDetails{
		Attributes: map[string]any{"error": err.Error()},
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
