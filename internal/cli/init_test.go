package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/config"
)

// withFakeHome points $HOME at a fresh temp dir for the duration of a test,
// so paths.Resolve() (and everything ccotel writes) stays isolated from the
// real user environment.
func withFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func readConfig(t *testing.T, home string) config.Config {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".ccotel", "config.json"))
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("unmarshal config.json: %v", err)
	}
	return cfg
}

func readSettingsEnv(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}
	env, ok := doc["env"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json has no env object: %v", doc)
	}
	return env
}

func TestRunInit_NoPreexistingSettings(t *testing.T) {
	home := withFakeHome(t)
	fake := withFakeService(t)

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	if fake.installCalls != 1 {
		t.Errorf("installCalls = %d, want 1", fake.installCalls)
	}

	cfg := readConfig(t, home)
	if cfg.SettingsBackupPath != "" {
		t.Errorf("SettingsBackupPath = %q, want empty when settings.json didn't preexist", cfg.SettingsBackupPath)
	}
	if cfg.RawBodies {
		t.Error("RawBodies = true, want false without --raw-bodies")
	}

	env := readSettingsEnv(t, home)
	want := wantedEnv(cfg)
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%q] = %v, want %q", k, env[k], v)
		}
	}
	if _, present := env["OTEL_LOG_RAW_API_BODIES"]; present {
		t.Error("OTEL_LOG_RAW_API_BODIES set without --raw-bodies")
	}
}

func TestRunInit_BacksUpAndPreservesExistingSettings(t *testing.T) {
	home := withFakeHome(t)
	withFakeService(t)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	original := `{"permissions": {"allow": ["Bash(echo:*)"]}, "env": {"ANTHROPIC_AUTH_TOKEN": "sk-real-token"}}`
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write settings.json fixture: %v", err)
	}

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	cfg := readConfig(t, home)
	if cfg.SettingsBackupPath == "" {
		t.Fatal("SettingsBackupPath is empty, want a backup path since settings.json preexisted")
	}
	backupData, err := os.ReadFile(cfg.SettingsBackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupData) != original {
		t.Errorf("backup content = %q, want original %q", backupData, original)
	}

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal settings.json: %v", err)
	}
	perms, ok := doc["permissions"].(map[string]any)
	if !ok || perms["allow"] == nil {
		t.Errorf("permissions key lost: %v", doc["permissions"])
	}
	env := doc["env"].(map[string]any)
	if env["ANTHROPIC_AUTH_TOKEN"] != "sk-real-token" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN clobbered: %v", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if env["CLAUDE_CODE_ENABLE_TELEMETRY"] != "1" {
		t.Errorf("CLAUDE_CODE_ENABLE_TELEMETRY not set: %v", env["CLAUDE_CODE_ENABLE_TELEMETRY"])
	}
}

func TestRunInit_RawBodies(t *testing.T) {
	home := withFakeHome(t)
	withFakeService(t)

	if err := runInit([]string{"--raw-bodies"}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	cfg := readConfig(t, home)
	if !cfg.RawBodies {
		t.Error("RawBodies = false, want true with --raw-bodies")
	}

	bodiesDir := filepath.Join(home, ".ccotel", "bodies")
	if info, err := os.Stat(bodiesDir); err != nil || !info.IsDir() {
		t.Errorf("bodies dir %s not created: %v", bodiesDir, err)
	}

	env := readSettingsEnv(t, home)
	want := "file:" + bodiesDir
	if env["OTEL_LOG_RAW_API_BODIES"] != want {
		t.Errorf("OTEL_LOG_RAW_API_BODIES = %v, want %q", env["OTEL_LOG_RAW_API_BODIES"], want)
	}
}

func TestRunInit_IdempotentSecondRun(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)

	if err := runInit(nil); err != nil {
		t.Fatalf("first runInit: %v", err)
	}
	if err := runInit(nil); err != nil {
		t.Fatalf("second runInit: %v", err)
	}
}
