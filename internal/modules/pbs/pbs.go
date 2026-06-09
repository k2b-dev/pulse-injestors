package pbs

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

func (c Collector) Name() string { return "pbs" }

func (c Collector) Collect(ctx context.Context, scope monitoring.Scope) (pulse.Batch, error) {
	b := monitoring.NewBuilder(scope)
	client, err := c.client()
	if err != nil {
		b.State("pbs.available", false, nil)
		b.Event("pbs.config.failed", nil, map[string]any{"error": err.Error()})
		return b.Batch(), nil
	}
	if err := collectVersion(ctx, b, client); err != nil {
		b.State("pbs.available", false, nil)
		b.Event("pbs.version.failed", nil, map[string]any{"error": err.Error()})
		return b.Batch(), nil
	}
	b.State("pbs.available", true, nil)

	stores, err := collectDatastoreUsage(ctx, b, client)
	if err != nil {
		b.Event("pbs.datastore.usage.failed", nil, map[string]any{"error": err.Error()})
	}
	for _, store := range stores {
		if err := collectSnapshots(ctx, b, client, store); err != nil {
			b.Event("pbs.datastore.snapshots.failed", map[string]string{"datastore": store}, map[string]any{"error": err.Error()})
		}
	}
	collectJobs(ctx, b, client)
	collectTasks(ctx, b, client)
	return b.Batch(), nil
}

func (c Collector) client() (Client, error) {
	if c.BaseURL == "" {
		return Client{}, fmt.Errorf("pbs api url is required")
	}
	if c.APIToken == "" {
		return Client{}, fmt.Errorf("pbs api token is required")
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
		return fmt.Errorf("pbs API HTTP %d", resp.StatusCode)
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

func collectDatastoreUsage(ctx context.Context, b *monitoring.Builder, client Client) ([]string, error) {
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
		b.State("pbs.datastore.present", true, dims)
		emitUsage(b, "pbs.datastore.bytes", row.Used, row.Total, dims)
		if row.Available > 0 {
			b.Metric("pbs.datastore.bytes.available", "gauge", row.Available, "bytes", dims)
		}
		if row.EstimatedFullDate > 0 {
			ts := time.Unix(row.EstimatedFullDate, 0).UTC()
			b.State("pbs.datastore.estimated_full.time", ts.Format(time.RFC3339), dims)
			b.Metric("pbs.datastore.estimated_full.seconds_until", "gauge", time.Until(ts).Seconds(), "seconds", dims)
		}
	}
	b.Metric("pbs.datastores.total", "gauge", float64(len(stores)), "count", nil)
	return stores, nil
}

func collectSnapshots(ctx context.Context, b *monitoring.Builder, client Client, store string) error {
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
	b.Metric("pbs.datastore.snapshots.total", "gauge", float64(len(rows)), "count", dims)
	for typ, count := range byType {
		b.Metric("pbs.datastore.snapshots.by_type", "gauge", float64(count), "count", map[string]string{"datastore": store, "type": typ})
	}
	if newest > 0 {
		ts := time.Unix(newest, 0).UTC()
		b.State("pbs.datastore.snapshot.latest.time", ts.Format(time.RFC3339), dims)
		b.Metric("pbs.datastore.snapshot.latest.age", "gauge", time.Since(ts).Seconds(), "seconds", dims)
	}
	return nil
}

func collectJobs(ctx context.Context, b *monitoring.Builder, client Client) {
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
			b.Event("pbs.job.list.failed", map[string]string{"kind": spec.Kind}, map[string]any{"error": err.Error()})
			continue
		}
		emitJobs(b, spec.Kind, parseJobs(rows))
	}
}

func emitJobs(b *monitoring.Builder, kind string, jobs []Job) {
	enabled := 0
	for _, job := range jobs {
		dims := map[string]string{"kind": kind, "job": job.ID}
		addDim(dims, "datastore", job.Store)
		b.State("pbs.job.present", true, dims)
		b.State("pbs.job.enabled", !job.Disabled, dims)
		if !job.Disabled {
			enabled++
		}
		if job.Schedule != "" {
			b.State("pbs.job.schedule", job.Schedule, dims)
		}
		if job.LastRunState != "" {
			b.State("pbs.job.last_run.state", job.LastRunState, dims)
			if job.LastRunState != "OK" && job.LastRunState != "ok" {
				b.Event("pbs.job.failed", dims, map[string]any{"state": job.LastRunState})
			}
		}
		if job.LastRunEndTime > 0 {
			ts := time.Unix(job.LastRunEndTime, 0).UTC()
			b.State("pbs.job.last_run.time", ts.Format(time.RFC3339), dims)
			b.Metric("pbs.job.last_run.age", "gauge", time.Since(ts).Seconds(), "seconds", dims)
		}
		if job.NextRun > 0 {
			ts := time.Unix(job.NextRun, 0).UTC()
			b.State("pbs.job.next_run.time", ts.Format(time.RFC3339), dims)
			b.Metric("pbs.job.next_run.seconds_until", "gauge", time.Until(ts).Seconds(), "seconds", dims)
		}
	}
	b.Metric("pbs.jobs.total", "gauge", float64(len(jobs)), "count", map[string]string{"kind": kind})
	b.Metric("pbs.jobs.enabled", "gauge", float64(enabled), "count", map[string]string{"kind": kind})
}

func collectTasks(ctx context.Context, b *monitoring.Builder, client Client) {
	var rows []Task
	if err := client.get(ctx, "/nodes/localhost/tasks?limit=50", &rows); err != nil {
		b.Event("pbs.tasks.failed", nil, map[string]any{"error": err.Error()})
		return
	}
	byStatus := map[string]int{}
	byTypeStatus := map[string]int{}
	failedEvents := 0
	b.Metric("pbs.tasks.recent", "gauge", float64(len(rows)), "count", nil)
	for _, task := range rows {
		status := taskStatusClass(task)
		typ := task.WorkerType
		if typ == "" {
			typ = "unknown"
		}
		byStatus[status]++
		byTypeStatus[typ+":"+status]++
		if status == "error" && failedEvents < 10 {
			b.Event("pbs.task.failed", taskDims(task), taskPayload(task))
			failedEvents++
		}
	}
	for status, count := range byStatus {
		b.Metric("pbs.tasks.by_status", "gauge", float64(count), "count", map[string]string{"status": status})
	}
	for key, count := range byTypeStatus {
		typ, status, _ := strings.Cut(key, ":")
		b.Metric("pbs.tasks.by_type_status", "gauge", float64(count), "count", map[string]string{"type": typ, "status": status})
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
	addDim(dims, "id", task.WorkerID)
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

func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("pbs api url must use http or https")
	}
	if u.Host == "" {
		return "", fmt.Errorf("pbs api url must include a host")
	}
	if !strings.HasSuffix(u.Path, "/api2/json") {
		u.Path = strings.TrimRight(u.Path, "/") + "/api2/json"
	}
	return strings.TrimRight(u.String(), "/"), nil
}

func authHeader(token string) string {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "PBSAPIToken=") {
		return token
	}
	return "PBSAPIToken=" + token
}
