package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/k2b-dev/pulse-injestors/internal/agent"
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
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/pulse-docker.toml when present." env:"PULSE_CONFIG"`

	IngestURL       string   `name:"ingest-url" help:"Pulse ingest endpoint URL." env:"PULSE_INGEST_URL"`
	IngestTokenFile string   `name:"ingest-token-file" help:"Private file with the Pulse source token. PULSE_INGEST_TOKEN is also read." env:"PULSE_INGEST_TOKEN_FILE"`
	ResourceID      string   `name:"resource-id" help:"Stable monitored host id. Defaults to the Docker daemon id for pulse-docker." env:"PULSE_RESOURCE_ID"`
	ResourceLabel   string   `name:"resource-label" help:"Human-readable monitored host label. Defaults to the Docker host name." env:"PULSE_RESOURCE_LABEL"`
	Dimensions      []string `name:"dimension" help:"Bounded global dimension as key=value. Repeat for multiple values." env:"PULSE_DIMENSIONS" sep:","`

	IntervalSeconds         int    `name:"interval-seconds" help:"Collection interval for run mode." env:"PULSE_INTERVAL_SECONDS"`
	CollectorTimeoutSeconds int    `name:"collector-timeout-seconds" help:"Overall timeout per collector in seconds." env:"PULSE_COLLECTOR_TIMEOUT_SECONDS"`
	TimeoutSeconds          int    `name:"timeout-seconds" help:"HTTP request timeout in seconds." env:"PULSE_HTTP_TIMEOUT_SECONDS"`
	SpoolDir                string `name:"spool-dir" help:"Private persistent delivery directory." env:"PULSE_SPOOL_DIR"`
	SpoolMaxBytes           int64  `name:"spool-max-bytes" help:"Maximum persistent delivery bytes; required with spool-dir." env:"PULSE_SPOOL_MAX_BYTES"`
	MaxRetries              int    `name:"max-retries" help:"HTTP retry count for network, 408, 409, 429 and 5xx failures." env:"PULSE_HTTP_MAX_RETRIES"`
	InitialBackoffMS        int    `name:"initial-backoff-ms" help:"Initial retry backoff in milliseconds." env:"PULSE_HTTP_INITIAL_BACKOFF_MS"`

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

	runner := &monitoring.Runner{
		Resource:   pulse.ResourceRef{Type: "host", ID: cfg.Resource.ID, Label: cfg.Resource.Label},
		Dimensions: cfg.Dimensions,
		Collectors: collectors(cfg),
		Timeout:    cfg.CollectorTimeout(),
		Interval:   cfg.RunnerInterval(),
		Logger:     log,
	}

	err = agent.Run(ctx, agent.Options{
		Name:    "pulse-docker",
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
		cfgPath = config.DefaultConfigPath("pulse-docker")
	}
	fileCfg, err := config.LoadFile(cfgPath, explicitConfig)
	if err != nil {
		return config.Config{}, fmt.Errorf("load %s: %w", cfgPath, err)
	}
	if c.ResourceID == "" && fileCfg.Resource.ID == "" {
		stableID, err := dockermodule.DaemonStableID(context.Background(), dockerSocketPath(c, fileCfg), 5*time.Second)
		if err != nil {
			return config.Config{}, fmt.Errorf("resolve stable docker host id: %w; set PULSE_RESOURCE_ID to a stable host id", err)
		}
		fileCfg.Resource.ID = stableID
		if fileCfg.Resource.Label == "" {
			fileCfg.Resource.Label = stableID
		}
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
	cfg.Resource.ID = dockermodule.NormalizeStableHostID(cfg.Resource.ID)
	if cfg.Resource.ID == "" {
		return config.Config{}, fmt.Errorf("resource id is required")
	}
	if c.ResourceLabel == "" && fileCfg.Resource.Label == "" {
		cfg.Resource.Label = cfg.Resource.ID
	}
	cfg.Resource.Type = "host"
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

func collectors(cfg config.Config) []monitoring.Collector {
	stableHostID := dockermodule.NormalizeStableHostID(cfg.Resource.ID)
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
