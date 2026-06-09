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
	"github.com/valentinkolb/pulse-injestors/internal/modules/ceph"
	"github.com/valentinkolb/pulse-injestors/internal/modules/proxmox"
	"github.com/valentinkolb/pulse-injestors/internal/modules/script"
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

	ProxmoxAPIURL             string `name:"proxmox-api-url" help:"Proxmox VE API base URL." env:"PULSE_PROXMOX_API_URL"`
	ProxmoxAPIToken           string `name:"proxmox-api-token" help:"Proxmox VE API token value." env:"PULSE_PROXMOX_API_TOKEN"`
	ProxmoxTimeoutSeconds     int    `name:"proxmox-timeout-seconds" help:"Proxmox API timeout in seconds." env:"PULSE_PROXMOX_TIMEOUT_SECONDS"`
	ProxmoxInsecureSkipVerify bool   `name:"proxmox-insecure-skip-verify" help:"Skip Proxmox API TLS verification." env:"PULSE_PROXMOX_INSECURE_SKIP_VERIFY"`
	ProxmoxEnableCephAPI      bool   `name:"proxmox-enable-ceph-api" help:"Collect Ceph data through the Proxmox API." env:"PULSE_PROXMOX_ENABLE_CEPH_API"`
	ProxmoxEnableLocalCeph    bool   `name:"proxmox-enable-local-ceph" help:"Also collect local Ceph CLI metrics on this node." env:"PULSE_PROXMOX_ENABLE_LOCAL_CEPH"`

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
		kong.Name("pulse-proxmox"),
		kong.Description("Pulse Proxmox VE monitoring ingestor."),
		kong.UsageOnError(),
		kong.Vars{"version": version},
	)
	if c.Version {
		fmt.Printf("pulse-proxmox %s\n", version)
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
	cfg, err := config.Resolve(fileCfg, config.Overlay{
		ConfigPath:                cfgPath,
		IngestURL:                 c.IngestURL,
		IngestToken:               c.IngestToken,
		EntityID:                  c.EntityID,
		EntityType:                c.EntityType,
		TimeoutSeconds:            c.TimeoutSeconds,
		MaxRetries:                c.MaxRetries,
		InitialBackoffMS:          c.InitialBackoffMS,
		IntervalSeconds:           c.IntervalSeconds,
		CollectorTimeoutSeconds:   c.CollectorTimeoutSeconds,
		ProxmoxAPIURL:             c.ProxmoxAPIURL,
		ProxmoxAPIToken:           c.ProxmoxAPIToken,
		ProxmoxTimeoutSeconds:     c.ProxmoxTimeoutSeconds,
		ProxmoxInsecureSkipVerify: c.ProxmoxInsecureSkipVerify,
		ProxmoxEnableCephAPI:      c.ProxmoxEnableCephAPI,
		ProxmoxEnableLocalCeph:    c.ProxmoxEnableLocalCeph,
		AllowMissingPulse:         c.Local,
	})
	if err != nil {
		return config.Config{}, err
	}
	if cfg.Proxmox.APIURL == "" {
		return config.Config{}, fmt.Errorf("proxmox api_url is required")
	}
	if cfg.Proxmox.APIToken == "" {
		return config.Config{}, fmt.Errorf("proxmox api_token is required")
	}
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
		proxmox.Collector{
			BaseURL:            cfg.Proxmox.APIURL,
			APIToken:           cfg.Proxmox.APIToken,
			Timeout:            cfg.ProxmoxTimeout(),
			InsecureSkipVerify: cfg.Proxmox.InsecureSkipVerify,
			EnableCephAPI:      cfg.Proxmox.EnableCephAPI,
		},
	}
	if cfg.Proxmox.EnableLocalCeph {
		out = append(out, ceph.Collector{Timeout: 5 * time.Second})
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
