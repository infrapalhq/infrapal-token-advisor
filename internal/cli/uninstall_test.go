package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunUninstall_RestoresSettingsAndRemovesBaseDir(t *testing.T) {
	home := withFakeHome(t)
	fake := withFakeService(t)

	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o700); err != nil {
		t.Fatalf("mkdir .claude: %v", err)
	}
	original := `{"env": {"ANTHROPIC_AUTH_TOKEN": "sk-real-token"}}`
	settingsPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(original), 0o600); err != nil {
		t.Fatalf("write settings.json fixture: %v", err)
	}

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	if err := runUninstall(nil); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if fake.uninstallCalls != 1 {
		t.Errorf("uninstallCalls = %d, want 1", fake.uninstallCalls)
	}

	restored, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if string(restored) != original {
		t.Errorf("settings.json = %q, want restored original %q", restored, original)
	}

	if _, err := os.Stat(filepath.Join(home, ".ccotel")); !os.IsNotExist(err) {
		t.Errorf(".ccotel still exists after uninstall (err=%v), want removed", err)
	}
}

func TestRunUninstall_KeepDB(t *testing.T) {
	home := withFakeHome(t)
	withFakeService(t)

	if err := runInit(nil); err != nil {
		t.Fatalf("runInit: %v", err)
	}
	if err := runUninstall([]string{"--keep-db"}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(home, ".ccotel")); err != nil {
		t.Errorf(".ccotel removed despite --keep-db: %v", err)
	}
}

func TestRunUninstall_NoPriorInit(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)

	// No runInit call: settings.json doesn't exist, config.json doesn't
	// exist, no backup path recorded. Should no-op gracefully, not error.
	if err := runUninstall(nil); err != nil {
		t.Fatalf("runUninstall without prior init: %v", err)
	}
}
