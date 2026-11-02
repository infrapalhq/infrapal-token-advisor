package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

func TestLoad_CarriesOverTheOldBackupPath(t *testing.T) {
	p := paths.Paths{BaseDir: t.TempDir()}
	p.ConfigPath = filepath.Join(p.BaseDir, "config.json")
	if err := os.WriteFile(p.ConfigPath, []byte(`{"settings_backup_path":"/old/backup"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Agents["claude-code"].BackupPath; got != "/old/backup" {
		t.Errorf("BackupPath = %q", got)
	}

	if err := Save(p, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	again, err := Load(p)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if got := again.Agents["claude-code"].BackupPath; got != "/old/backup" {
		t.Errorf("BackupPath after a save = %q", got)
	}
}

func TestLoad_CarriesOverTheOldAPIKey(t *testing.T) {
	p := paths.Paths{BaseDir: t.TempDir()}
	p.ConfigPath = filepath.Join(p.BaseDir, "config.json")
	if err := os.WriteFile(p.ConfigPath, []byte(`{"anthropic_api_key":"sk-old"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.AIKeys["anthropic"]; got != "sk-old" {
		t.Errorf("key = %q", got)
	}
}

func TestResolveAIKey_EnvWinsAndKeysStaySeparate(t *testing.T) {
	cfg := Config{AIKeys: map[string]string{"one": "stored-one", "two": "stored-two"}}
	t.Setenv("KEY_ONE", "")
	if got := ResolveAIKey(cfg, "one", "KEY_ONE"); got != "stored-one" {
		t.Errorf("stored = %q", got)
	}
	if got := ResolveAIKey(cfg, "two", "KEY_ONE"); got != "stored-two" {
		t.Errorf("another provider's = %q", got)
	}
	t.Setenv("KEY_ONE", "from-env")
	if got := ResolveAIKey(cfg, "one", "KEY_ONE"); got != "from-env" {
		t.Errorf("env = %q", got)
	}
	if got := ResolveAIKey(Config{}, "none", "KEY_NONE"); got != "" {
		t.Errorf("nothing configured = %q", got)
	}
}
