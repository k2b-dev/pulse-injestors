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

	"github.com/valentinkolb/pulse-injestors/internal/config"
	"github.com/valentinkolb/pulse-injestors/internal/entity"
	"github.com/valentinkolb/pulse-injestors/internal/modules/macos"
	"github.com/valentinkolb/pulse-injestors/internal/modules/script"
	"github.com/valentinkolb/pulse-injestors/internal/monitoring"
	"github.com/valentinkolb/pulse-injestors/internal/pulse"
)

var version = "dev"

type cli struct {
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/ingestor.toml when present." env:"PULSE_CONFIG"`

	IngestURL   string   `name:"ingest-url" help:"Pulse ingest endpoint URL." env:"PULSE_INGEST_URL"`
	IngestToken string   `name:"ingest-token" help:"Pulse ingest bearer token." env:"PULSE_INGEST_TOKEN"`
	EntityID    string   `name:"entity-id" help:"Stable monitored entity id. Defaults to hostname." env:"PULSE_ENTITY_ID"`
	EntityLabel string   `name:"entity-label" help:"Human-readable monitored host label. Defaults to hostname." env:"PULSE_ENTITY_LABEL"`
	Dimensions  []string `name:"dimension" help:"Bounded global dimension as key=value. Repeat for multiple values." env:"PULSE_DIMENSIONS" sep:","`

	IntervalSeconds         int `name:"interval-seconds" help:"Collection interval for run mode." env:"PULSE_INTERVAL_SECONDS"`
	CollectorTimeoutSeconds int `name:"collector-timeout-seconds" help:"Overall timeout per collector in seconds." env:"PULSE_COLLECTOR_TIMEOUT_SECONDS"`
	TimeoutSeconds          int `name:"timeout-seconds" help:"HTTP request timeout in seconds." env:"PULSE_HTTP_TIMEOUT_SECONDS"`
	MaxRetries              int `name:"max-retries" help:"HTTP retry count for network, 408, 429 and 5xx failures." env:"PULSE_HTTP_MAX_RETRIES"`
	InitialBackoffMS        int `name:"initial-backoff-ms" help:"Initial retry backoff in milliseconds." env:"PULSE_HTTP_INITIAL_BACKOFF_MS"`

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

	runner := monitoring.Runner{
		EntityID:   entity.HostID(cfg.Entity.ID),
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
	dimensions, err := config.ParseDimensions(c.Dimensions)
	if err != nil {
		return config.Config{}, err
	}
	cfg, err := config.Resolve(fileCfg, config.Overlay{
		ConfigPath:                   cfgPath,
		IngestURL:                    c.IngestURL,
		IngestToken:                  c.IngestToken,
		EntityID:                     c.EntityID,
		EntityLabel:                  c.EntityLabel,
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
	cfg.Entity.ID = entity.StableHostID(cfg.Entity.ID)
	if cfg.Entity.ID == "" {
		return config.Config{}, fmt.Errorf("entity id is required")
	}
	cfg.Entity.Type = "host"
	return cfg, nil
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
