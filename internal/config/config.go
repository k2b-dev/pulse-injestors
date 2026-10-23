package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// DefaultConfigPath is where an ingestor looks for its config without --config,
// for example /etc/pulse/pulse-linux.toml. Each ingestor has its own file so
// several can run on one host.
func DefaultConfigPath(binary string) string {
	return "/etc/pulse/" + binary + ".toml"
}

type Config struct {
	Pulse      PulseConfig       `toml:"pulse"`
	Resource   ResourceConfig    `toml:"resource"`
	Dimensions map[string]string `toml:"dimensions"`
	HTTP       HTTPConfig        `toml:"http"`
	Runner     RunnerConfig      `toml:"runner"`
	Host       HostConfig        `toml:"host"`
	Docker     DockerConfig      `toml:"docker"`
	Linux      LinuxConfig       `toml:"linux"`
	MacOS      MacOSConfig       `toml:"macos"`
	Proxmox    ProxmoxConfig     `toml:"proxmox"`
	PBS        PBSConfig         `toml:"pbs"`
	Uptime     UptimeConfig      `toml:"uptime"`
	Backups    []BackupConfig    `toml:"backup"`
	Scripts    []ScriptConfig    `toml:"script"`
}

type PulseConfig struct {
	SpoolDir      string `toml:"spool_dir"`
	SpoolMaxBytes int64  `toml:"spool_max_bytes"`
	IngestURL     string `toml:"ingest_url"`
	IngestToken   string `toml:"ingest_token"`
	// IngestTokenFile names a private file holding the token.
	IngestTokenFile string `toml:"ingest_token_file"`
}

type ResourceConfig struct {
	ID    string `toml:"id"`
	Label string `toml:"label"`
	Type  string `toml:"type"`
}

type HTTPConfig struct {
	TimeoutSeconds   int `toml:"timeout_seconds"`
	MaxRetries       int `toml:"max_retries"`
	InitialBackoffMS int `toml:"initial_backoff_ms"`
}

type RunnerConfig struct {
	Cadence                 map[string]int `toml:"cadence"`
	IntervalSeconds         int            `toml:"interval_seconds"`
	CollectorTimeoutSeconds int            `toml:"collector_timeout_seconds"`
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
	NetworkExclude           []string `toml:"network_exclude"`
	CertificatePaths         []string `toml:"certificate_paths"`
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
	EnableSystemProfiler         *bool `toml:"enable_system_profiler"`
	SystemProfilerTimeoutSeconds int   `toml:"system_profiler_timeout_seconds"`
}

type ProxmoxConfig struct {
	ListTimeoutSeconds int    `toml:"list_timeout_seconds"`
	PveshPath          string `toml:"pvesh_path"`
	TimeoutSeconds     int    `toml:"timeout_seconds"`
	EnableCephAPI      *bool  `toml:"enable_ceph_api"`
	EnableLocalCeph    *bool  `toml:"enable_local_ceph"`
}

type PBSConfig struct {
	ListTimeoutSeconds int    `toml:"list_timeout_seconds"`
	CommandPath        string `toml:"command_path"`
	TimeoutSeconds     int    `toml:"timeout_seconds"`
}

type UptimeConfig struct {
	EnableDefaults *bool                `toml:"enable_defaults"`
	Concurrency    int                  `toml:"concurrency"`
	TimeoutSeconds int                  `toml:"timeout_seconds"`
	Targets        []UptimeTargetConfig `toml:"target"`
}

type UptimeTargetConfig struct {
	ID             string `toml:"id"`
	Label          string `toml:"label"`
	Kind           string `toml:"kind"`
	Address        string `toml:"address"`
	ExpectedStatus int    `toml:"expected_status"`
	TimeoutSeconds int    `toml:"timeout_seconds"`
}

type BackupConfig struct {
	Name       string   `toml:"name"`
	MarkerFile string   `toml:"marker_file"`
	Command    []string `toml:"command"`
}

type ScriptConfig struct {
	CadenceSeconds int               `toml:"cadence_seconds"`
	Name           string            `toml:"name"`
	Command        []string          `toml:"command"`
	TimeoutSeconds int               `toml:"timeout_seconds"`
	MaxOutputBytes int64             `toml:"max_output_bytes"`
	Dimensions     map[string]string `toml:"dimensions"`
}

type Overlay struct {
	SpoolDir                      string
	SpoolMaxBytes                 int64
	ConfigPath                    string
	IngestURL                     string
	IngestToken                   string
	IngestTokenFile               string
	ResourceID                    string
	ResourceLabel                 string
	ResourceType                  string
	Dimensions                    map[string]string
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
	DisableSystemProfiler         bool
	SystemProfilerTimeoutSeconds  int
	ProxmoxPveshPath              string
	ProxmoxTimeoutSeconds         int
	ProxmoxEnableCephAPI          *bool
	ProxmoxEnableLocalCeph        *bool
	PBSCommandPath                string
	PBSTimeoutSeconds             int
	UptimeDisableDefaults         bool
	UptimeConcurrency             int
	UptimeTimeoutSeconds          int
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
	metadata, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, err
	}
	if len(metadata.Undecoded()) > 0 {
		return cfg, errors.New("configuration contains unknown fields")
	}
	if metadata.IsDefined("proxmox", "list_timeout_seconds") && cfg.Proxmox.ListTimeoutSeconds <= 0 {
		return cfg, errors.New("proxmox list_timeout_seconds must be positive")
	}
	if metadata.IsDefined("pbs", "list_timeout_seconds") && cfg.PBS.ListTimeoutSeconds <= 0 {
		return cfg, errors.New("pbs list_timeout_seconds must be positive")
	}
	return cfg, nil
}

func Resolve(file Config, overlay Overlay) (Config, error) {
	// Check before merging: even an empty reserved value is invalid.
	for _, dimensions := range []map[string]string{file.Dimensions, overlay.Dimensions} {
		if _, ok := dimensions["host"]; ok {
			return file, errors.New("dimension 'host' is reserved; the resource id identifies the host")
		}
	}
	if file.Pulse.IngestToken != "" && file.Pulse.IngestTokenFile != "" {
		return file, errors.New("set either pulse ingest_token or ingest_token_file, not both")
	}
	if overlay.IngestToken != "" && overlay.IngestTokenFile != "" {
		return file, errors.New("set either PULSE_INGEST_TOKEN or the ingest token file, not both")
	}
	cfg := defaults()
	merge(&cfg, file)
	applyOverlay(&cfg, overlay)

	host, hostErr := os.Hostname()
	if cfg.Resource.ID == "" {
		if hostErr != nil || host == "" {
			return cfg, errors.New("resource id is required when hostname cannot be resolved")
		}
		cfg.Resource.ID = host
	}
	if cfg.Resource.Label == "" {
		if hostErr == nil && host != "" {
			cfg.Resource.Label = host
		} else {
			cfg.Resource.Label = cfg.Resource.ID
		}
	}
	if err := validateDimensions(cfg.Dimensions); err != nil {
		return cfg, err
	}
	if !overlay.AllowMissingPulse {
		if err := resolveToken(&cfg.Pulse); err != nil {
			return cfg, err
		}
		if cfg.Pulse.IngestURL == "" {
			return cfg, errors.New("pulse ingest url is required")
		}
		if cfg.Pulse.IngestToken == "" {
			return cfg, errors.New("pulse ingest token is required: set ingest_token_file, PULSE_INGEST_TOKEN_FILE or PULSE_INGEST_TOKEN")
		}
		if err := validateHTTPURL(cfg.Pulse.IngestURL); err != nil {
			return cfg, err
		}
	} else if cfg.Pulse.IngestURL != "" {
		if err := validateHTTPURL(cfg.Pulse.IngestURL); err != nil {
			return cfg, err
		}
	}
	if cfg.Pulse.SpoolDir != "" && cfg.Pulse.SpoolMaxBytes <= 0 {
		return cfg, errors.New("pulse spool_max_bytes must be positive when spool_dir is configured")
	}
	if cfg.Pulse.SpoolMaxBytes < 0 {
		return cfg, errors.New("pulse spool_max_bytes must not be negative")
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
	if cfg.MacOS.SystemProfilerTimeoutSeconds <= 0 {
		return cfg, errors.New("macos system_profiler_timeout_seconds must be positive")
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
	if cfg.Proxmox.ListTimeoutSeconds <= 0 {
		return cfg, errors.New("proxmox list_timeout_seconds must be positive")
	}
	if cfg.Proxmox.TimeoutSeconds <= 0 {
		return cfg, errors.New("proxmox timeout_seconds must be positive")
	}
	if cfg.PBS.ListTimeoutSeconds <= 0 {
		return cfg, errors.New("pbs list_timeout_seconds must be positive")
	}
	if cfg.PBS.TimeoutSeconds <= 0 {
		return cfg, errors.New("pbs timeout_seconds must be positive")
	}
	if cfg.Uptime.Concurrency <= 0 {
		return cfg, errors.New("uptime concurrency must be positive")
	}
	if cfg.Uptime.TimeoutSeconds <= 0 {
		return cfg, errors.New("uptime timeout_seconds must be positive")
	}
	if err := validateUptimeTargets(cfg.Uptime.Targets); err != nil {
		return cfg, err
	}
	if cfg.Linux.PackageTimeoutSeconds <= 0 {
		return cfg, errors.New("linux package_timeout_seconds must be positive")
	}
	if cfg.Linux.DiskHealthTimeoutSeconds <= 0 {
		return cfg, errors.New("linux disk_health_timeout_seconds must be positive")
	}
	for name, seconds := range cfg.Runner.Cadence {
		if strings.TrimSpace(name) == "" || seconds < cfg.Runner.IntervalSeconds || int64(seconds) > int64((1<<63-1)/time.Second) {
			return cfg, fmt.Errorf("runner cadence %q must be >= interval_seconds and fit a duration", name)
		}
	}
	for _, pattern := range append(append([]string{}, cfg.Linux.NetworkExclude...), cfg.Linux.CertificatePaths...) {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return cfg, fmt.Errorf("invalid glob %q: %w", pattern, err)
		}
	}
	backupNames := map[string]bool{}
	for _, backup := range cfg.Backups {
		if strings.TrimSpace(backup.Name) == "" || backupNames[backup.Name] {
			return cfg, errors.New("backup names must be nonempty and unique")
		}
		backupNames[backup.Name] = true
		if (backup.MarkerFile == "") == (len(backup.Command) == 0) || (len(backup.Command) > 0 && strings.TrimSpace(backup.Command[0]) == "") {
			return cfg, fmt.Errorf("backup %q requires exactly one marker_file or command", backup.Name)
		}
	}
	scriptNames := map[string]bool{}
	for _, script := range cfg.Scripts {
		if scriptNames[script.Name] {
			return cfg, fmt.Errorf("duplicate script %q", script.Name)
		}
		scriptNames[script.Name] = true
		if script.CadenceSeconds != 0 && (script.CadenceSeconds < cfg.Runner.IntervalSeconds || int64(script.CadenceSeconds) > int64((1<<63-1)/time.Second)) {
			return cfg, fmt.Errorf("script %q cadence_seconds must be >= interval_seconds and fit a duration", script.Name)
		}
		if script.Name == "" {
			return cfg, errors.New("script name is required")
		}
		if len(script.Command) == 0 {
			return cfg, fmt.Errorf("script %q command is required", script.Name)
		}
	}
	return cfg, nil
}

// CredentialName is the systemd credential that holds the ingest token
// ($CREDENTIALS_DIRECTORY/pulse-ingest-token, set with LoadCredential=).
const CredentialName = "pulse-ingest-token"

func resolveToken(p *PulseConfig) error {
	if p.IngestToken == "" && p.IngestTokenFile == "" {
		if dir := os.Getenv("CREDENTIALS_DIRECTORY"); dir != "" {
			if _, err := os.Stat(dir + "/" + CredentialName); err == nil {
				p.IngestTokenFile = dir + "/" + CredentialName
			}
		}
	}
	if p.IngestTokenFile == "" {
		return nil
	}
	token, err := ReadTokenFile(p.IngestTokenFile)
	if err != nil {
		return err
	}
	p.IngestToken = token
	return nil
}

// ReadTokenFile reads a token from a file other users cannot access.
func ReadTokenFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("ingest token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("ingest token file must be a regular file")
	}
	if info.Mode().Perm()&0007 != 0 {
		return "", errors.New("ingest token file must not be accessible by other users (chmod 600 or 640)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("ingest token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", errors.New("ingest token file must contain exactly one token")
	}
	return token, nil
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

func (c Config) ProxmoxCephAPIEnabled() bool {
	return c.Proxmox.EnableCephAPI == nil || *c.Proxmox.EnableCephAPI
}

func (c Config) ProxmoxLocalCephEnabled() bool {
	return c.Proxmox.EnableLocalCeph == nil || *c.Proxmox.EnableLocalCeph
}

func (c Config) HomebrewEnabled() bool {
	return c.MacOS.EnableHomebrew == nil || *c.MacOS.EnableHomebrew
}

func (c Config) SoftwareUpdateEnabled() bool {
	return c.MacOS.EnableSoftwareUpdate != nil && *c.MacOS.EnableSoftwareUpdate
}

func (c Config) SystemProfilerEnabled() bool {
	return c.MacOS.EnableSystemProfiler == nil || *c.MacOS.EnableSystemProfiler
}

func (c Config) HomebrewTimeout() time.Duration {
	return time.Duration(c.MacOS.HomebrewTimeoutSeconds) * time.Second
}

func (c Config) SoftwareUpdateTimeout() time.Duration {
	return time.Duration(c.MacOS.SoftwareUpdateTimeoutSeconds) * time.Second
}

func (c Config) SystemProfilerTimeout() time.Duration {
	return time.Duration(c.MacOS.SystemProfilerTimeoutSeconds) * time.Second
}

func (c Config) ProxmoxTimeout() time.Duration {
	return time.Duration(c.Proxmox.TimeoutSeconds) * time.Second
}

func (c Config) PBSTimeout() time.Duration {
	return time.Duration(c.PBS.TimeoutSeconds) * time.Second
}

func (c Config) UptimeDefaultsEnabled() bool {
	return c.Uptime.EnableDefaults == nil || *c.Uptime.EnableDefaults
}

func (c Config) UptimeTimeout() time.Duration {
	return time.Duration(c.Uptime.TimeoutSeconds) * time.Second
}

func defaults() Config {
	return Config{
		Resource: ResourceConfig{
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
			SystemProfilerTimeoutSeconds: 10,
		},
		Proxmox: ProxmoxConfig{
			ListTimeoutSeconds: 60,
			PveshPath:          "pvesh",
			TimeoutSeconds:     10,
		},
		PBS: PBSConfig{
			ListTimeoutSeconds: 60,
			CommandPath:        "proxmox-backup-debug",
			TimeoutSeconds:     10,
		},
		Uptime: UptimeConfig{
			Concurrency:    4,
			TimeoutSeconds: 5,
		},
	}
}

func merge(dst *Config, src Config) {
	dst.Runner.Cadence = src.Runner.Cadence
	dst.Linux.NetworkExclude = src.Linux.NetworkExclude
	dst.Linux.CertificatePaths = src.Linux.CertificatePaths
	dst.Backups = src.Backups
	if src.Pulse.SpoolDir != "" {
		dst.Pulse.SpoolDir = src.Pulse.SpoolDir
	}
	if src.Pulse.SpoolMaxBytes != 0 {
		dst.Pulse.SpoolMaxBytes = src.Pulse.SpoolMaxBytes
	}
	if src.Pulse.IngestURL != "" {
		dst.Pulse.IngestURL = src.Pulse.IngestURL
	}
	if src.Pulse.IngestToken != "" {
		dst.Pulse.IngestToken = src.Pulse.IngestToken
	}
	if src.Pulse.IngestTokenFile != "" {
		dst.Pulse.IngestTokenFile = src.Pulse.IngestTokenFile
	}
	if src.Resource.ID != "" {
		dst.Resource.ID = src.Resource.ID
	}
	if src.Resource.Label != "" {
		dst.Resource.Label = src.Resource.Label
	}
	if src.Resource.Type != "" {
		dst.Resource.Type = src.Resource.Type
	}
	dst.Dimensions = mergeDimensions(dst.Dimensions, src.Dimensions)
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
	if src.MacOS.EnableSystemProfiler != nil {
		dst.MacOS.EnableSystemProfiler = src.MacOS.EnableSystemProfiler
	}
	if src.MacOS.SystemProfilerTimeoutSeconds != 0 {
		dst.MacOS.SystemProfilerTimeoutSeconds = src.MacOS.SystemProfilerTimeoutSeconds
	}
	if src.Proxmox.PveshPath != "" {
		dst.Proxmox.PveshPath = src.Proxmox.PveshPath
	}
	if src.Proxmox.ListTimeoutSeconds != 0 {
		dst.Proxmox.ListTimeoutSeconds = src.Proxmox.ListTimeoutSeconds
	}
	if src.Proxmox.TimeoutSeconds != 0 {
		dst.Proxmox.TimeoutSeconds = src.Proxmox.TimeoutSeconds
	}
	if src.Proxmox.EnableCephAPI != nil {
		dst.Proxmox.EnableCephAPI = src.Proxmox.EnableCephAPI
	}
	if src.Proxmox.EnableLocalCeph != nil {
		dst.Proxmox.EnableLocalCeph = src.Proxmox.EnableLocalCeph
	}
	if src.PBS.CommandPath != "" {
		dst.PBS.CommandPath = src.PBS.CommandPath
	}
	if src.PBS.ListTimeoutSeconds != 0 {
		dst.PBS.ListTimeoutSeconds = src.PBS.ListTimeoutSeconds
	}
	if src.PBS.TimeoutSeconds != 0 {
		dst.PBS.TimeoutSeconds = src.PBS.TimeoutSeconds
	}
	if src.Uptime.EnableDefaults != nil {
		dst.Uptime.EnableDefaults = src.Uptime.EnableDefaults
	}
	if src.Uptime.Concurrency != 0 {
		dst.Uptime.Concurrency = src.Uptime.Concurrency
	}
	if src.Uptime.TimeoutSeconds != 0 {
		dst.Uptime.TimeoutSeconds = src.Uptime.TimeoutSeconds
	}
	if len(src.Uptime.Targets) > 0 {
		dst.Uptime.Targets = src.Uptime.Targets
	}
	if len(src.Scripts) > 0 {
		dst.Scripts = src.Scripts
	}
}

func applyOverlay(dst *Config, o Overlay) {
	if o.SpoolDir != "" {
		dst.Pulse.SpoolDir = o.SpoolDir
	}
	if o.SpoolMaxBytes != 0 {
		dst.Pulse.SpoolMaxBytes = o.SpoolMaxBytes
	}
	if o.IngestURL != "" {
		dst.Pulse.IngestURL = o.IngestURL
	}
	// A token source from the environment or CLI replaces both file settings.
	if o.IngestToken != "" {
		dst.Pulse.IngestToken, dst.Pulse.IngestTokenFile = o.IngestToken, ""
	}
	if o.IngestTokenFile != "" {
		dst.Pulse.IngestToken, dst.Pulse.IngestTokenFile = "", o.IngestTokenFile
	}
	if o.ResourceID != "" {
		dst.Resource.ID = o.ResourceID
	}
	if o.ResourceLabel != "" {
		dst.Resource.Label = o.ResourceLabel
	}
	if o.ResourceType != "" {
		dst.Resource.Type = o.ResourceType
	}
	dst.Dimensions = mergeDimensions(dst.Dimensions, o.Dimensions)
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
	if o.DisableSystemProfiler {
		enabled := false
		dst.MacOS.EnableSystemProfiler = &enabled
	}
	if o.SystemProfilerTimeoutSeconds != 0 {
		dst.MacOS.SystemProfilerTimeoutSeconds = o.SystemProfilerTimeoutSeconds
	}
	if o.ProxmoxPveshPath != "" {
		dst.Proxmox.PveshPath = o.ProxmoxPveshPath
	}
	if o.ProxmoxTimeoutSeconds != 0 {
		dst.Proxmox.TimeoutSeconds = o.ProxmoxTimeoutSeconds
	}
	if o.ProxmoxEnableCephAPI != nil {
		dst.Proxmox.EnableCephAPI = o.ProxmoxEnableCephAPI
	}
	if o.ProxmoxEnableLocalCeph != nil {
		dst.Proxmox.EnableLocalCeph = o.ProxmoxEnableLocalCeph
	}
	if o.PBSCommandPath != "" {
		dst.PBS.CommandPath = o.PBSCommandPath
	}
	if o.PBSTimeoutSeconds != 0 {
		dst.PBS.TimeoutSeconds = o.PBSTimeoutSeconds
	}
	if o.UptimeDisableDefaults {
		enabled := false
		dst.Uptime.EnableDefaults = &enabled
	}
	if o.UptimeConcurrency != 0 {
		dst.Uptime.Concurrency = o.UptimeConcurrency
	}
	if o.UptimeTimeoutSeconds != 0 {
		dst.Uptime.TimeoutSeconds = o.UptimeTimeoutSeconds
	}
}

func validateUptimeTargets(targets []UptimeTargetConfig) error {
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if !validUptimeTargetID(target.ID) {
			return fmt.Errorf("uptime target id %q must contain only letters, digits, dot, underscore, or hyphen", target.ID)
		}
		if seen[target.ID] {
			return fmt.Errorf("uptime target id %q is duplicated", target.ID)
		}
		seen[target.ID] = true
		if strings.TrimSpace(target.Label) == "" {
			return fmt.Errorf("uptime target %q label is required", target.ID)
		}
		if strings.TrimSpace(target.Address) == "" {
			return fmt.Errorf("uptime target %q address is required", target.ID)
		}
		switch strings.ToLower(strings.TrimSpace(target.Kind)) {
		case "icmp":
			if target.ExpectedStatus != 0 {
				return fmt.Errorf("uptime target %q expected_status is only valid for http checks", target.ID)
			}
			if net.ParseIP(target.Address) == nil {
				return fmt.Errorf("uptime target %q ICMP address must be an IP address", target.ID)
			}
		case "dns":
			if target.ExpectedStatus != 0 {
				return fmt.Errorf("uptime target %q expected_status is only valid for http checks", target.ID)
			}
			if strings.ContainsAny(target.Address, "/: \t\r\n") {
				return fmt.Errorf("uptime target %q DNS address must be a hostname", target.ID)
			}
		case "tcp":
			if target.ExpectedStatus != 0 {
				return fmt.Errorf("uptime target %q expected_status is only valid for http checks", target.ID)
			}
			host, port, err := net.SplitHostPort(target.Address)
			portNumber, portErr := strconv.Atoi(port)
			if err != nil || host == "" || portErr != nil || portNumber < 1 || portNumber > 65535 {
				return fmt.Errorf("uptime target %q TCP address must use host:port", target.ID)
			}
		case "http":
			u, err := url.Parse(target.Address)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("uptime target %q address must be an http or https URL", target.ID)
			}
			if u.User != nil {
				return fmt.Errorf("uptime target %q URL must not contain credentials", target.ID)
			}
			if target.ExpectedStatus != 0 && (target.ExpectedStatus < 100 || target.ExpectedStatus > 599) {
				return fmt.Errorf("uptime target %q expected_status must be between 100 and 599", target.ID)
			}
		default:
			return fmt.Errorf("uptime target %q kind must be icmp, dns, tcp, or http", target.ID)
		}
		if target.TimeoutSeconds < 0 {
			return fmt.Errorf("uptime target %q timeout_seconds must not be negative", target.ID)
		}
	}
	return nil
}

func validUptimeTargetID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func mergeDimensions(base, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for key, value := range base {
		if value != "" {
			out[key] = value
		}
	}
	for key, value := range overlay {
		if value != "" {
			out[key] = value
		}
	}
	return out
}

func validateDimensions(dimensions map[string]string) error {
	if len(dimensions) > 32 {
		return errors.New("dimensions cannot exceed 32 keys")
	}
	for key, value := range dimensions {
		if key == "host" {
			return errors.New("dimension 'host' is reserved; the resource id identifies the host")
		}
		if key == "" || len(key) > 80 {
			return fmt.Errorf("invalid dimension key %q", key)
		}
		if len(value) > 500 {
			return fmt.Errorf("dimension %q exceeds 500 characters", key)
		}
	}
	return nil
}

func ParseDimensions(values []string) (map[string]string, error) {
	dimensions := map[string]string{}
	for _, value := range values {
		key, dimensionValue, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		dimensionValue = strings.TrimSpace(dimensionValue)
		if !ok || key == "" || dimensionValue == "" {
			return nil, fmt.Errorf("dimension %q must use key=value", value)
		}
		dimensions[key] = dimensionValue
	}
	if err := validateDimensions(dimensions); err != nil {
		return nil, err
	}
	if len(dimensions) == 0 {
		return nil, nil
	}
	return dimensions, nil
}

func validateHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("pulse ingest url is invalid")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("pulse ingest url must use http or https")
	}
	if u.Host == "" {
		return errors.New("pulse ingest url must include a host")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("pulse ingest url must not contain credentials, query or fragment")
	}
	if u.EscapedPath() != "/api/pulse/ingest" {
		return errors.New("pulse ingest url path must be /api/pulse/ingest")
	}
	return nil
}
