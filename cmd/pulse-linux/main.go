package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/valentinkolb/pulse-injestors/internal/config"
	"github.com/valentinkolb/pulse-injestors/internal/modules/btrfs"
	"github.com/valentinkolb/pulse-injestors/internal/modules/diskhealth"
	"github.com/valentinkolb/pulse-injestors/internal/modules/filesystem"
	"github.com/valentinkolb/pulse-injestors/internal/modules/network"
	"github.com/valentinkolb/pulse-injestors/internal/modules/packages"
	"github.com/valentinkolb/pulse-injestors/internal/modules/script"
	"github.com/valentinkolb/pulse-injestors/internal/modules/system"
	"github.com/valentinkolb/pulse-injestors/internal/modules/systemd"
	"github.com/valentinkolb/pulse-injestors/internal/modules/thermal"
	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

var version = "dev"

type cli struct {
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/ingestor.toml when present." env:"PULSE_CONFIG"`

	IngestURL   string `name:"ingest-url" help:"Pulse ingest endpoint URL." env:"PULSE_INGEST_URL"`
	IngestToken string `name:"ingest-token" help:"Pulse ingest bearer token." env:"PULSE_INGEST_TOKEN"`
	EntityID    string `name:"entity-id" help:"Stable monitored entity id. Defaults to hostname." env:"PULSE_ENTITY_ID"`
	EntityType  string `name:"entity-type" help:"Monitored entity type." env:"PULSE_ENTITY_TYPE"`

	IntervalSeconds         int `name:"interval-seconds" help:"Collection interval for run mode." env:"PULSE_INTERVAL_SECONDS"`
	CollectorTimeoutSeconds int `name:"collector-timeout-seconds" help:"Overall timeout per collector in seconds." env:"PULSE_COLLECTOR_TIMEOUT_SECONDS"`
	TimeoutSeconds          int `name:"timeout-seconds" help:"HTTP request timeout in seconds." env:"PULSE_HTTP_TIMEOUT_SECONDS"`
	MaxRetries              int `name:"max-retries" help:"HTTP retry count for network, 408, 429 and 5xx failures." env:"PULSE_HTTP_MAX_RETRIES"`
	InitialBackoffMS        int `name:"initial-backoff-ms" help:"Initial retry backoff in milliseconds." env:"PULSE_HTTP_INITIAL_BACKOFF_MS"`

	ProcRoot    string `name:"proc-root" help:"procfs root to read." env:"PULSE_HOST_PROC_ROOT"`
	SysRoot     string `name:"sys-root" help:"sysfs root to read." env:"PULSE_HOST_SYS_ROOT"`
	HostRoot    string `name:"host-root" help:"Host root filesystem." env:"PULSE_HOST_ROOT"`
	CPUSampleMS int    `name:"cpu-sample-ms" help:"CPU usage sample window in milliseconds." env:"PULSE_HOST_CPU_SAMPLE_MS"`

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

	runner := monitoring.Runner{
		EntityID:   cfg.Entity.ID,
		EntityType: cfg.Entity.Type,
		Dimensions: map[string]string{
			"host": cfg.Entity.ID,
		},
		Collectors: collectors(cfg),
		Sender:     sender(c, cfg, log),
		Timeout:    cfg.CollectorTimeout(),
		Interval:   cfg.RunnerInterval(),
		Logger:     log,
	}

	switch kctx.Command() {
	case "run":
		if err := runner.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("run", "err", err)
			os.Exit(1)
		}
	default:
		if err := runner.Once(ctx); err != nil {
			log.Error("once", "err", err)
			os.Exit(1)
		}
	}
}

func loadConfig(c cli) (config.Config, error) {
	cfgPath := c.Config
	explicitConfig := cfgPath != ""
	if cfgPath == "" {
		cfgPath = config.DefaultConfigPath
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
	return config.Resolve(fileCfg, config.Overlay{
		ConfigPath:                    cfgPath,
		IngestURL:                     c.IngestURL,
		IngestToken:                   c.IngestToken,
		EntityID:                      c.EntityID,
		EntityType:                    c.EntityType,
		TimeoutSeconds:                c.TimeoutSeconds,
		MaxRetries:                    c.MaxRetries,
		InitialBackoffMS:              c.InitialBackoffMS,
		IntervalSeconds:               c.IntervalSeconds,
		CollectorTimeoutSeconds:       c.CollectorTimeoutSeconds,
		ProcRoot:                      c.ProcRoot,
		SysRoot:                       c.SysRoot,
		HostRoot:                      c.HostRoot,
		CPUSampleMS:                   c.CPUSampleMS,
		LinuxSystemdUnits:             splitCSV(c.SystemdUnits),
		LinuxPackageTimeoutSeconds:    c.PackageTimeoutSeconds,
		LinuxDiskHealthTimeoutSeconds: c.DiskHealthTimeoutSeconds,
		AllowMissingPulse:             c.Local,
	})
}

func sender(c cli, cfg config.Config, log *slog.Logger) monitoring.Sender {
	if c.Local {
		return monitoring.StdoutSender{Writer: os.Stdout, Pretty: true, Report: c.Pretty}
	}
	return pulse.Client{
		URL:            cfg.Pulse.IngestURL,
		Token:          cfg.Pulse.IngestToken,
		HTTPClient:     &http.Client{Timeout: cfg.HTTPTimeout()},
		MaxRetries:     cfg.HTTP.MaxRetries,
		InitialBackoff: cfg.InitialBackoff(),
		Logger:         log,
	}
}

func collectors(cfg config.Config) []monitoring.Collector {
	out := []monitoring.Collector{
		system.Collector{ProcRoot: cfg.Host.ProcRoot, CPUSampleTime: cfg.CPUSampleWindow()},
		filesystem.Collector{ProcRoot: cfg.Host.ProcRoot, HostRoot: cfg.Host.Root},
		network.Collector{ProcRoot: cfg.Host.ProcRoot},
		packages.Collector{Timeout: time.Duration(cfg.Linux.PackageTimeoutSeconds) * time.Second},
		systemd.Collector{Units: cfg.Linux.SystemdUnits, Timeout: 5 * time.Second},
		diskhealth.Collector{Timeout: time.Duration(cfg.Linux.DiskHealthTimeoutSeconds) * time.Second},
	}
	if cfg.ThermalEnabled() {
		out = append(out, thermal.Collector{SysRoot: cfg.Host.SysRoot})
	}
	if cfg.BtrfsEnabled() {
		out = append(out, btrfs.Collector{ProcRoot: cfg.Host.ProcRoot, HostRoot: cfg.Host.Root, Timeout: 5 * time.Second})
	}
	if len(cfg.Scripts) > 0 {
		scripts := make([]script.Script, 0, len(cfg.Scripts))
		for _, s := range cfg.Scripts {
			scripts = append(scripts, script.Script{
				Name:           s.Name,
				Command:        s.Command,
				Timeout:        time.Duration(s.TimeoutSeconds) * time.Second,
				MaxOutputBytes: s.MaxOutputBytes,
				Dimensions:     s.Dimensions,
			})
		}
		out = append(out, script.Collector{Scripts: scripts})
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
