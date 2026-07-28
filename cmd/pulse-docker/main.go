package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/modules/btrfs"
	dockermodule "github.com/k2b-dev/pulse-injestors/internal/modules/docker"
	"github.com/k2b-dev/pulse-injestors/internal/modules/filesystem"
	"github.com/k2b-dev/pulse-injestors/internal/modules/script"
	"github.com/k2b-dev/pulse-injestors/internal/modules/system"
	"github.com/k2b-dev/pulse-injestors/internal/modules/thermal"
	"github.com/k2b-dev/pulse-injestors/internal/modules/zfs"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var version = "dev"

type cli struct {
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/ingestor.toml when present." env:"PULSE_CONFIG"`

	IngestURL   string   `name:"ingest-url" help:"Pulse ingest endpoint URL." env:"PULSE_INGEST_URL"`
	IngestToken string   `name:"ingest-token" help:"Pulse ingest bearer token." env:"PULSE_INGEST_TOKEN"`
	EntityID    string   `name:"entity-id" help:"Stable monitored host id. Defaults to the Docker daemon id for pulse-docker." env:"PULSE_ENTITY_ID"`
	EntityLabel string   `name:"entity-label" help:"Human-readable monitored host label. Defaults to the Docker host name." env:"PULSE_ENTITY_LABEL"`
	Dimensions  []string `name:"dimension" help:"Bounded global dimension as key=value. Repeat for multiple values." env:"PULSE_DIMENSIONS" sep:","`

	IntervalSeconds         int `name:"interval-seconds" help:"Collection interval for run mode." env:"PULSE_INTERVAL_SECONDS"`
	CollectorTimeoutSeconds int `name:"collector-timeout-seconds" help:"Overall timeout per collector in seconds." env:"PULSE_COLLECTOR_TIMEOUT_SECONDS"`
	TimeoutSeconds          int `name:"timeout-seconds" help:"HTTP request timeout in seconds." env:"PULSE_HTTP_TIMEOUT_SECONDS"`
	MaxRetries              int `name:"max-retries" help:"HTTP retry count for network, 408, 429 and 5xx failures." env:"PULSE_HTTP_MAX_RETRIES"`
	InitialBackoffMS        int `name:"initial-backoff-ms" help:"Initial retry backoff in milliseconds." env:"PULSE_HTTP_INITIAL_BACKOFF_MS"`

	ProcRoot                      string `name:"proc-root" help:"Host procfs root to read." env:"PULSE_HOST_PROC_ROOT"`
	SysRoot                       string `name:"sys-root" help:"Host sysfs root to read." env:"PULSE_HOST_SYS_ROOT"`
	HostRoot                      string `name:"host-root" help:"Host root filesystem mount." env:"PULSE_HOST_ROOT"`
	CPUSampleMS                   int    `name:"cpu-sample-ms" help:"CPU usage sample window in milliseconds." env:"PULSE_HOST_CPU_SAMPLE_MS"`
	DockerSocketPath              string `name:"docker-socket" help:"Docker Engine unix socket." env:"PULSE_DOCKER_SOCKET"`
	DockerHostRoot                string `name:"docker-host-root" help:"Host root used to stat Docker mount sources." env:"PULSE_DOCKER_HOST_ROOT"`
	DockerConcurrency             int    `name:"docker-concurrency" help:"Max concurrent Docker container stats requests." env:"PULSE_DOCKER_CONCURRENCY"`
	DockerContainerTimeoutSeconds int    `name:"docker-container-timeout-seconds" help:"Per-container Docker stats/inspect timeout." env:"PULSE_DOCKER_CONTAINER_TIMEOUT_SECONDS"`
	DockerEnableRegistryChecks    bool   `name:"docker-enable-registry-checks" help:"Check remote registry digests for tagged images." env:"PULSE_DOCKER_ENABLE_REGISTRY_CHECKS"`
	DockerRegistryTimeoutSeconds  int    `name:"docker-registry-timeout-seconds" help:"Per-image registry manifest check timeout." env:"PULSE_DOCKER_REGISTRY_TIMEOUT_SECONDS"`

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
		kong.Name("pulse-docker"),
		kong.Description("Pulse Docker-host monitoring ingestor."),
		kong.UsageOnError(),
		kong.Vars{"version": version},
	)
	if c.Version {
		fmt.Printf("pulse-docker %s\n", version)
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
		EntityID:   "host:" + cfg.Entity.ID,
		EntityType: "host",
		Label:      cfg.Entity.Label,
		Dimensions: monitoring.MergeDimensions(cfg.Dimensions, map[string]string{
			"host": cfg.Entity.ID,
		}),
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
	if c.EntityID == "" && fileCfg.Entity.ID == "" {
		stableID, err := dockermodule.DaemonStableID(context.Background(), dockerSocketPath(c, fileCfg), 5*time.Second)
		if err != nil {
			return config.Config{}, fmt.Errorf("resolve stable docker host id: %w; set PULSE_ENTITY_ID to a stable host id", err)
		}
		fileCfg.Entity.ID = stableID
		if fileCfg.Entity.Label == "" {
			fileCfg.Entity.Label = stableID
		}
	}
	dimensions, err := config.ParseDimensions(c.Dimensions)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Resolve(fileCfg, config.Overlay{
		ConfigPath:                    cfgPath,
		IngestURL:                     c.IngestURL,
		IngestToken:                   c.IngestToken,
		EntityID:                      c.EntityID,
		EntityLabel:                   c.EntityLabel,
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
		DockerSocketPath:              c.DockerSocketPath,
		DockerHostRoot:                c.DockerHostRoot,
		DockerConcurrency:             c.DockerConcurrency,
		DockerContainerTimeoutSeconds: c.DockerContainerTimeoutSeconds,
		DockerEnableRegistryChecks:    c.DockerEnableRegistryChecks,
		DockerRegistryTimeoutSeconds:  c.DockerRegistryTimeoutSeconds,
		AllowMissingPulse:             c.Local,
	})
	if err != nil {
		return config.Config{}, err
	}
	cfg.Entity.ID = dockermodule.NormalizeStableHostID(cfg.Entity.ID)
	if cfg.Entity.ID == "" {
		return config.Config{}, fmt.Errorf("entity id is required")
	}
	if c.EntityLabel == "" && fileCfg.Entity.Label == "" {
		cfg.Entity.Label = cfg.Entity.ID
	}
	cfg.Entity.Type = "host"
	return cfg, nil
}

func dockerSocketPath(c cli, fileCfg config.Config) string {
	if c.DockerSocketPath != "" {
		return c.DockerSocketPath
	}
	if fileCfg.Docker.SocketPath != "" {
		return fileCfg.Docker.SocketPath
	}
	return "/var/run/docker.sock"
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
	stableHostID := dockermodule.NormalizeStableHostID(cfg.Entity.ID)
	out := []monitoring.Collector{
		system.Collector{ProcRoot: cfg.Host.ProcRoot, CPUSampleTime: cfg.CPUSampleWindow()},
		filesystem.Collector{ProcRoot: cfg.Host.ProcRoot, HostRoot: cfg.Host.Root},
		dockermodule.Collector{
			SocketPath:       cfg.Docker.SocketPath,
			HostRoot:         cfg.Docker.HostRoot,
			StableHostID:     stableHostID,
			Timeout:          cfg.HTTPTimeout(),
			ContainerTimeout: time.Duration(cfg.Docker.ContainerTimeoutSeconds) * time.Second,
			Concurrency:      cfg.Docker.Concurrency,
			RegistryChecks:   cfg.Docker.EnableRegistryChecks,
			RegistryTimeout:  time.Duration(cfg.Docker.RegistryTimeoutSeconds) * time.Second,
		},
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
