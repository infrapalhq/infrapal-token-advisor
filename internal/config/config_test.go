package config

import (
	"os"
	"path/filepath"
	"strings"
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

func TestLoad_RemovesAIModeKeys(t *testing.T) {
	p := paths.Paths{BaseDir: t.TempDir()}
	p.ConfigPath = filepath.Join(p.BaseDir, "config.json")
	old := `{"api_port":5000,"anthropic_api_key":"sk-old","ai_provider":"anthropic","ai_keys":{"anthropic":"sk-new"}}`
	if err := os.WriteFile(p.ConfigPath, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.APIPort != 5000 {
		t.Errorf("APIPort = %d, want the saved 5000", cfg.APIPort)
	}
	data, err := os.ReadFile(p.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range removedFields {
		if strings.Contains(string(data), field) {
			t.Errorf("config.json still has %s: %s", field, data)
		}
	}
	if strings.Contains(string(data), "sk-") {
		t.Errorf("config.json still has a key: %s", data)
	}
}
