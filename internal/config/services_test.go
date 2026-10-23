package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceSectionsAreOptIn(t *testing.T) {
	for _, tc := range []struct {
		body    string
		enabled bool
	}{
		{"", false}, {"[postgres]\n", true}, {"[patroni]\nurl='https://127.0.0.1:8008'\n[nats]\nmonitor_url='http://127.0.0.1:8222'\n[valkey]\naddress='127.0.0.1:6379'\n[clickhouse]\nurl='https://127.0.0.1:8443'", true},
	} {
		path := filepath.Join(t.TempDir(), "config.toml")
		if e := os.WriteFile(path, []byte(tc.body), 0600); e != nil {
			t.Fatal(e)
		}
		raw, e := LoadFile(path, true)
		if e != nil {
			t.Fatal(e)
		}
		c, e := Resolve(raw, Overlay{AllowMissingPulse: true})
		if e != nil {
			t.Fatal(e)
		}
		enabled := c.Patroni != nil || c.Postgres != nil || c.NATS != nil || c.Valkey != nil || c.ClickHouse != nil
		if enabled != tc.enabled {
			t.Fatal("section presence lost")
		}
		if c.Valkey != nil && c.Valkey.TLS != nil {
			t.Fatal("default TLS overwritten")
		}
	}
}
func TestServiceValidation(t *testing.T) {
	plain := false
	cases := []Config{
		{Patroni: &PatroniConfig{URL: "https://user:secret@localhost"}},
		{Patroni: &PatroniConfig{URL: "https://localhost", Cluster: " "}},
		{Patroni: &PatroniConfig{URL: "https://localhost", Username: "user"}},
		{Postgres: &PostgresConfig{Command: []string{}}},
		{Postgres: &PostgresConfig{Database: "postgresql://secret@localhost"}},
		{Postgres: &PostgresConfig{Database: "password=secret"}},
		{NATS: &NATSConfig{MonitorURL: "ftp://localhost"}},
		{Valkey: &ValkeyConfig{Address: "localhost"}},
		{Valkey: &ValkeyConfig{Address: "localhost:0"}},
		{Valkey: &ValkeyConfig{Address: "localhost:6379", Username: "user"}},
		{Valkey: &ValkeyConfig{Address: "localhost:6379", TLS: &plain, CAFile: "ca.pem"}},
		{ClickHouse: &ClickHouseConfig{URL: "https://localhost/?password=secret"}},
		{ClickHouse: &ClickHouseConfig{URL: "https://localhost", User: "user\r\nsecret"}},
	}
	for _, c := range cases {
		_, e := Resolve(c, Overlay{AllowMissingPulse: true})
		if e == nil {
			t.Fatalf("accepted invalid config: %+v", c)
		}
		if strings.Contains(e.Error(), "secret") {
			t.Fatal("secret in validation error")
		}
	}
}
func TestInlineServicePasswordsRejected(t *testing.T) {
	for _, section := range []string{"patroni", "postgres", "nats", "valkey", "clickhouse"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		os.WriteFile(path, []byte("["+section+"]\npassword='fixture-secret'"), 0600)
		_, e := LoadFile(path, true)
		if e == nil {
			t.Fatal("inline password accepted")
		}
		if strings.Contains(e.Error(), "fixture-secret") {
			t.Fatal("secret in error")
		}
	}
}

func TestPasswordHTTPRequiresLoopback(t *testing.T) {
	for _, endpoint := range []struct {
		url     string
		allowed bool
	}{
		{"http://127.0.0.1:8008", true}, {"http://127.2.3.4", true}, {"http://[::1]:8008", true},
		{"http://localhost:8008", true}, {"https://db.example.test", true},
		{"http://db.example.test", false}, {"http://192.0.2.1", false}, {"http://[2001:db8::1]", false},
		{"http://localhost.example.test", false}, {"http://127.0.0.1.example.test", false},
	} {
		for _, password := range []string{"", "/fixture/password"} {
			for _, c := range []Config{
				{Patroni: &PatroniConfig{URL: endpoint.url, PasswordFile: password}},
				{ClickHouse: &ClickHouseConfig{URL: endpoint.url, PasswordFile: password}},
			} {
				if c.Patroni != nil && password != "" {
					c.Patroni.Username = "monitor"
				}
				_, err := Resolve(c, Overlay{AllowMissingPulse: true})
				if (err == nil) != (password == "" || endpoint.allowed) {
					t.Errorf("%s password=%t: %v", endpoint.url, password != "", err)
				}
			}
		}
	}
}
