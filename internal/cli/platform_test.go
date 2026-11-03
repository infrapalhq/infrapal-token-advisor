package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/service"
)

func withNoServiceManager(t *testing.T) {
	t.Helper()
	orig := newService
	newService = func(service.Paths) (service.Service, error) { return nil, service.ErrUnsupported }
	t.Cleanup(func() { newService = orig })
}

// On a platform with no service manager, commands explain what to do instead
// of failing.
func TestCommands_ExplainAMissingServiceManager(t *testing.T) {
	withFakeHome(t)
	withNoServiceManager(t)
	if out := stdout(t, func() error { return runInit(nil) }); !strings.Contains(out, "no supported service manager") {
		t.Errorf("init output:\n%s", out)
	}
	if out := stdout(t, func() error { return runStatus(nil) }); !strings.Contains(out, "no supported service manager") {
		t.Errorf("status output:\n%s", out)
	}
	if out := stdout(t, func() error { return runUninstall([]string{"--keep-db"}) }); !strings.Contains(out, "no supported service manager") {
		t.Errorf("uninstall output:\n%s", out)
	}
}

func TestInit_ReportsAServiceThatWontInstall(t *testing.T) {
	withFakeHome(t)
	fake := withFakeService(t)
	fake.installErr = errors.New("launchctl refused")
	err := runInit(nil)
	if err == nil || !strings.Contains(err.Error(), "launchctl refused") {
		t.Errorf("err = %v", err)
	}
}

func TestCommands_RejectUnknownFlags(t *testing.T) {
	withFakeHome(t)
	withFakeService(t)
	for name, run := range map[string]func([]string) error{
		"init": runInit, "status": runStatus, "doctor": runDoctor, "stop": runStop,
		"uninstall": runUninstall, "logs": runLogs, "start": runStart, "ui": runUI,
	} {
		if err := run([]string{"--no-such-flag"}); err == nil {
			t.Errorf("%s accepted an unknown flag", name)
		}
	}
}

// doctor's warnings are how a user finds out the service has been crashing.
func TestDoctor_WarnsAboutCrashesAndMissingTraces(t *testing.T) {
	home := withFakeHome(t)
	withFakeService(t)
	if err := runInit(nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, ".infrapal-token-advisor")
	now := time.Now().Format(time.RFC3339)
	log := "time=" + now + ` level=WARN msg="previous run did not shut down cleanly"` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "token-advisor.log"), []byte(log), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "collector.err.log"), []byte("fatal error: out of memory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	db, _, err := openDB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO events_raw (ts, event_name, body) VALUES (?, 'user_prompt', '{}')`, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	db.Close()

	out := stdout(t, func() error { return runDoctor(nil) })
	for _, want := range []string{"unclean shutdown", "no fatal output from the service", "collector.err.log", "no spans yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
}
