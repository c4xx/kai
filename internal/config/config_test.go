package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingConfig(t *testing.T) {
	t.Setenv("KAI_DATA_DIR", t.TempDir())
	// Point config dir to a temp dir where no config.toml exists.
	// We can't easily override config path without refactor, so test DefaultDataDir.
	dir := t.TempDir()
	t.Setenv("KAI_DATA_DIR", dir)

	dd, err := DefaultDataDir()
	if err != nil {
		t.Fatal(err)
	}
	if dd != dir {
		t.Fatalf("expected %s, got %s", dir, dd)
	}
}

func TestApplyDefaults(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)

	if cfg.Schedule != "0 9 * * *" {
		t.Errorf("unexpected schedule: %s", cfg.Schedule)
	}
	if cfg.Trust.StateChange != "confirm" {
		t.Errorf("unexpected trust.state_change: %s", cfg.Trust.StateChange)
	}
	if cfg.Limits.DailyTokenBudget != 100000 {
		t.Errorf("unexpected daily_token_budget: %d", cfg.Limits.DailyTokenBudget)
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KAI_DATA_DIR", dir)

	configDir := filepath.Join(dir, "config")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}

	configContent := `
github_token = "ghp_test"
anthropic_api_key = "sk-ant-test"
schedule = "0 8 * * *"
watch_repos = ["owner/repo"]

[trust]
state_change = "auto"

[limits]
daily_token_budget = 50000
`
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	var cfg Config
	if _, err := loadFromPath(configPath, &cfg); err != nil {
		t.Fatal(err)
	}
	applyDefaults(&cfg)

	if cfg.GitHubToken != "ghp_test" {
		t.Errorf("unexpected github_token: %s", cfg.GitHubToken)
	}
	if cfg.Trust.StateChange != "auto" {
		t.Errorf("unexpected trust.state_change: %s", cfg.Trust.StateChange)
	}
	if cfg.Limits.DailyTokenBudget != 50000 {
		t.Errorf("unexpected daily_token_budget: %d", cfg.Limits.DailyTokenBudget)
	}
	// Unset field should get default.
	if cfg.Limits.MaxTokensContext != 8000 {
		t.Errorf("unexpected max_tokens_context: %d", cfg.Limits.MaxTokensContext)
	}
}

func TestEnsureDataDirs(t *testing.T) {
	dir := t.TempDir()
	cfg := &Config{DataDir: dir}
	if err := EnsureDataDirs(cfg); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"briefings", "pending"} {
		path := filepath.Join(dir, sub)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected directory %s to exist: %v", path, err)
		}
	}
}
