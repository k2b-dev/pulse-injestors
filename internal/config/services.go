package config

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/k2b-dev/pulse-injestors/internal/serviceprobe"
)

type PatroniConfig struct {
	ServerName         string `toml:"server_name"`
	URL                string `toml:"url"`
	Cluster            string `toml:"cluster"`
	CAFile             string `toml:"ca_file"`
	InsecureSkipVerify bool   `toml:"insecure_skip_verify"`
	Username           string `toml:"username"`
	PasswordFile       string `toml:"password_file"`
}
type PostgresConfig struct {
	Command  []string `toml:"command"`
	Database string   `toml:"database"`
}
type NATSConfig struct {
	MonitorURL string `toml:"monitor_url"`
}
type ValkeyConfig struct {
	Address      string `toml:"address"`
	TLS          *bool  `toml:"tls"`
	ServerName   string `toml:"server_name"`
	CAFile       string `toml:"ca_file"`
	PasswordFile string `toml:"password_file"`
	Username     string `toml:"username"`
}
type ClickHouseConfig struct {
	URL          string `toml:"url"`
	User         string `toml:"user"`
	PasswordFile string `toml:"password_file"`
	CAFile       string `toml:"ca_file"`
	ServerName   string `toml:"server_name"`
}

func (c Config) validateServices() error {
	if p := c.Patroni; p != nil {
		if !serviceprobe.ValidURL(p.URL) {
			return errors.New("patroni.url must be an http/https endpoint without credentials, query or fragment")
		}
		if p.PasswordFile != "" && !passwordURLAllowed(p.URL) {
			return errors.New("patroni basic authentication requires HTTPS except on loopback")
		}
		if p.Cluster != "" && (strings.TrimSpace(p.Cluster) != p.Cluster || len([]rune(p.Cluster)) > 240 || strings.ContainsAny(p.Cluster, "\r\n\x00")) {
			return errors.New("patroni.cluster must be a nonblank scope name of at most 240 characters")
		}
		if (p.Username == "") != (p.PasswordFile == "") {
			return errors.New("patroni username and password_file must be configured together")
		}
	}
	if p := c.Postgres; p != nil {
		if p.Command != nil && (len(p.Command) == 0 || strings.TrimSpace(p.Command[0]) == "") {
			return errors.New("postgres.command must contain an executable")
		}
		// A database name must not become a libpq connection string with credentials.
		if strings.ContainsAny(p.Database, "=\x00\r\n") || strings.Contains(p.Database, "://") {
			return errors.New("postgres.database must be a database name")
		}
	}
	if p := c.NATS; p != nil && !serviceprobe.ValidURL(p.MonitorURL) {
		return errors.New("nats.monitor_url must be an http/https endpoint without credentials, query or fragment")
	}
	if p := c.Valkey; p != nil {
		host, port, e := net.SplitHostPort(p.Address)
		n, pe := strconv.Atoi(port)
		if e != nil || pe != nil || host == "" || n < 1 || n > 65535 {
			return errors.New("valkey.address must use host:port")
		}
		if p.Username != "" && p.PasswordFile == "" {
			return errors.New("valkey.username requires password_file")
		}
		if p.TLS != nil && !*p.TLS && (p.CAFile != "" || p.ServerName != "") {
			return errors.New("valkey CA and server_name require TLS")
		}
	}
	if p := c.ClickHouse; p != nil {
		if !serviceprobe.ValidURL(p.URL) {
			return errors.New("clickhouse.url must be an http/https endpoint without credentials, query or fragment")
		}
		if p.PasswordFile != "" && !passwordURLAllowed(p.URL) {
			return errors.New("clickhouse password authentication requires HTTPS except on loopback")
		}
		if strings.ContainsAny(p.User, "\r\n\x00") {
			return errors.New("invalid clickhouse.user")
		}
	}
	return nil
}

// Do not resolve DNS names here: only literal loopback addresses and localhost
// qualify for sending passwords without TLS.
func passwordURLAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return u.Scheme == "https" || strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}
