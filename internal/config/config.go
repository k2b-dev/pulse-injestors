package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

const DefaultConfigPath = "/etc/pulse/ingestor.toml"

type Config struct {
	Pulse   PulseConfig    `toml:"pulse"`
	Entity  EntityConfig   `toml:"entity"`
	HTTP    HTTPConfig     `toml:"http"`
	Runner  RunnerConfig   `toml:"runner"`
	Host    HostConfig     `toml:"host"`
	Docker  DockerConfig   `toml:"docker"`
	Linux   LinuxConfig    `toml:"linux"`
	MacOS   MacOSConfig    `toml:"macos"`
	Scripts []ScriptConfig `toml:"script"`
}

type PulseConfig struct {
	IngestURL   string `toml:"ingest_url"`
	IngestToken string `toml:"ingest_token"`
}

type EntityConfig struct {
	ID   string `toml:"id"`
	Type string `toml:"type"`
}

type HTTPConfig struct {
	TimeoutSeconds   int `toml:"timeout_seconds"`
	MaxRetries       int `toml:"max_retries"`
	InitialBackoffMS int `toml:"initial_backoff_ms"`
}

type RunnerConfig struct {
	IntervalSeconds         int `toml:"interval_seconds"`
	CollectorTimeoutSeconds int `toml:"collector_timeout_seconds"`
}

type HostConfig struct {
	ProcRoot      string `toml:"proc_root"`
	SysRoot       string `toml:"sys_root"`
	Root          string `toml:"root"`
	CPUSampleMS   int    `toml:"cpu_sample_ms"`
	EnableThermal *bool  `toml:"enable_thermal"`
	EnableBtrfs   *bool  `toml:"enable_btrfs"`
	EnableZfs     *bool  `toml:"enable_zfs"`
	EnableCeph    *bool  `toml:"enable_ceph"`
}

type DockerConfig struct {
	SocketPath              string `toml:"socket_path"`
	HostRoot                string `toml:"host_root"`
	Concurrency             int    `toml:"concurrency"`
	ContainerTimeoutSeconds int    `toml:"container_timeout_seconds"`
	EnableRegistryChecks    bool   `toml:"enable_registry_checks"`
	RegistryTimeoutSeconds  int    `toml:"registry_timeout_seconds"`
}

type LinuxConfig struct {
	Profile                  string   `toml:"profile"`
	SystemdUnits             []string `toml:"systemd_units"`
	PackageTimeoutSeconds    int      `toml:"package_timeout_seconds"`
	DiskHealthTimeoutSeconds int      `toml:"disk_health_timeout_seconds"`
}

type MacOSConfig struct {
	EnableHomebrew               *bool `toml:"enable_homebrew"`
	HomebrewTimeoutSeconds       int   `toml:"homebrew_timeout_seconds"`
	EnableSoftwareUpdate         *bool `toml:"enable_software_update"`
	SoftwareUpdateTimeoutSeconds int   `toml:"software_update_timeout_seconds"`
}

type ScriptConfig struct {
	Name           string            `toml:"name"`
	Command        []string          `toml:"command"`
	TimeoutSeconds int               `toml:"timeout_seconds"`
	MaxOutputBytes int64             `toml:"max_output_bytes"`
	Dimensions     map[string]string `toml:"dimensions"`
}

type Overlay struct {
	ConfigPath                    string
	IngestURL                     string
	IngestToken                   string
	EntityID                      string
	EntityType                    string
	TimeoutSeconds                int
	MaxRetries                    int
	InitialBackoffMS              int
	IntervalSeconds               int
	CollectorTimeoutSeconds       int
	ProcRoot                      string
	SysRoot                       string
	HostRoot                      string
	CPUSampleMS                   int
	DockerSocketPath              string
	DockerHostRoot                string
	DockerConcurrency             int
	DockerContainerTimeoutSeconds int
	DockerEnableRegistryChecks    bool
	DockerRegistryTimeoutSeconds  int
	LinuxProfile                  string
	LinuxSystemdUnits             []string
	LinuxPackageTimeoutSeconds    int
	LinuxDiskHealthTimeoutSeconds int
	HomebrewTimeoutSeconds        int
	SoftwareUpdateTimeoutSeconds  int
	AllowMissingPulse             bool
}

func LoadFile(path string, explicit bool) (Config, error) {
	var cfg Config
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !explicit {
			return cfg, nil
		}
		return cfg, err
	}
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func Resolve(file Config, overlay Overlay) (Config, error) {
	cfg := defaults()
	merge(&cfg, file)
	applyOverlay(&cfg, overlay)

	if cfg.Entity.ID == "" {
		host, err := os.Hostname()
		if err != nil || host == "" {
			return cfg, errors.New("entity id is required when hostname cannot be resolved")
		}
		cfg.Entity.ID = host
	}
	if !overlay.AllowMissingPulse {
		if cfg.Pulse.IngestURL == "" {
			return cfg, errors.New("pulse ingest url is required")
		}
		if cfg.Pulse.IngestToken == "" {
			return cfg, errors.New("pulse ingest token is required")
		}
		if err := validateHTTPURL(cfg.Pulse.IngestURL); err != nil {
			return cfg, err
		}
	} else if cfg.Pulse.IngestURL != "" {
		if err := validateHTTPURL(cfg.Pulse.IngestURL); err != nil {
			return cfg, err
		}
	}
	if cfg.HTTP.TimeoutSeconds <= 0 {
		return cfg, errors.New("http timeout_seconds must be positive")
	}
	if cfg.HTTP.MaxRetries < 0 {
		return cfg, errors.New("http max_retries must not be negative")
	}
	if cfg.HTTP.InitialBackoffMS <= 0 {
		return cfg, errors.New("http initial_backoff_ms must be positive")
	}
	if cfg.Runner.IntervalSeconds <= 0 {
		return cfg, errors.New("runner interval_seconds must be positive")
	}
	if cfg.Runner.CollectorTimeoutSeconds <= 0 {
		return cfg, errors.New("runner collector_timeout_seconds must be positive")
	}
	if cfg.Host.CPUSampleMS <= 0 {
		return cfg, errors.New("host cpu_sample_ms must be positive")
	}
	if cfg.MacOS.HomebrewTimeoutSeconds <= 0 {
		return cfg, errors.New("macos homebrew_timeout_seconds must be positive")
	}
	if cfg.MacOS.SoftwareUpdateTimeoutSeconds <= 0 {
		return cfg, errors.New("macos software_update_timeout_seconds must be positive")
	}
	if cfg.Docker.Concurrency <= 0 {
		return cfg, errors.New("docker concurrency must be positive")
	}
	if cfg.Docker.ContainerTimeoutSeconds <= 0 {
		return cfg, errors.New("docker container_timeout_seconds must be positive")
	}
	if cfg.Docker.RegistryTimeoutSeconds <= 0 {
		return cfg, errors.New("docker registry_timeout_seconds must be positive")
	}
	if cfg.Linux.PackageTimeoutSeconds <= 0 {
		return cfg, errors.New("linux package_timeout_seconds must be positive")
	}
	if cfg.Linux.DiskHealthTimeoutSeconds <= 0 {
		return cfg, errors.New("linux disk_health_timeout_seconds must be positive")
	}
	for _, script := range cfg.Scripts {
		if script.Name == "" {
			return cfg, errors.New("script name is required")
		}
		if len(script.Command) == 0 {
			return cfg, fmt.Errorf("script %q command is required", script.Name)
		}
	}
	return cfg, nil
}

func (c Config) HTTPTimeout() time.Duration {
	return time.Duration(c.HTTP.TimeoutSeconds) * time.Second
}

func (c Config) InitialBackoff() time.Duration {
	return time.Duration(c.HTTP.InitialBackoffMS) * time.Millisecond
}

func (c Config) CPUSampleWindow() time.Duration {
	return time.Duration(c.Host.CPUSampleMS) * time.Millisecond
}

func (c Config) RunnerInterval() time.Duration {
	return time.Duration(c.Runner.IntervalSeconds) * time.Second
}

func (c Config) CollectorTimeout() time.Duration {
	return time.Duration(c.Runner.CollectorTimeoutSeconds) * time.Second
}

func (c Config) ThermalEnabled() bool {
	return c.Host.EnableThermal == nil || *c.Host.EnableThermal
}

func (c Config) BtrfsEnabled() bool {
	return c.Host.EnableBtrfs == nil || *c.Host.EnableBtrfs
}

func (c Config) ZfsEnabled() bool {
	return c.Host.EnableZfs == nil || *c.Host.EnableZfs
}

func (c Config) CephEnabled() bool {
	return c.Host.EnableCeph != nil && *c.Host.EnableCeph
}

func (c Config) HomebrewEnabled() bool {
	return c.MacOS.EnableHomebrew == nil || *c.MacOS.EnableHomebrew
}

func (c Config) SoftwareUpdateEnabled() bool {
	return c.MacOS.EnableSoftwareUpdate != nil && *c.MacOS.EnableSoftwareUpdate
}

func (c Config) HomebrewTimeout() time.Duration {
	return time.Duration(c.MacOS.HomebrewTimeoutSeconds) * time.Second
}

func (c Config) SoftwareUpdateTimeout() time.Duration {
	return time.Duration(c.MacOS.SoftwareUpdateTimeoutSeconds) * time.Second
}

func defaults() Config {
	return Config{
		Entity: EntityConfig{
			Type: "host",
		},
		HTTP: HTTPConfig{
			TimeoutSeconds:   10,
			MaxRetries:       3,
			InitialBackoffMS: 500,
		},
		Runner: RunnerConfig{
			IntervalSeconds:         60,
			CollectorTimeoutSeconds: 60,
		},
		Host: HostConfig{
			ProcRoot:    "/host/proc",
			SysRoot:     "/host/sys",
			Root:        "/host/root",
			CPUSampleMS: 250,
		},
		Docker: DockerConfig{
			SocketPath:              "/var/run/docker.sock",
			HostRoot:                "/host/root",
			Concurrency:             4,
			ContainerTimeoutSeconds: 10,
			RegistryTimeoutSeconds:  10,
		},
		Linux: LinuxConfig{
			PackageTimeoutSeconds:    20,
			DiskHealthTimeoutSeconds: 20,
		},
		MacOS: MacOSConfig{
			HomebrewTimeoutSeconds:       20,
			SoftwareUpdateTimeoutSeconds: 60,
		},
	}
}

func merge(dst *Config, src Config) {
	if src.Pulse.IngestURL != "" {
		dst.Pulse.IngestURL = src.Pulse.IngestURL
	}
	if src.Pulse.IngestToken != "" {
		dst.Pulse.IngestToken = src.Pulse.IngestToken
	}
	if src.Entity.ID != "" {
		dst.Entity.ID = src.Entity.ID
	}
	if src.Entity.Type != "" {
		dst.Entity.Type = src.Entity.Type
	}
	if src.HTTP.TimeoutSeconds != 0 {
		dst.HTTP.TimeoutSeconds = src.HTTP.TimeoutSeconds
	}
	if src.HTTP.MaxRetries != 0 {
		dst.HTTP.MaxRetries = src.HTTP.MaxRetries
	}
	if src.HTTP.InitialBackoffMS != 0 {
		dst.HTTP.InitialBackoffMS = src.HTTP.InitialBackoffMS
	}
	if src.Runner.IntervalSeconds != 0 {
		dst.Runner.IntervalSeconds = src.Runner.IntervalSeconds
	}
	if src.Runner.CollectorTimeoutSeconds != 0 {
		dst.Runner.CollectorTimeoutSeconds = src.Runner.CollectorTimeoutSeconds
	}
	if src.Host.ProcRoot != "" {
		dst.Host.ProcRoot = src.Host.ProcRoot
	}
	if src.Host.SysRoot != "" {
		dst.Host.SysRoot = src.Host.SysRoot
	}
	if src.Host.Root != "" {
		dst.Host.Root = src.Host.Root
	}
	if src.Host.CPUSampleMS != 0 {
		dst.Host.CPUSampleMS = src.Host.CPUSampleMS
	}
	if src.Host.EnableThermal != nil {
		dst.Host.EnableThermal = src.Host.EnableThermal
	}
	if src.Host.EnableBtrfs != nil {
		dst.Host.EnableBtrfs = src.Host.EnableBtrfs
	}
	if src.Host.EnableZfs != nil {
		dst.Host.EnableZfs = src.Host.EnableZfs
	}
	if src.Host.EnableCeph != nil {
		dst.Host.EnableCeph = src.Host.EnableCeph
	}
	if src.Docker.SocketPath != "" {
		dst.Docker.SocketPath = src.Docker.SocketPath
	}
	if src.Docker.HostRoot != "" {
		dst.Docker.HostRoot = src.Docker.HostRoot
	}
	if src.Docker.Concurrency != 0 {
		dst.Docker.Concurrency = src.Docker.Concurrency
	}
	if src.Docker.ContainerTimeoutSeconds != 0 {
		dst.Docker.ContainerTimeoutSeconds = src.Docker.ContainerTimeoutSeconds
	}
	if src.Docker.EnableRegistryChecks {
		dst.Docker.EnableRegistryChecks = true
	}
	if src.Docker.RegistryTimeoutSeconds != 0 {
		dst.Docker.RegistryTimeoutSeconds = src.Docker.RegistryTimeoutSeconds
	}
	if len(src.Linux.SystemdUnits) > 0 {
		dst.Linux.SystemdUnits = src.Linux.SystemdUnits
	}
	if src.Linux.Profile != "" {
		dst.Linux.Profile = src.Linux.Profile
	}
	if src.Linux.PackageTimeoutSeconds != 0 {
		dst.Linux.PackageTimeoutSeconds = src.Linux.PackageTimeoutSeconds
	}
	if src.Linux.DiskHealthTimeoutSeconds != 0 {
		dst.Linux.DiskHealthTimeoutSeconds = src.Linux.DiskHealthTimeoutSeconds
	}
	if src.MacOS.EnableHomebrew != nil {
		dst.MacOS.EnableHomebrew = src.MacOS.EnableHomebrew
	}
	if src.MacOS.HomebrewTimeoutSeconds != 0 {
		dst.MacOS.HomebrewTimeoutSeconds = src.MacOS.HomebrewTimeoutSeconds
	}
	if src.MacOS.EnableSoftwareUpdate != nil {
		dst.MacOS.EnableSoftwareUpdate = src.MacOS.EnableSoftwareUpdate
	}
	if src.MacOS.SoftwareUpdateTimeoutSeconds != 0 {
		dst.MacOS.SoftwareUpdateTimeoutSeconds = src.MacOS.SoftwareUpdateTimeoutSeconds
	}
	if len(src.Scripts) > 0 {
		dst.Scripts = src.Scripts
	}
}

func applyOverlay(dst *Config, o Overlay) {
	if o.IngestURL != "" {
		dst.Pulse.IngestURL = o.IngestURL
	}
	if o.IngestToken != "" {
		dst.Pulse.IngestToken = o.IngestToken
	}
	if o.EntityID != "" {
		dst.Entity.ID = o.EntityID
	}
	if o.EntityType != "" {
		dst.Entity.Type = o.EntityType
	}
	if o.TimeoutSeconds != 0 {
		dst.HTTP.TimeoutSeconds = o.TimeoutSeconds
	}
	if o.MaxRetries != 0 {
		dst.HTTP.MaxRetries = o.MaxRetries
	}
	if o.InitialBackoffMS != 0 {
		dst.HTTP.InitialBackoffMS = o.InitialBackoffMS
	}
	if o.IntervalSeconds != 0 {
		dst.Runner.IntervalSeconds = o.IntervalSeconds
	}
	if o.CollectorTimeoutSeconds != 0 {
		dst.Runner.CollectorTimeoutSeconds = o.CollectorTimeoutSeconds
	}
	if o.ProcRoot != "" {
		dst.Host.ProcRoot = o.ProcRoot
	}
	if o.SysRoot != "" {
		dst.Host.SysRoot = o.SysRoot
	}
	if o.HostRoot != "" {
		dst.Host.Root = o.HostRoot
	}
	if o.CPUSampleMS != 0 {
		dst.Host.CPUSampleMS = o.CPUSampleMS
	}
	if o.DockerSocketPath != "" {
		dst.Docker.SocketPath = o.DockerSocketPath
	}
	if o.DockerHostRoot != "" {
		dst.Docker.HostRoot = o.DockerHostRoot
	}
	if o.DockerConcurrency != 0 {
		dst.Docker.Concurrency = o.DockerConcurrency
	}
	if o.DockerContainerTimeoutSeconds != 0 {
		dst.Docker.ContainerTimeoutSeconds = o.DockerContainerTimeoutSeconds
	}
	if o.DockerEnableRegistryChecks {
		dst.Docker.EnableRegistryChecks = true
	}
	if o.DockerRegistryTimeoutSeconds != 0 {
		dst.Docker.RegistryTimeoutSeconds = o.DockerRegistryTimeoutSeconds
	}
	if len(o.LinuxSystemdUnits) > 0 {
		dst.Linux.SystemdUnits = o.LinuxSystemdUnits
	}
	if o.LinuxProfile != "" {
		dst.Linux.Profile = o.LinuxProfile
	}
	if o.LinuxPackageTimeoutSeconds != 0 {
		dst.Linux.PackageTimeoutSeconds = o.LinuxPackageTimeoutSeconds
	}
	if o.LinuxDiskHealthTimeoutSeconds != 0 {
		dst.Linux.DiskHealthTimeoutSeconds = o.LinuxDiskHealthTimeoutSeconds
	}
	if o.HomebrewTimeoutSeconds != 0 {
		dst.MacOS.HomebrewTimeoutSeconds = o.HomebrewTimeoutSeconds
	}
	if o.SoftwareUpdateTimeoutSeconds != 0 {
		dst.MacOS.SoftwareUpdateTimeoutSeconds = o.SoftwareUpdateTimeoutSeconds
	}
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("pulse ingest url is invalid: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("pulse ingest url must use http or https")
	}
	if u.Host == "" {
		return errors.New("pulse ingest url must include a host")
	}
	return nil
}
