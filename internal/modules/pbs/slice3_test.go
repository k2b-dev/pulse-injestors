package pbs

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

func TestGroupsAndFailedVerification(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{
		"api get /admin/datastore --output-format json":                              `[{"store":"s3"},{"store":"main"}]`,
		"api get /admin/datastore/main/namespace --max-depth 7 --output-format json": `[]`,
		"api get /admin/datastore/main/groups --output-format json":                  `[{"backup-type": "vm", "backup-id": "101", "last-backup": 1700000900}, {"backup-type": "ct", "backup-id": "203", "last-backup": 1700000800}, {"backup-type": "host", "backup-id": "x", "last-backup": 1700000700}]`,
		"api get /admin/datastore/main/snapshots --output-format json":               `[{"backup-type":"vm","backup-id":"101","backup-time":1700000000,"verification":{"state":"failed"}},{"backup-type":"vm","backup-id":"101","backup-time":1700000900,"verification":{"state":"ok"}},{"backup-type":"ct","backup-id":"203","backup-time":1700000800},{"backup-type":"host","backup-id":"x","backup-time":1700000700}]`,
		"api get /admin/datastore/s3/namespace --max-depth 7 --output-format json":   `[]`,
		"api get /admin/datastore/s3/groups --output-format json":                    `[{"backup-type": "vm", "backup-id": "101", "last-backup": 1700000500}]`,
		"api get /admin/datastore/s3/snapshots --output-format json":                 `[{"backup-type":"vm","backup-id":"101","backup-time":1700000500,"verification":{"state":"failed"}}]`,
	}}
	scope := monitoring.Scope{Resource: pulse.ResourceRef{Type: "proxmox-backup-server", ID: "backup"}, Timestamp: time.Unix(1700001000, 0)}
	batch, _ := (&Collector{Runner: runner, Part: "groups"}).Collect(context.Background(), scope)
	if len(batch.Events) > 0 || countMetric(batch.Metrics, "pbs.group.last_snapshot.age") != 4 {
		t.Fatal(batch)
	}
	for _, m := range batch.Metrics {
		if m.Name == "pbs.group.last_snapshot.age" && m.Resource.ID == "backup:main:vm%2F101" && m.Value != 100 {
			t.Fatal(m)
		}
		if m.Name == "pbs.datastore.verify.failed_snapshots" && m.Value != 1 {
			t.Fatal(m)
		}
	}
	if countState(batch.States, "pbs.groups.truncated") != 1 {
		t.Fatal(batch.States)
	}
}

func TestGroupsBoundAcrossDatastores(t *testing.T) {
	var rows []string
	for i := 0; i < 2001; i++ {
		rows = append(rows, fmt.Sprintf(`{"backup-type":"vm","backup-id":"%d","last-backup":1700000000}`, i))
	}
	runner := &fakeRunner{responses: map[string]string{
		"api get /admin/datastore --output-format json":                           `[{"store":"b"},{"store":"a"}]`,
		"api get /admin/datastore/a/namespace --max-depth 7 --output-format json": `[]`,
		"api get /admin/datastore/a/snapshots --output-format json":               `[]`,
		"api get /admin/datastore/a/groups --output-format json":                  "[" + strings.Join(rows, ",") + "]",
		"api get /admin/datastore/b/namespace --max-depth 7 --output-format json": `[]`,
		"api get /admin/datastore/b/groups --output-format json":                  `[{"backup-type": "host", "backup-id": "x", "last-backup": 1700000000}]`,
		"api get /admin/datastore/b/snapshots --output-format json":               `[{"backup-type":"host","backup-id":"x","backup-time":1700000000}]`,
	}}
	batch, _ := (&Collector{Runner: runner, Part: "groups"}).Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "proxmox-backup-server", ID: "backup"}, Timestamp: time.Unix(1700001000, 0)})
	if countMetric(batch.Metrics, "pbs.group.last_snapshot.age") != 2000 {
		t.Fatal("unbounded groups")
	}
	for _, s := range batch.States {
		if s.Key == "pbs.groups.truncated" && s.Value != true {
			t.Fatal(s)
		}
	}
}

func TestZeroDatastoreCapacity(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{"api get /status/datastore-usage --output-format json": `[{"store":"full","used":100,"total":100,"avail":0},{"store":"empty","used":0,"total":100,"avail":100}]`}}
	batch, _ := (&Collector{Runner: runner, Part: "datastores"}).Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "proxmox-backup-server", ID: "backup"}, Timestamp: time.Unix(1700001000, 0)})
	if countMetric(batch.Metrics, "pbs.datastore.bytes.used") != 2 || countMetric(batch.Metrics, "pbs.datastore.bytes.available") != 2 {
		t.Fatal(batch.Metrics)
	}
}

func TestDatastoreAPIErrorDoesNotInventZeroCapacity(t *testing.T) {
	runner := &fakeRunner{responses: map[string]string{"api get /status/datastore-usage --output-format json": `[{"store":"s3","error":"datastore unavailable"}]`}}
	batch, _ := (&Collector{Runner: runner, Part: "datastores"}).Collect(context.Background(), monitoring.Scope{Resource: pulse.ResourceRef{Type: "proxmox-backup-server", ID: "backup"}, Timestamp: time.Unix(1700001000, 0)})
	if countMetric(batch.Metrics, "pbs.datastore.bytes.available") != 0 || countEvent(batch.Events, "pbs.datastore.usage.failed") != 1 {
		t.Fatal(batch)
	}
}
