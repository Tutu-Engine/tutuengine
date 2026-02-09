// Package daemon manages the TuTu daemon lifecycle and configuration.
package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/BurntSushi/toml"
)

// Config holds all daemon configuration.
type Config struct {
	Node      NodeConfig      `toml:"node"`
	API       APIConfig       `toml:"api"`
	Models    ModelsConfig    `toml:"models"`
	Inference InferenceConfig `toml:"inference"`
	Logging   LoggingConfig   `toml:"logging"`
	Network   NetworkConfig   `toml:"network"`
	Resources ResourcesConfig `toml:"resources"`
	Security  SecurityConfig  `toml:"security"`
	Telemetry TelemetryConfig `toml:"telemetry"`
}

// NodeConfig identifies this node.
type NodeConfig struct {
	ID     string `toml:"id"`
	Region string `toml:"region"`
}

// APIConfig controls the HTTP API server.
type APIConfig struct {
	Host          string   `toml:"host"`
	Port          int      `toml:"port"`
	CORSOrigins   []string `toml:"cors_origins"`
	MaxConcurrent int      `toml:"max_concurrent"`
}

// ModelsConfig controls model storage.
type ModelsConfig struct {
	Dir        string `toml:"dir"`
	MaxStorage string `toml:"max_storage"`
	Default    string `toml:"default"`
	AutoPull   bool   `toml:"auto_pull"`
}

// InferenceConfig controls the inference engine.
type InferenceConfig struct {
	GPULayers     int `toml:"gpu_layers"`
	ContextLength int `toml:"context_length"`
	BatchSize     int `toml:"batch_size"`
	Threads       int `toml:"threads"`
}

// LoggingConfig controls logging behavior.
type LoggingConfig struct {
	Level     string `toml:"level"`
	File      string `toml:"file"`
	MaxSizeMB int    `toml:"max_size_mb"`
	MaxFiles  int    `toml:"max_files"`
}

// NetworkConfig controls distributed network participation (Phase 1).
type NetworkConfig struct {
	Enabled           bool   `toml:"enabled"`
	CloudCore         string `toml:"cloud_core"`
	HeartbeatInterval string `toml:"heartbeat_interval"`
}

// ResourcesConfig controls the resource governor (Phase 1).
type ResourcesConfig struct {
	MaxCPUPercent   int  `toml:"max_cpu_percent"`
	MaxMemoryPercent int `toml:"max_memory_percent"`
	ThermalThrottle int  `toml:"thermal_throttle"`
	ThermalShutdown int  `toml:"thermal_shutdown"`
	IdleDetection   bool `toml:"idle_detection"`
}

// SecurityConfig controls security features (Phase 1).
type SecurityConfig struct {
	Sandbox        string `toml:"sandbox"`
	RequireSigning bool   `toml:"require_signing"`
	TLS            bool   `toml:"tls"`
}

// TelemetryConfig controls observability (Phase 1).
type TelemetryConfig struct {
	Enabled        bool `toml:"enabled"`
	Prometheus     bool `toml:"prometheus"`
	PrometheusPort int  `toml:"prometheus_port"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() Config {
	homeDir := tutuHome()
	return Config{
		Node: NodeConfig{
			Region: "auto",
		},
		API: APIConfig{
			Host:          "127.0.0.1",
			Port:          11434,
			CORSOrigins:   []string{"*"},
			MaxConcurrent: 4,
		},
		Models: ModelsConfig{
			Dir:        filepath.Join(homeDir, "models"),
			MaxStorage: "50GB",
			Default:    "llama3.2",
			AutoPull:   true,
		},
		Inference: InferenceConfig{
			GPULayers:     -1,   // auto
			ContextLength: 4096,
			BatchSize:     512,
			Threads:       0, // auto = runtime.NumCPU() - 2
		},
		Logging: LoggingConfig{
			Level:     "info",
			File:      filepath.Join(homeDir, "tutu.log"),
			MaxSizeMB: 50,
			MaxFiles:  5,
		},
		Network: NetworkConfig{
			Enabled:           false, // Off by default — opt-in
			CloudCore:         "https://api.tutu.network",
			HeartbeatInterval: "10s",
		},
		Resources: ResourcesConfig{
			MaxCPUPercent:    80,
			MaxMemoryPercent: 70,
			ThermalThrottle:  80,
			ThermalShutdown:  95,
			IdleDetection:    true,
		},
		Security: SecurityConfig{
			Sandbox:        "process", // "gvisor" when available
			RequireSigning: true,
			TLS:            true,
		},
		Telemetry: TelemetryConfig{
			Enabled:        true,
			Prometheus:     false, // Opt-in: expose /metrics
			PrometheusPort: 9090,
		},
	}
}

// LoadConfig reads config from ~/.tutu/config.toml, falling back to defaults.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	path := filepath.Join(tutuHome(), "config.toml")

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return cfg, nil // No config file yet — use defaults
	}

	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config: %w", err)
	}

	// Apply auto-detection
	if cfg.Inference.Threads == 0 {
		cfg.Inference.Threads = max(1, runtime.NumCPU()-2)
	}

	return cfg, nil
}

// SaveConfig writes the config to ~/.tutu/config.toml.
func SaveConfig(cfg Config) error {
	path := filepath.Join(tutuHome(), "config.toml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(cfg)
}

// tutuHome returns the TuTu data directory.
func tutuHome() string {
	if env := os.Getenv("TUTU_HOME"); env != "" {
		return env
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".tutu")
}

// TutuHome is exported for use by other packages.
func TutuHome() string {
	return tutuHome()
}
