package patroni

import (
	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe/probetest"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReplicaClusterLagAndTLS(t *testing.T) {
	secret := "fixture-patroni-secret"
	s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "probe" || p != secret {
			t.Error("auth mismatch")
		}
		if r.URL.Path == "/cluster" {
			io.WriteString(w, `{"members":[{"name":"other","lag":999},{"name":"node-b","state":"streaming","lag":42}]}`)
			return
		}
		if r.URL.Path != "/patroni" {
			t.Error("unexpected request")
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `{"role":"replica","state":"streaming","timeline":7,"patroni":{"name":"node-b","scope":"db"},"xlog":{"received_location":9007199254741000,"replayed_location":9007199254740993}}`)
	}))
	defer s.Close()
	c := Collector{Config: config.PatroniConfig{URL: s.URL, Username: "probe", PasswordFile: probetest.File(t, secret), ServerName: "example.com", CAFile: probetest.CA(t, s)}}
	b := probetest.Collect(t, c)
	probetest.Metric(t, b, "postgres.replication.lag", 42, "bytes")
	probetest.Metric(t, b, "postgres.replication.replay_backlog", 7, "bytes")
	probetest.State(t, b, "postgres.replication.lag_known", true)
	probetest.State(t, b, "postgres.replication.state", "streaming")
	probetest.Metric(t, b, "postgres.timeline", 7, "")
	probetest.State(t, b, "postgres.role", "replica")
	probetest.NoSecret(t, b, "", secret)
	c.Config.ServerName = "wrong.invalid"
	probetest.Failure(t, c, "patroni", secret)
	c.Config.ServerName = "example.com"
	c.Config.CAFile = ""
	probetest.Failure(t, c, "patroni", secret)
	c.Config.InsecureSkipVerify = true
	probetest.Collect(t, c)
}
func TestLeaderClusterScope(t *testing.T) {
	for _, role := range []string{"primary", "standby_leader"} {
		for _, override := range []string{"", "override"} {
			t.Run(override, func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/patroni":
						io.WriteString(w, `{"role":"`+role+`","state":"running","timeline":3,"patroni":{"scope":"db"}}`)
					case "/cluster":
						io.WriteString(w, `{"members":[{"name":"a","role":"`+role+`","state":"running"},{"name":"b","role":"sync_standby","state":"streaming"},{"name":"c","role":"replica","state":"stopped"}]}`)
					default:
						t.Error(r.URL.Path)
					}
				}))
				defer s.Close()
				b := probetest.Collect(t, Collector{Config: config.PatroniConfig{URL: s.URL, Cluster: override}})
				probetest.Metric(t, b, "postgres.cluster.members", 3, "")
				probetest.Metric(t, b, "postgres.cluster.members_running", 2, "")
				probetest.Metric(t, b, "postgres.cluster.sync_standbys", 1, "")
				probetest.State(t, b, "postgres.cluster.leader", "a")
				want := override
				if want == "" {
					want = "db"
				}
				for _, m := range b.Metrics {
					if strings.HasPrefix(m.Name, "postgres.cluster.") && (m.Resource.Type != "postgres-cluster" || m.Resource.ID != want) {
						t.Fatalf("resource=%+v", m.Resource)
					}
				}
			})
		}
	}
}
func TestFailuresAndSecretRedaction(t *testing.T) {
	secret := "fixture-patroni-secret"
	for _, body := range []string{`{}`, `{"role":"primary","state":"running"}`, secret, strings.Repeat("x", serviceprobe.MaxBytes+1)} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		probetest.Failure(t, Collector{Config: config.PatroniConfig{URL: s.URL, Username: "probe", PasswordFile: probetest.File(t, secret)}}, "patroni", secret)
		s.Close()
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cluster" {
			io.WriteString(w, `{"members":[{"name":"b","lag":"unknown","state":"`+secret+`"}]}`)
		} else {
			io.WriteString(w, `{"role":"replica","state":"`+secret+`","patroni":{"name":"b"}}`)
		}
	}))
	defer s.Close()
	c := Collector{Config: config.PatroniConfig{URL: s.URL, Username: "probe", PasswordFile: probetest.File(t, secret)}}
	probetest.NoSecret(t, probetest.Collect(t, c), "", secret)
}

func TestReplicaUnknownLag(t *testing.T) {
	for _, lag := range []string{`"unknown"`, `null`} {
		t.Run(lag, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/patroni" {
					io.WriteString(w, `{"role":"replica","state":"running","patroni":{"name":"b"},"xlog":{"received_location":10,"replayed_location":12}}`)
				} else {
					io.WriteString(w, `{"members":[{"name":"a","lag":0},{"name":"b","lag":`+lag+`,"state":"streaming"}]}`)
				}
			}))
			defer s.Close()
			b := probetest.Collect(t, Collector{Config: config.PatroniConfig{URL: s.URL}})
			probetest.State(t, b, "postgres.replication.lag_known", false)
			probetest.State(t, b, "postgres.replication.state", "streaming")
			probetest.Metric(t, b, "postgres.replication.replay_backlog", 0, "bytes")
			for _, m := range b.Metrics {
				if m.Name == "postgres.replication.lag" || strings.HasPrefix(m.Name, "postgres.cluster.") {
					t.Fatal(m)
				}
			}
		})
	}
}
