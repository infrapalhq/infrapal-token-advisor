// Package config manages ccotel's own persisted settings, distinct from the
// Claude Code settings.json that init patches. It lives at
// ~/.ccotel/config.json and holds the handful of choices that change
// ccotel's own behavior (ports, raw-body capture).
package config

import (
	"encoding/json"
	"os"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

const (
	// DefaultOTLPPort is where the OTLP http/protobuf receiver listens.
	DefaultOTLPPort = 4318
	// DefaultAPIPort is where the dashboard's JSON API + embedded UI listens.
	DefaultAPIPort = 4319
)

// Config is ccotel's own settings, persisted as JSON.
type Config struct {
	OTLPPort int `json:"otlp_port"`
	APIPort  int `json:"api_port"`

	// RawBodies enables OTEL_LOG_RAW_API_BODIES in file: mode. Never set to
	// inline mode: inline truncates at the content limit and bloats the DB.
	RawBodies bool `json:"raw_bodies"`

	// SettingsBackupPath is the ~/.claude/settings.json backup init made
	// before patching it, e.g. "settings.json.ccotel-backup-1234567890".
	// Empty if init never ran, or settings.json didn't exist yet at init
	// time. uninstall uses this to restore settings.json byte-for-byte.
	SettingsBackupPath string `json:"settings_backup_path"`

	// AnthropicAPIKey is the user's own key, pasted into the AI Mode
	// settings panel. Used only in direct response to the "Generate"
	// button (internal/ai, internal/api/ai.go) — ccotel's one deliberate
	// network-egress exception; see ResolveAnthropicAPIKey.
	AnthropicAPIKey string `json:"anthropic_api_key,omitempty"`
}

// ResolveAnthropicAPIKey returns the key AI Mode should use: the
// ANTHROPIC_API_KEY environment variable if set (never persisted back to
// config.json), else the key stored in cfg. Empty means AI Mode is
// unconfigured.
func ResolveAnthropicAPIKey(cfg Config) string {
	if v := os.Getenv("ANTHROPIC_API_KEY"); v != "" {
		return v
	}
	return cfg.AnthropicAPIKey
}

// Default returns ccotel's out-of-the-box configuration.
func Default() Config {
	return Config{
		OTLPPort: DefaultOTLPPort,
		APIPort:  DefaultAPIPort,
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
	return cfg, nil
}

// Save writes cfg to config.json inside the ccotel base directory.
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
	// config.json can now hold a secret (AnthropicAPIKey). WriteFile only
	// applies FileMode when creating the file, so explicitly chmod to
	// tighten a pre-existing looser-mode file too — mirrors store.Open's
	// belt-and-suspenders chmod on ccotel.db.
	return os.Chmod(p.ConfigPath, paths.FileMode)
}
