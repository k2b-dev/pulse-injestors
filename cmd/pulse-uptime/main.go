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

	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/modules/uptime"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var version = "dev"

type cli struct {
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/ingestor.toml when present." env:"PULSE_CONFIG"`

	IngestURL   string   `name:"ingest-url" help:"Pulse ingest endpoint URL." env:"PULSE_INGEST_URL"`
	IngestToken string   `name:"ingest-token" help:"Pulse ingest bearer token." env:"PULSE_INGEST_TOKEN"`
	EntityID    string   `name:"entity-id" help:"Stable uptime probe id. Defaults to hostname." env:"PULSE_ENTITY_ID"`
	EntityLabel string   `name:"entity-label" help:"Human-readable uptime probe label. Defaults to hostname." env:"PULSE_ENTITY_LABEL"`
	Dimensions  []string `name:"dimension" help:"Bounded global dimension as key=value. Repeat for multiple values." env:"PULSE_DIMENSIONS" sep:","`

	IntervalSeconds         int `name:"interval-seconds" help:"Collection interval for run mode." env:"PULSE_INTERVAL_SECONDS"`
	CollectorTimeoutSeconds int `name:"collector-timeout-seconds" help:"Overall timeout for the uptime collector in seconds." env:"PULSE_COLLECTOR_TIMEOUT_SECONDS"`
	TimeoutSeconds          int `name:"timeout-seconds" help:"Pulse HTTP request timeout in seconds." env:"PULSE_HTTP_TIMEOUT_SECONDS"`
	MaxRetries              int `name:"max-retries" help:"Pulse HTTP retry count for network, 408, 429 and 5xx failures." env:"PULSE_HTTP_MAX_RETRIES"`
	InitialBackoffMS        int `name:"initial-backoff-ms" help:"Initial Pulse retry backoff in milliseconds." env:"PULSE_HTTP_INITIAL_BACKOFF_MS"`

	DisableDefaults     bool `name:"disable-defaults" help:"Disable the built-in internet connectivity targets." env:"PULSE_UPTIME_DISABLE_DEFAULTS"`
	CheckConcurrency    int  `name:"check-concurrency" help:"Maximum concurrent uptime checks." env:"PULSE_UPTIME_CONCURRENCY"`
	CheckTimeoutSeconds int  `name:"check-timeout-seconds" help:"Default timeout per uptime check." env:"PULSE_UPTIME_TIMEOUT_SECONDS"`

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
		kong.Name("pulse-uptime"),
		kong.Description("Pulse internet connectivity and endpoint uptime ingestor."),
		kong.UsageOnError(),
		kong.Vars{"version": version},
	)
	if c.Version {
		fmt.Printf("pulse-uptime %s\n", version)
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
	targets, err := configuredTargets(cfg)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	runner := monitoring.Runner{
		EntityID:   entity.ID("uptime-probe", cfg.Entity.ID),
		EntityType: "uptime-probe",
		Label:      cfg.Entity.Label,
		Dimensions: monitoring.MergeDimensions(cfg.Dimensions, map[string]string{
			"probe": cfg.Entity.ID,
		}),
		Collectors: []monitoring.Collector{uptime.Collector{
			Targets:     targets,
			Concurrency: cfg.Uptime.Concurrency,
			Timeout:     cfg.UptimeTimeout(),
		}},
		Sender:   sender(c, cfg, log),
		Timeout:  cfg.CollectorTimeout(),
		Interval: cfg.RunnerInterval(),
		Logger:   log,
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
		ConfigPath:              cfgPath,
		IngestURL:               c.IngestURL,
		IngestToken:             c.IngestToken,
		EntityID:                c.EntityID,
		EntityLabel:             c.EntityLabel,
		Dimensions:              dimensions,
		TimeoutSeconds:          c.TimeoutSeconds,
		MaxRetries:              c.MaxRetries,
		InitialBackoffMS:        c.InitialBackoffMS,
		IntervalSeconds:         c.IntervalSeconds,
		CollectorTimeoutSeconds: c.CollectorTimeoutSeconds,
		UptimeDisableDefaults:   c.DisableDefaults,
		UptimeConcurrency:       c.CheckConcurrency,
		UptimeTimeoutSeconds:    c.CheckTimeoutSeconds,
		AllowMissingPulse:       c.Local,
	})
	if err != nil {
		return config.Config{}, err
	}
	cfg.Entity.ID = entity.StableHostID(cfg.Entity.ID)
	if cfg.Entity.ID == "" {
		return config.Config{}, fmt.Errorf("entity id is required")
	}
	cfg.Entity.Type = "uptime-probe"
	return cfg, nil
}

func configuredTargets(cfg config.Config) ([]uptime.Target, error) {
	var targets []uptime.Target
	if cfg.UptimeDefaultsEnabled() {
		targets = append(targets, uptime.DefaultTargets(cfg.UptimeTimeout())...)
	}
	seen := make(map[string]bool, len(targets)+len(cfg.Uptime.Targets))
	for _, target := range targets {
		seen[target.ID] = true
	}
	for _, target := range cfg.Uptime.Targets {
		if seen[target.ID] {
			return nil, fmt.Errorf("uptime target id %q conflicts with a built-in target; disable defaults or choose another id", target.ID)
		}
		seen[target.ID] = true
		timeout := cfg.UptimeTimeout()
		if target.TimeoutSeconds > 0 {
			timeout = time.Duration(target.TimeoutSeconds) * time.Second
		}
		targets = append(targets, uptime.Target{
			ID:             target.ID,
			Label:          strings.TrimSpace(target.Label),
			Kind:           strings.ToLower(strings.TrimSpace(target.Kind)),
			Address:        strings.TrimSpace(target.Address),
			ExpectedStatus: target.ExpectedStatus,
			Timeout:        timeout,
		})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one uptime target is required")
	}
	return targets, nil
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
