// Package config loads kai's TOML configuration file and resolves data paths.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds the full application configuration.
type Config struct {
	GitHubToken        string   `toml:"github_token"`
	AnthropicAPIKey    string   `toml:"anthropic_api_key"`
	Schedule           string   `toml:"schedule"`
	WatchRepos         []string `toml:"watch_repos"`
	GitHubPollInterval string   `toml:"github_poll_interval"`
	BriefingFeedback   bool     `toml:"briefing_feedback"`

	Trust  TrustConfig  `toml:"trust"`
	Limits LimitsConfig `toml:"limits"`
	Paths  PathsConfig  `toml:"paths"`

	// Resolved at load time — not from TOML.
	DataDir   string `toml:"-"`
	ConfigDir string `toml:"-"`
}

// TrustConfig controls which blast-radius levels require confirmation.
type TrustConfig struct {
	// StateChange: "confirm" (default) | "auto"
	StateChange string `toml:"state_change"`
}

// LimitsConfig sets resource limits.
type LimitsConfig struct {
	MaxTokensContext       int `toml:"max_tokens_context"`
	DailyTokenBudget       int `toml:"daily_token_budget"`
	GitHubRequestsPerHour  int `toml:"github_requests_per_hour"`
}

// PathsConfig allows overriding default data directory via config.
type PathsConfig struct {
	DataDir string `toml:"data_dir"`
}

// defaults applied when fields are empty.
func applyDefaults(c *Config) {
	if c.Schedule == "" {
		c.Schedule = "0 9 * * *"
	}
	if c.GitHubPollInterval == "" {
		c.GitHubPollInterval = "60s"
	}
	if c.Trust.StateChange == "" {
		c.Trust.StateChange = "confirm"
	}
	if c.Limits.MaxTokensContext == 0 {
		c.Limits.MaxTokensContext = 8000
	}
	if c.Limits.DailyTokenBudget == 0 {
		c.Limits.DailyTokenBudget = 100000
	}
	if c.Limits.GitHubRequestsPerHour == 0 {
		c.Limits.GitHubRequestsPerHour = 60
	}
}

// DefaultConfigDir returns ~/.config/kai.
func DefaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "kai"), nil
}

// DefaultDataDir returns ~/.local/share/kai (or KAI_DATA_DIR env override).
func DefaultDataDir() (string, error) {
	if override := os.Getenv("KAI_DATA_DIR"); override != "" {
		return override, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".local", "share", "kai"), nil
}

// ConfigPath returns the path to the config file.
func ConfigPath() (string, error) {
	dir, err := DefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// loadFromPath decodes a TOML config file into cfg. Exported for testing.
func loadFromPath(path string, cfg *Config) (toml.MetaData, error) {
	meta, err := toml.DecodeFile(path, cfg)
	if err != nil {
		if os.IsNotExist(err) {
			return meta, fmt.Errorf("config not found: run `kai init` to configure")
		}
		return meta, fmt.Errorf("loading config: %w", err)
	}
	return meta, nil
}

// Load reads the config file and resolves all paths.
// Returns a clear error if the file doesn't exist (prompts user to run kai init).
func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	var cfg Config
	if _, err := loadFromPath(path, &cfg); err != nil {
		return nil, err
	}

	applyDefaults(&cfg)

	// Resolve data dir: env > config > default.
	dataDir, err := resolveDataDir(&cfg)
	if err != nil {
		return nil, err
	}
	cfg.DataDir = dataDir

	configDir, err := DefaultConfigDir()
	if err != nil {
		return nil, err
	}
	cfg.ConfigDir = configDir

	return &cfg, nil
}

// resolveDataDir picks the data directory in priority order:
// 1. KAI_DATA_DIR env var
// 2. [paths].data_dir in config.toml
// 3. ~/.local/share/kai
func resolveDataDir(cfg *Config) (string, error) {
	if v := os.Getenv("KAI_DATA_DIR"); v != "" {
		return v, nil
	}
	if cfg.Paths.DataDir != "" {
		return cfg.Paths.DataDir, nil
	}
	return DefaultDataDir()
}

// EnsureDataDirs creates required subdirectories under DataDir.
func EnsureDataDirs(cfg *Config) error {
	dirs := []string{
		cfg.DataDir,
		filepath.Join(cfg.DataDir, "briefings"),
		filepath.Join(cfg.DataDir, "pending"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return fmt.Errorf("creating directory %s: %w", d, err)
		}
	}
	return nil
}
