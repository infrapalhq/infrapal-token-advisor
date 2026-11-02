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
