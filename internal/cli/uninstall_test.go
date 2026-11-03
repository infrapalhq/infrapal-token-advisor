package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
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

	if _, err := os.Stat(filepath.Join(home, ".infrapal-token-advisor")); !os.IsNotExist(err) {
		t.Errorf(".infrapal-token-advisor still exists after uninstall (err=%v), want removed", err)
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

	if _, err := os.Stat(filepath.Join(home, ".infrapal-token-advisor")); err != nil {
		t.Errorf(".infrapal-token-advisor removed despite --keep-db: %v", err)
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

func TestRunStop_StopsTheService(t *testing.T) {
	withFakeHome(t)
	fake := withFakeService(t)
	if err := runStop(nil); err != nil {
		t.Fatalf("runStop: %v", err)
	}
	if fake.stopCalls != 1 {
		t.Errorf("stopCalls = %d", fake.stopCalls)
	}
}

func TestRunStop_WithNoServiceManager(t *testing.T) {
	withFakeHome(t)
	orig := newService
	newService = func(service.Paths) (service.Service, error) { return nil, service.ErrUnsupported }
	t.Cleanup(func() { newService = orig })
	if err := runStop(nil); err != nil {
		t.Errorf("an unsupported platform should explain, not fail: %v", err)
	}
}

func TestRunLogs_ShowsTheTail(t *testing.T) {
	home := withFakeHome(t)
	dir := filepath.Join(home, ".infrapal-token-advisor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	log := "time=1 level=INFO msg=one\ntime=2 level=ERROR msg=two\ntime=3 level=INFO msg=three\n"
	if err := os.WriteFile(filepath.Join(dir, "token-advisor.log"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := stdout(t, func() error { return runLogs([]string{"-n", "2"}) }); strings.Contains(out, "msg=one") || !strings.Contains(out, "msg=three") {
		t.Errorf("-n 2 output:\n%s", out)
	}
	if out := stdout(t, func() error { return runLogs([]string{"--errors"}) }); !strings.Contains(out, "msg=two") || strings.Contains(out, "msg=three") {
		t.Errorf("--errors output:\n%s", out)
	}
}
