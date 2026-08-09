package claudesettings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_MissingFile(t *testing.T) {
	doc, existed, err := Load(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if existed {
		t.Errorf("existed = true, want false for a missing file")
	}
	if len(doc) != 0 {
		t.Errorf("doc = %v, want empty", doc)
	}
}

func TestMergeEnv_PreservesUnrelatedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{
		"permissions": {"allow": ["Bash(echo:*)"]},
		"env": {"ANTHROPIC_AUTH_TOKEN": "sk-real-token", "OTEL_EXPORTER_OTLP_ENDPOINT": "http://old-collector:4317"}
	}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	doc, existed, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !existed {
		t.Fatalf("existed = false, want true")
	}

	changes, err := MergeEnv(doc, map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
		"CLAUDE_CODE_ENABLE_TELEMETRY": "1",
	})
	if err != nil {
		t.Fatalf("MergeEnv: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d: %+v", len(changes), changes)
	}

	if err := Save(path, doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reread, _, err := Load(path)
	if err != nil {
		t.Fatalf("re-Load: %v", err)
	}

	perms, ok := reread["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions key lost or wrong type: %v", reread["permissions"])
	}
	allow, ok := perms["allow"].([]any)
	if !ok || len(allow) != 1 || allow[0] != "Bash(echo:*)" {
		t.Errorf("permissions.allow corrupted: %v", perms["allow"])
	}

	env, ok := reread["env"].(map[string]any)
	if !ok {
		t.Fatalf("env key lost or wrong type: %v", reread["env"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-real-token" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN clobbered: %v", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["OTEL_EXPORTER_OTLP_ENDPOINT"] != "http://localhost:4318" {
		t.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT = %v, want overwritten", env["OTEL_EXPORTER_OTLP_ENDPOINT"])
	}
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("CLAUDE_CODE_ENABLE_TELEMETRY = %v, want added", env["CLAUDE_CODE_ENABLE_TELEMETRY"])
	}
}

func TestMergeEnv_NoOpWhenAlreadyCorrect(t *testing.T) {
	doc := Doc{"env": map[string]any{"FOO": "bar"}}
	changes, err := MergeEnv(doc, map[string]string{"FOO": "bar"})
	if err != nil {
		t.Fatalf("MergeEnv: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("expected no changes when value already matches, got %+v", changes)
	}
}

func TestMergeEnv_RejectsNonObjectEnv(t *testing.T) {
	doc := Doc{"env": "not-an-object"}
	if _, err := MergeEnv(doc, map[string]string{"FOO": "bar"}); err == nil {
		t.Error("expected an error when env is not a JSON object")
	}
}

func TestBackupAndRestore(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{"env": {"FOO": "original"}}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	backupPath, err := Backup(path, time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected a non-empty backup path for an existing file")
	}

	doc, _, _ := Load(path)
	if _, err := MergeEnv(doc, map[string]string{"FOO": "changed"}); err != nil {
		t.Fatalf("MergeEnv: %v", err)
	}
	if err := Save(path, doc); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := Restore(path, backupPath); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	restored, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load after restore: %v", err)
	}
	env := restored["env"].(map[string]any)
	if env["FOO"] != "original" {
		t.Errorf("FOO after restore = %v, want original", env["FOO"])
	}
}

func TestBackup_MissingFileReturnsEmptyPath(t *testing.T) {
	backupPath, err := Backup(filepath.Join(t.TempDir(), "settings.json"), time.Now())
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if backupPath != "" {
		t.Errorf("backupPath = %q, want empty when source doesn't exist", backupPath)
	}
}

func TestSave_ProducesValidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	doc := Doc{"env": map[string]any{"A": "1"}}
	if err := Save(path, doc); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
}
