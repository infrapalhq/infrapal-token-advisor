package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newLaunchd(t *testing.T, f *fakeRunner) *launchdService {
	t.Helper()
	dir := t.TempDir()
	return &launchdService{
		p:         Paths{ExecPath: filepath.Join(dir, "token-advisor")},
		plistPath: filepath.Join(dir, "agents", launchdLabel+".plist"),
		run:       f,
	}
}

// installLaunchdFiles puts a real plist and binary in place.
func installLaunchdFiles(t *testing.T, s *launchdService) {
	t.Helper()
	if err := os.WriteFile(s.p.ExecPath, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(s.plistPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.plistPath, launchdPlistXML(s.p), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchd_InstallReloadsTheJob(t *testing.T) {
	f := &fakeRunner{}
	s := newLaunchd(t, f)
	if err := s.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	wantCalls(t, f, "launchctl unload "+s.plistPath, "launchctl load -w "+s.plistPath)
	if _, err := os.Stat(s.plistPath); err != nil {
		t.Errorf("plist not written: %v", err)
	}
}

func TestLaunchd_InstallIgnoresUnloadFailure(t *testing.T) {
	f := &fakeRunner{fail: map[string]error{"launchctl unload": errCommand}}
	if err := newLaunchd(t, f).Install(); err != nil {
		t.Errorf("a first install has nothing to unload: %v", err)
	}
}

func TestLaunchd_InstallReportsLoadFailure(t *testing.T) {
	f := &fakeRunner{fail: map[string]error{"launchctl load": errCommand}}
	if err := newLaunchd(t, f).Install(); err == nil {
		t.Error("Install should fail when the job won't load")
	}
}

func TestLaunchd_InstallRefusesGoRunBinary(t *testing.T) {
	f := &fakeRunner{}
	s := newLaunchd(t, f)
	s.p.ExecPath = "/tmp/go-build123/b001/exe/token-advisor"
	if err := s.Install(); err == nil {
		t.Fatal("Install should refuse a temporary binary")
	}
	wantCalls(t, f)
	if _, err := os.Stat(s.plistPath); !os.IsNotExist(err) {
		t.Errorf("no plist should be written: %v", err)
	}
}

// launchd remembers -w across restarts, and skips disabled jobs at login, so a
// single stop with it would turn autostart off for good.
func TestLaunchd_StopNeverDisablesTheJob(t *testing.T) {
	f := &fakeRunner{}
	s := newLaunchd(t, f)
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	wantCalls(t, f, "launchctl unload "+s.plistPath)
}

func TestLaunchd_StartLoadsTheJob(t *testing.T) {
	f := &fakeRunner{}
	s := newLaunchd(t, f)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	wantCalls(t, f, "launchctl load -w "+s.plistPath)
}

func TestLaunchd_UninstallRemovesThePlistEvenIfUnloadFails(t *testing.T) {
	f := &fakeRunner{fail: map[string]error{"launchctl unload": errCommand}}
	s := newLaunchd(t, f)
	installLaunchdFiles(t, s)
	if err := s.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(s.plistPath); !os.IsNotExist(err) {
		t.Errorf("plist should be gone: %v", err)
	}
	if err := s.Uninstall(); err != nil {
		t.Errorf("a second Uninstall: %v", err)
	}
}

func TestLaunchd_CheckAutostart(t *testing.T) {
	disabled := map[string]string{"launchctl print-disabled": `"` + launchdLabel + `" => disabled`}

	t.Run("not installed", func(t *testing.T) {
		err := newLaunchd(t, &fakeRunner{}).CheckAutostart()
		if err == nil || !strings.Contains(err.Error(), "isn't installed") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("binary gone", func(t *testing.T) {
		s := newLaunchd(t, &fakeRunner{})
		installLaunchdFiles(t, s)
		if err := os.Remove(s.p.ExecPath); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckAutostart(); err == nil || !strings.Contains(err.Error(), "no longer exists") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("disabled", func(t *testing.T) {
		s := newLaunchd(t, &fakeRunner{output: disabled})
		installLaunchdFiles(t, s)
		if err := s.CheckAutostart(); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("healthy", func(t *testing.T) {
		s := newLaunchd(t, &fakeRunner{output: map[string]string{"launchctl print-disabled": `"com.other" => disabled`}})
		installLaunchdFiles(t, s)
		if err := s.CheckAutostart(); err != nil {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("launchctl can't say", func(t *testing.T) {
		s := newLaunchd(t, &fakeRunner{fail: map[string]error{"launchctl print-disabled": errCommand}})
		installLaunchdFiles(t, s)
		if err := s.CheckAutostart(); err != nil {
			t.Errorf("err = %v", err)
		}
	})
}

func TestLaunchd_Status(t *testing.T) {
	list := "launchctl list"
	cases := []struct {
		name    string
		runner  *fakeRunner
		running bool
		detail  string
	}{
		{"running", &fakeRunner{output: map[string]string{list: "\"PID\" = 4242;"}}, true, "running (pid 4242)"},
		{"stopped", &fakeRunner{output: map[string]string{list: "\"PID\" = 0;"}}, false, "loaded but not running"},
		{"no pid", &fakeRunner{output: map[string]string{list: "\"Label\" = \"x\";"}}, false, "loaded but not running"},
		{"not loaded", &fakeRunner{fail: map[string]error{list: errCommand}}, false, "installed but not loaded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newLaunchd(t, tc.runner)
			installLaunchdFiles(t, s)
			st, err := s.Status()
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if !st.Installed || st.Running != tc.running || st.Detail != tc.detail {
				t.Errorf("Status = %+v, want running=%v detail=%q", st, tc.running, tc.detail)
			}
		})
	}

	st, err := newLaunchd(t, &fakeRunner{}).Status()
	if err != nil || st.Installed {
		t.Errorf("with no plist: %+v, %v", st, err)
	}
}
