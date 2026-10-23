package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alecthomas/kong"

	"github.com/k2b-dev/pulse-injestors/internal/agent"
	"github.com/k2b-dev/pulse-injestors/internal/config"
	"github.com/k2b-dev/pulse-injestors/internal/entity"
	"github.com/k2b-dev/pulse-injestors/internal/modules/uptime"
	"github.com/k2b-dev/pulse-injestors/internal/monitoring"
	"github.com/k2b-dev/pulse-injestors/internal/pulse"
)

var version = "dev"

type cli struct {
	Config string `name:"config" help:"TOML config path. Defaults to /etc/pulse/pulse-uptime.toml when present." env:"PULSE_CONFIG"`

	IngestURL       string   `name:"ingest-url" help:"Pulse ingest endpoint URL." env:"PULSE_INGEST_URL"`
	IngestTokenFile string   `name:"ingest-token-file" help:"Private file with the Pulse source token. PULSE_INGEST_TOKEN is also read." env:"PULSE_INGEST_TOKEN_FILE"`
	ResourceID      string   `name:"resource-id" help:"Stable uptime probe id. Defaults to hostname." env:"PULSE_RESOURCE_ID"`
	ResourceLabel   string   `name:"resource-label" help:"Human-readable uptime probe label. Defaults to hostname." env:"PULSE_RESOURCE_LABEL"`
	Dimensions      []string `name:"dimension" help:"Bounded global dimension as key=value. Repeat for multiple values." env:"PULSE_DIMENSIONS" sep:","`

	IntervalSeconds         int    `name:"interval-seconds" help:"Collection interval for run mode." env:"PULSE_INTERVAL_SECONDS"`
	CollectorTimeoutSeconds int    `name:"collector-timeout-seconds" help:"Overall timeout for the uptime collector in seconds." env:"PULSE_COLLECTOR_TIMEOUT_SECONDS"`
	TimeoutSeconds          int    `name:"timeout-seconds" help:"Pulse HTTP request timeout in seconds." env:"PULSE_HTTP_TIMEOUT_SECONDS"`
	SpoolDir                string `name:"spool-dir" help:"Private persistent delivery directory." env:"PULSE_SPOOL_DIR"`
	SpoolMaxBytes           int64  `name:"spool-max-bytes" help:"Maximum persistent delivery bytes; required with spool-dir." env:"PULSE_SPOOL_MAX_BYTES"`
	MaxRetries              int    `name:"max-retries" help:"Pulse HTTP retry count for network, 408, 409, 429 and 5xx failures." env:"PULSE_HTTP_MAX_RETRIES"`
	InitialBackoffMS        int    `name:"initial-backoff-ms" help:"Initial Pulse retry backoff in milliseconds." env:"PULSE_HTTP_INITIAL_BACKOFF_MS"`

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
	runner := &monitoring.Runner{
		Resource: pulse.ResourceRef{Type: "uptime-probe", ID: cfg.Resource.ID, Label: cfg.Resource.Label},
		Dimensions: monitoring.MergeDimensions(cfg.Dimensions, map[string]string{
			"probe": cfg.Resource.ID,
		}),
		Collectors: (uptime.Collector{
			Targets:     targets,
			Concurrency: cfg.Uptime.Concurrency,
			Timeout:     cfg.UptimeTimeout(),
		}).Scheduled(),
		Timeout:  cfg.CollectorTimeout(),
		Interval: cfg.RunnerInterval(),
		Logger:   log,
	}

	err = agent.Run(ctx, agent.Options{
		Name:    "pulse-uptime",
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
		cfgPath = config.DefaultConfigPath("pulse-uptime")
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
		SpoolDir:                c.SpoolDir,
		SpoolMaxBytes:           c.SpoolMaxBytes,
		ConfigPath:              cfgPath,
		IngestURL:               c.IngestURL,
		IngestToken:             os.Getenv("PULSE_INGEST_TOKEN"),
		IngestTokenFile:         c.IngestTokenFile,
		ResourceID:              c.ResourceID,
		ResourceLabel:           c.ResourceLabel,
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
	cfg.Resource.ID = entity.StableHostID(cfg.Resource.ID)
	if cfg.Resource.ID == "" {
		return config.Config{}, fmt.Errorf("resource id is required")
	}
	cfg.Resource.Type = "uptime-probe"
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
		if strings.ToLower(strings.TrimSpace(target.Kind)) == uptime.KindPorts {
			timeout = 900 * time.Second
		}
		if target.TimeoutSeconds > 0 {
			timeout = time.Duration(target.TimeoutSeconds) * time.Second
		}
		var records []uptime.Record
		for _, record := range target.Records {
			records = append(records, uptime.Record{Type: strings.ToUpper(record.Type), Expect: record.Expect, Contains: record.Contains})
		}
		targets = append(targets, uptime.Target{
			Cadence: time.Duration(target.CadenceSeconds) * time.Second,
			Name:    target.Name, Resolver: target.Resolver, Expected: target.Expected, Range: target.Range,
			Records: records, CAFile: target.CAFile, ServerName: target.ServerName, WarnDays: target.WarnDays, Lists: target.Lists,
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
