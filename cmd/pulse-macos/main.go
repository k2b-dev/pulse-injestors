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
	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/modules/macos"
	"github.com/k2b-dev/pulse-injestors/internal/modules/script"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var version = "dev"

type cli struct {
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/pulse-macos.toml when present." env:"PULSE_CONFIG"`

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

	HomebrewTimeoutSeconds       int  `name:"homebrew-timeout-seconds" help:"Homebrew outdated timeout in seconds." env:"PULSE_MACOS_HOMEBREW_TIMEOUT_SECONDS"`
	SoftwareUpdateTimeoutSeconds int  `name:"softwareupdate-timeout-seconds" help:"softwareupdate timeout in seconds." env:"PULSE_MACOS_SOFTWAREUPDATE_TIMEOUT_SECONDS"`
	DisableSystemProfiler        bool `name:"disable-system-profiler" help:"Disable macOS system_profiler based collectors." env:"PULSE_MACOS_DISABLE_SYSTEM_PROFILER"`
	SystemProfilerTimeoutSeconds int  `name:"system-profiler-timeout-seconds" help:"system_profiler timeout in seconds." env:"PULSE_MACOS_SYSTEM_PROFILER_TIMEOUT_SECONDS"`

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
		kong.Name("pulse-macos"),
		kong.Description("Pulse macOS device monitoring ingestor."),
		kong.UsageOnError(),
		kong.Vars{"version": version},
	)
	if c.Version {
		fmt.Printf("pulse-macos %s\n", version)
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
		Resource: pulse.ResourceRef{Type: "host", ID: cfg.Resource.ID, Label: cfg.Resource.Label},
		Dimensions: monitoring.MergeDimensions(cfg.Dimensions, map[string]string{
			"host": cfg.Resource.ID,
		}),
		Collectors: collectors(cfg),
		Timeout:    cfg.CollectorTimeout(),
		Interval:   cfg.RunnerInterval(),
		Logger:     log,
	}

	err = agent.Run(ctx, agent.Options{
		Name:    "pulse-macos",
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
		cfgPath = config.DefaultConfigPath("pulse-macos")
	}
	fileCfg, err := config.LoadFile(cfgPath, explicitConfig)
	if err != nil {
		return config.Config{}, fmt.Errorf("load %s: %w", cfgPath, err)
	}
	dimensions, err := config.ParseDimensions(c.Dimensions)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Resolve(fileCfg, config.Overlay{
		SpoolDir:                     c.SpoolDir,
		SpoolMaxBytes:                c.SpoolMaxBytes,
		ConfigPath:                   cfgPath,
		IngestURL:                    c.IngestURL,
		IngestToken:                  os.Getenv("PULSE_INGEST_TOKEN"),
		IngestTokenFile:              c.IngestTokenFile,
		ResourceID:                   c.ResourceID,
		ResourceLabel:                c.ResourceLabel,
		Dimensions:                   dimensions,
		TimeoutSeconds:               c.TimeoutSeconds,
		MaxRetries:                   c.MaxRetries,
		InitialBackoffMS:             c.InitialBackoffMS,
		IntervalSeconds:              c.IntervalSeconds,
		CollectorTimeoutSeconds:      c.CollectorTimeoutSeconds,
		HomebrewTimeoutSeconds:       c.HomebrewTimeoutSeconds,
		SoftwareUpdateTimeoutSeconds: c.SoftwareUpdateTimeoutSeconds,
		DisableSystemProfiler:        c.DisableSystemProfiler,
		SystemProfilerTimeoutSeconds: c.SystemProfilerTimeoutSeconds,
		AllowMissingPulse:            c.Local,
	})
	if err != nil {
		return config.Config{}, err
	}
	cfg.Resource.ID = entity.StableHostID(cfg.Resource.ID)
	if cfg.Resource.ID == "" {
		return config.Config{}, fmt.Errorf("resource id is required")
	}
	cfg.Resource.Type = "host"
	return cfg, nil
}

func collectors(cfg config.Config) []monitoring.Collector {
	out := []monitoring.Collector{
		macos.Collector{
			EnableHomebrew:        cfg.HomebrewEnabled(),
			HomebrewTimeout:       cfg.HomebrewTimeout(),
			EnableSoftwareUpdate:  cfg.SoftwareUpdateEnabled(),
			SoftwareUpdateTimeout: cfg.SoftwareUpdateTimeout(),
			EnableSystemProfiler:  cfg.SystemProfilerEnabled(),
			SystemProfilerTimeout: cfg.SystemProfilerTimeout(),
		},
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
