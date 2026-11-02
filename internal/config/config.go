// Package config manages infrapal-token-advisor's own persisted settings, distinct from the
// Claude Code settings.json that init patches. It lives at
// ~/.infrapal-token-advisor/config.json and holds the handful of choices that change
// infrapal-token-advisor's own behavior (ports, raw-body capture).
package config

import (
	"encoding/json"
	"os"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

const (
	// DefaultRetentionMonths is how long captured data is kept.
	DefaultRetentionMonths = 6
	// DefaultOTLPPort is where the OTLP http/protobuf receiver listens.
	DefaultOTLPPort = 4318
	// DefaultAPIPort is where the dashboard's JSON API + embedded UI listens.
	DefaultAPIPort = 4319
)

// Config is infrapal-token-advisor's own settings, persisted as JSON.
type Config struct {
	OTLPPort int `json:"otlp_port"`
	APIPort  int `json:"api_port"`

	// RawBodies enables OTEL_LOG_RAW_API_BODIES in file: mode. Never set to
	// inline mode: inline truncates at the content limit and bloats the DB.
	RawBodies bool `json:"raw_bodies"`

	// Agents holds each connected coding agent, by ID. uninstall uses it to
	// know what to disconnect and restore.
	Agents map[string]AgentState `json:"agents,omitempty"`

	// AIProvider is the model service AI Mode uses, by ID. Empty means the
	// default (see internal/ai).
	AIProvider string `json:"ai_provider,omitempty"`

	// AIKeys holds the user's own API keys by provider, pasted into the AI
	// Mode settings panel. Used only in direct response to the "Generate"
	// button (internal/ai, internal/api/ai.go) — infrapal-token-advisor's one deliberate
	// network-egress exception; see ResolveAIKey.
	AIKeys map[string]string `json:"ai_keys,omitempty"`

	// RetentionMonths is how long captured data is kept before the collector
	// deletes it. 0 keeps everything.
	RetentionMonths int `json:"retention_months"`

	// LogLevel is the collector's log level: debug, info (the default),
	// warn or error.
	LogLevel string `json:"log_level,omitempty"`
}

// AgentState is what's remembered about one connected agent.
type AgentState struct {
	// BackupPath is the agent's settings from before init first changed them.
	// Empty if init changed nothing that already existed.
	BackupPath string `json:"backup_path,omitempty"`
}

// ResolveAIKey returns the key a provider should use: its environment variable
// if set (never persisted back to config.json), else the key stored in cfg.
// Empty means AI Mode is unconfigured.
func ResolveAIKey(cfg Config, providerID, keyEnv string) string {
	if v := os.Getenv(keyEnv); v != "" {
		return v
	}
	return cfg.AIKeys[providerID]
}

// Default returns infrapal-token-advisor's out-of-the-box configuration.
func Default() Config {
	return Config{
		OTLPPort:        DefaultOTLPPort,
		APIPort:         DefaultAPIPort,
		RetentionMonths: DefaultRetentionMonths,
	}
}

// Load reads config.json, falling back to Default() if it doesn't exist yet.
func Load(p paths.Paths) (Config, error) {
	data, err := os.ReadFile(p.ConfigPath)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	// Installs from before multi-agent support kept one backup path and one
	// API key. Carry them over, or uninstall would forget what to restore and
	// AI Mode would lose its key.
	var old struct {
		SettingsBackupPath string `json:"settings_backup_path"`
		AnthropicAPIKey    string `json:"anthropic_api_key"`
	}
	if err := json.Unmarshal(data, &old); err != nil {
		return Config{}, err
	}
	if cfg.Agents == nil && old.SettingsBackupPath != "" {
		cfg.Agents = map[string]AgentState{"claude-code": {BackupPath: old.SettingsBackupPath}}
	}
	if cfg.AIKeys == nil && old.AnthropicAPIKey != "" {
		cfg.AIKeys = map[string]string{"anthropic": old.AnthropicAPIKey}
	}
	return cfg, nil
}

// Save writes cfg to config.json inside the infrapal-token-advisor base directory.
func Save(p paths.Paths, cfg Config) error {
	if err := p.EnsureBaseDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(p.ConfigPath, data, paths.FileMode); err != nil {
		return err
	}
	// config.json can now hold a secret (AIKeys). WriteFile only
	// applies FileMode when creating the file, so explicitly chmod to
	// tighten a pre-existing looser-mode file too — mirrors store.Open's
	// belt-and-suspenders chmod on token-advisor.db.
	return os.Chmod(p.ConfigPath, paths.FileMode)
}
