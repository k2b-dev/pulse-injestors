package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/k2b-dev/pulse-injestors/internal/agent"
	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/modules/backup"
	"github.com/k2b-dev/pulse-injestors/internal/modules/btrfs"
	"github.com/k2b-dev/pulse-injestors/internal/modules/ceph"
	"github.com/k2b-dev/pulse-injestors/internal/modules/certificates"
	"github.com/k2b-dev/pulse-injestors/internal/modules/clickhouse"
	"github.com/k2b-dev/pulse-injestors/internal/modules/diskhealth"
	"github.com/k2b-dev/pulse-injestors/internal/modules/diskio"
	"github.com/k2b-dev/pulse-injestors/internal/modules/filesystem"
	"github.com/k2b-dev/pulse-injestors/internal/modules/inventory"
	"github.com/k2b-dev/pulse-injestors/internal/modules/kernel"
	"github.com/k2b-dev/pulse-injestors/internal/modules/linuxruntime"
	"github.com/k2b-dev/pulse-injestors/internal/modules/nats"
	"github.com/k2b-dev/pulse-injestors/internal/modules/network"
	"github.com/k2b-dev/pulse-injestors/internal/modules/packages"
	"github.com/k2b-dev/pulse-injestors/internal/modules/patroni"
	"github.com/k2b-dev/pulse-injestors/internal/modules/postgres"
	"github.com/k2b-dev/pulse-injestors/internal/modules/posture"
	"github.com/k2b-dev/pulse-injestors/internal/modules/script"
	"github.com/k2b-dev/pulse-injestors/internal/modules/system"
	"github.com/k2b-dev/pulse-injestors/internal/modules/systemd"
	"github.com/k2b-dev/pulse-injestors/internal/modules/thermal"
	"github.com/k2b-dev/pulse-injestors/internal/modules/valkey"
	"github.com/k2b-dev/pulse-injestors/internal/modules/zfs"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var version = "dev"

type cli struct {
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/pulse-linux.toml when present." env:"PULSE_CONFIG"`

	IngestURL       string   `name:"ingest-url" help:"Pulse ingest endpoint URL." env:"PULSE_INGEST_URL"`
	IngestTokenFile string   `name:"ingest-token-file" help:"Private file with the Pulse source token. PULSE_INGEST_TOKEN is also read." env:"PULSE_INGEST_TOKEN_FILE"`
	ResourceID      string   `name:"resource-id" help:"Stable monitored resource id. Defaults to hostname." env:"PULSE_RESOURCE_ID"`
	ResourceLabel   string   `name:"resource-label" help:"Human-readable monitored host label. Defaults to hostname." env:"PULSE_RESOURCE_LABEL"`
	Dimensions      []string `name:"dimension" help:"Bounded global dimension as key=value. Repeat for multiple values." env:"PULSE_DIMENSIONS" sep:","`

	IntervalSeconds         int    `name:"interval-seconds" help:"Collection interval for run mode." env:"PULSE_INTERVAL_SECONDS"`
	CollectorTimeoutSeconds int    `name:"collector-timeout-seconds" help:"Overall timeout per collector in seconds." env:"PULSE_COLLECTOR_TIMEOUT_SECONDS"`
	TimeoutSeconds          int    `name:"timeout-seconds" help:"HTTP request timeout in seconds." env:"PULSE_HTTP_TIMEOUT_SECONDS"`
	SpoolDir                string `name:"spool-dir" help:"Private persistent delivery directory." env:"PULSE_SPOOL_DIR"`
	SpoolMaxBytes           int64  `name:"spool-max-bytes" help:"Maximum persistent delivery bytes; required with spool-dir." env:"PULSE_SPOOL_MAX_BYTES"`
	MaxRetries              int    `name:"max-retries" help:"HTTP retry count for network, 408, 409, 429 and 5xx failures." env:"PULSE_HTTP_MAX_RETRIES"`
	InitialBackoffMS        int    `name:"initial-backoff-ms" help:"Initial retry backoff in milliseconds." env:"PULSE_HTTP_INITIAL_BACKOFF_MS"`

	ProcRoot    string `name:"proc-root" help:"procfs root to read." env:"PULSE_HOST_PROC_ROOT"`
	SysRoot     string `name:"sys-root" help:"sysfs root to read." env:"PULSE_HOST_SYS_ROOT"`
	HostRoot    string `name:"host-root" help:"Host root filesystem." env:"PULSE_HOST_ROOT"`
	CPUSampleMS int    `name:"cpu-sample-ms" help:"CPU usage sample window in milliseconds." env:"PULSE_HOST_CPU_SAMPLE_MS"`

	LinuxProfile             string `name:"linux-profile" help:"Linux monitoring profile: server, desktop, docker-host, proxmox-node, pbs, postgres, pg-edge, nats, nats-edge, valkey, clickhouse." env:"PULSE_LINUX_PROFILE"`
	SystemdUnits             string `name:"systemd-units" help:"Comma-separated systemd units to monitor." env:"PULSE_LINUX_SYSTEMD_UNITS"`
	PackageTimeoutSeconds    int    `name:"package-timeout-seconds" help:"Linux package update timeout in seconds." env:"PULSE_LINUX_PACKAGE_TIMEOUT_SECONDS"`
	DiskHealthTimeoutSeconds int    `name:"disk-health-timeout-seconds" help:"SMART/NVMe disk health timeout in seconds." env:"PULSE_LINUX_DISK_HEALTH_TIMEOUT_SECONDS"`

	Local   bool `name:"local" help:"Write collected Pulse batch JSON to stdout instead of sending it." env:"PULSE_LOCAL"`
	Pretty  bool `name:"pretty" help:"With --local, print a human-readable report instead of JSON." env:"PULSE_LOCAL_PRETTY"`
	Verbose bool `name:"verbose" short:"v" help:"Verbose logging." env:"PULSE_VERBOSE"`
	Version bool `name:"version" help:"Print version and exit."`

	Once onceCmd `cmd:"" help:"Collect, push once, and exit."`
	Run  runCmd  `cmd:"" default:"1" help:"Collect and push forever on the configured interval."`
}

type onceCmd struct{}
type runCmd struct{}

func main() {
	var c cli
	kctx := kong.Parse(&c,
		kong.Name("pulse-linux"),
		kong.Description("Pulse Linux host monitoring ingestor."),
		kong.UsageOnError(),
		kong.Vars{"version": version},
	)
	if c.Version {
		fmt.Printf("pulse-linux %s\n", version)
		return
	}

	level := slog.LevelInfo
	if c.Verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := loadConfig(c)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	runner := &monitoring.Runner{
		Resource:   pulse.ResourceRef{Type: "host", ID: cfg.Resource.ID, Label: cfg.Resource.Label},
		Dimensions: cfg.Dimensions,
		Collectors: collectors(cfg),
		Timeout:    cfg.CollectorTimeout(),
		Interval:   cfg.RunnerInterval(),
		Logger:     log,
	}

	err = agent.Run(ctx, agent.Options{
		Name:    "pulse-linux",
		Version: version,
		Command: kctx.Command(),
		Local:   c.Local,
		Pretty:  c.Pretty,
		Config:  cfg,
		Runner:  runner,
		Logger:  log,
	})
	if err != nil && ctx.Err() == nil {
		log.Error(kctx.Command(), "err", err)
		os.Exit(1)
	}
}

func loadConfig(c cli) (config.Config, error) {
	cfgPath := c.Config
	explicitConfig := cfgPath != ""
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath("pulse-linux")
	}
	fileCfg, err := config.LoadFile(cfgPath, explicitConfig)
	if err != nil {
		return config.Config{}, fmt.Errorf("load %s: %w", cfgPath, err)
	}
	if fileCfg.Host.ProcRoot == "" {
		fileCfg.Host.ProcRoot = "/proc"
	}
	if fileCfg.Host.SysRoot == "" {
		fileCfg.Host.SysRoot = "/sys"
	}
	if fileCfg.Host.Root == "" {
		fileCfg.Host.Root = "/"
	}
	dimensions, err := config.ParseDimensions(c.Dimensions)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Resolve(fileCfg, config.Overlay{
		SpoolDir:                      c.SpoolDir,
		SpoolMaxBytes:                 c.SpoolMaxBytes,
		ConfigPath:                    cfgPath,
		IngestURL:                     c.IngestURL,
		IngestToken:                   os.Getenv("PULSE_INGEST_TOKEN"),
		IngestTokenFile:               c.IngestTokenFile,
		ResourceID:                    c.ResourceID,
		ResourceLabel:                 c.ResourceLabel,
		Dimensions:                    dimensions,
		TimeoutSeconds:                c.TimeoutSeconds,
		MaxRetries:                    c.MaxRetries,
		InitialBackoffMS:              c.InitialBackoffMS,
		IntervalSeconds:               c.IntervalSeconds,
		CollectorTimeoutSeconds:       c.CollectorTimeoutSeconds,
		ProcRoot:                      c.ProcRoot,
		SysRoot:                       c.SysRoot,
		HostRoot:                      c.HostRoot,
		CPUSampleMS:                   c.CPUSampleMS,
		LinuxProfile:                  c.LinuxProfile,
		LinuxSystemdUnits:             splitCSV(c.SystemdUnits),
		LinuxPackageTimeoutSeconds:    c.PackageTimeoutSeconds,
		LinuxDiskHealthTimeoutSeconds: c.DiskHealthTimeoutSeconds,
		AllowMissingPulse:             c.Local,
	})
	if err != nil {
		return config.Config{}, err
	}
	cfg.Resource.ID = entity.StableHostID(cfg.Resource.ID)
	if cfg.Resource.ID == "" {
		return config.Config{}, fmt.Errorf("resource id is required")
	}
	cfg.Resource.Type = "host"
	if err := validateCadenceNames(cfg); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

func collectors(cfg config.Config) []monitoring.Collector {
	out := []monitoring.Collector{
		system.Collector{ProcRoot: cfg.Host.ProcRoot, CPUSampleTime: cfg.CPUSampleWindow()},
		systemd.Collector{Units: linuxSystemdUnits(cfg), Timeout: 5 * time.Second},
		filesystem.Collector{ProcRoot: cfg.Host.ProcRoot, HostRoot: cfg.Host.Root},
		network.Collector{ProcRoot: cfg.Host.ProcRoot, Exclude: cfg.Linux.NetworkExclude},
		linuxruntime.Collector{ProcRoot: cfg.Host.ProcRoot},
		&kernel.Collector{ProcRoot: cfg.Host.ProcRoot, StateDir: cfg.Pulse.SpoolDir},
		diskio.Collector{ProcRoot: cfg.Host.ProcRoot, SysRoot: cfg.Host.SysRoot},
	}
	if cfg.ThermalEnabled() {
		out = append(out, thermal.Collector{SysRoot: cfg.Host.SysRoot})
	}
	if cfg.BtrfsEnabled() {
		out = append(out, btrfs.Collector{ProcRoot: cfg.Host.ProcRoot, HostRoot: cfg.Host.Root, Timeout: 5 * time.Second})
	}
	if cfg.ZfsEnabled() {
		out = append(out, zfs.Collector{Timeout: 5 * time.Second})
	}
	if cfg.CephEnabled() {
		out = append(out, ceph.Collector{Timeout: 5 * time.Second})
	}
	markers := make([]backup.Marker, 0, len(cfg.Backups))
	for _, m := range cfg.Backups {
		markers = append(markers, backup.Marker{Name: m.Name, MarkerFile: m.MarkerFile, Command: m.Command})
	}
	out = append(out,
		backup.Collector{Markers: markers},
		packages.Collector{HostRoot: cfg.Host.Root, Timeout: time.Duration(cfg.Linux.PackageTimeoutSeconds) * time.Second},
		certificates.Collector{Paths: cfg.Linux.CertificatePaths},
		diskhealth.Collector{Timeout: time.Duration(cfg.Linux.DiskHealthTimeoutSeconds) * time.Second},
		inventory.Collector{HostRoot: cfg.Host.Root, ProcRoot: cfg.Host.ProcRoot, SysRoot: cfg.Host.SysRoot},
	)
	if cfg.Patroni != nil {
		out = append(out, patroni.Collector{Config: *cfg.Patroni})
	}
	if cfg.Postgres != nil {
		out = append(out, postgres.Collector{Config: *cfg.Postgres}, postgres.Collector{Config: *cfg.Postgres, Version: true})
	}
	if cfg.NATS != nil {
		for _, g := range []string{"health", "jetstream", "version"} {
			out = append(out, nats.Collector{Config: *cfg.NATS, Group: g})
		}
	}
	if cfg.Valkey != nil {
		for _, g := range []string{"health", "capacity", "version"} {
			out = append(out, valkey.Collector{Config: *cfg.Valkey, Group: g})
		}
	}
	if cfg.ClickHouse != nil {
		for _, g := range []string{"health", "capacity", "version"} {
			out = append(out, clickhouse.Collector{Config: *cfg.ClickHouse, Group: g})
		}
	}
	if cfg.Linux.Profile == "desktop" {
		out = append(out, posture.Collector{HostRoot: cfg.Host.Root})
	}
	for i, c := range out {
		every := linuxCadences[c.Name()]
		if cfg.Linux.Profile == "desktop" {
			if desktop, ok := desktopCadences[c.Name()]; ok {
				every = desktop
			}
		}
		if seconds, ok := cfg.Runner.Cadence[c.Name()]; ok {
			every = time.Duration(seconds) * time.Second
		}
		out[i] = monitoring.WithCadence{Collector: c, Every: max(every, cfg.RunnerInterval())}
	}
	for _, s := range cfg.Scripts {
		every := max(time.Duration(s.CadenceSeconds)*time.Second, cfg.RunnerInterval())
		if seconds, ok := cfg.Runner.Cadence["script:"+s.Name]; ok {
			every = time.Duration(seconds) * time.Second
		}
		out = append(out, monitoring.WithCadence{
			Collector: script.Collector{
				CollectorName: "script:" + s.Name,
				Scripts: []script.Script{{
					Name: s.Name, Command: s.Command,
					Timeout:        time.Duration(s.TimeoutSeconds) * time.Second,
					MaxOutputBytes: s.MaxOutputBytes, Dimensions: s.Dimensions,
				}},
			}, Every: every,
		})
	}

	return out
}

func linuxSystemdUnits(cfg config.Config) []string {
	return mergeStringLists(profileSystemdUnits(cfg.Linux.Profile), cfg.Linux.SystemdUnits)
}

func profileSystemdUnits(profile string) []string {
	switch strings.TrimSpace(strings.ToLower(profile)) {
	case "", "none":
		return nil
	case "server":
		return []string{"ssh.service", "sshd.service"}
	case "desktop":
		return []string{"display-manager.service", "NetworkManager.service", "cups.service"}
	case "proxmox-node":
		return []string{"pve-cluster.service", "corosync.service", "pvedaemon.service", "pveproxy.service", "pvestatd.service", "pve-ha-lrm.service", "pve-ha-crm.service"}
	case "pbs":
		return []string{"proxmox-backup.service", "proxmox-backup-proxy.service"}
	case "postgres":
		return []string{"patroni.service", "etcd.service"}
	case "pg-edge":
		return []string{"pgbouncer.service", "haproxy.service", "keepalived.service"}
	case "nats":
		return []string{"nats-server.service"}
	case "nats-edge":
		return []string{"haproxy.service", "keepalived.service"}
	case "valkey":
		return []string{"valkey.service"}
	case "clickhouse":
		return []string{"clickhouse-server.service"}
	case "docker-host":
		return []string{"docker.service", "containerd.service", "ssh.service", "sshd.service"}
	default:
		return nil
	}
}

func mergeStringLists(values ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, list := range values {
		for _, value := range list {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// linuxCadences is the default sampling schedule; once still runs every collector.
var linuxCadences = map[string]time.Duration{
	"system": 60 * time.Second, "systemd": 60 * time.Second, "kernel": 60 * time.Second,
	"filesystem": 300 * time.Second, "network": 300 * time.Second, "linuxruntime": 300 * time.Second,
	"diskio": 300 * time.Second, "thermal": 300 * time.Second, "btrfs": 300 * time.Second,
	"zfs": 300 * time.Second, "ceph": 300 * time.Second, "backup": 900 * time.Second,
	"packages": 3600 * time.Second, "certificates": 3600 * time.Second, "diskhealth": 3600 * time.Second,
	"inventory": 86400 * time.Second,
	"patroni":   60 * time.Second, "postgres": 300 * time.Second, "postgres-version": 86400 * time.Second,
	"nats": 60 * time.Second, "nats-jetstream": 300 * time.Second, "nats-version": 86400 * time.Second,
	"valkey": 60 * time.Second, "valkey-capacity": 300 * time.Second, "valkey-version": 86400 * time.Second,
	"clickhouse": 60 * time.Second, "clickhouse-capacity": 300 * time.Second, "clickhouse-version": 86400 * time.Second,
}

var desktopCadences = map[string]time.Duration{
	"system": 300 * time.Second, "systemd": 300 * time.Second, "kernel": 300 * time.Second,
	"filesystem": 900 * time.Second, "network": 900 * time.Second, "linuxruntime": 900 * time.Second,
	"diskio": 900 * time.Second, "thermal": 900 * time.Second, "btrfs": 900 * time.Second, "zfs": 900 * time.Second, "ceph": 900 * time.Second,
	"posture": 24 * time.Hour,
}

// validateCadenceNames accepts built-ins (including disabled optional modules)
// and only the scripts configured for this host.
func validateCadenceNames(cfg config.Config) error {
	valid := make(map[string]bool, len(linuxCadences)+len(cfg.Scripts))
	for name := range linuxCadences {
		valid[name] = true
	}
	valid["posture"] = true
	for _, script := range cfg.Scripts {
		valid["script:"+script.Name] = true
	}
	names := make([]string, 0, len(valid))
	for name := range valid {
		names = append(names, name)
	}
	sort.Strings(names)
	var unknown []string
	for name := range cfg.Runner.Cadence {
		if !valid[name] {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("unknown runner cadence collector(s): %s; valid names: %s", strings.Join(unknown, ", "), strings.Join(names, ", "))
	}
	return nil
}
