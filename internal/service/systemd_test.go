package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	systemctl  = "systemctl --user "
	isEnabled  = systemctl + "is-enabled"
	isActive   = systemctl + "is-active"
	reloadCmd  = systemctl + "daemon-reload"
	enableCmd  = systemctl + "enable " + systemdUnitName
	restartCmd = systemctl + "restart " + systemdUnitName
	lingerCmd  = "loginctl enable-linger"
)

func newSystemd(t *testing.T, f *fakeRunner) *systemdService {
	t.Helper()
	dir := t.TempDir()
	return &systemdService{
		p:        Paths{ExecPath: filepath.Join(dir, "token-advisor")},
		unitPath: filepath.Join(dir, "user", systemdUnitName),
		run:      f,
	}
}

// installSystemdFiles puts a real unit file and binary in place.
func installSystemdFiles(t *testing.T, s *systemdService) {
	t.Helper()
	if err := os.WriteFile(s.p.ExecPath, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(s.unitPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.unitPath, systemdUnitFile(s.p), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSystemd_InstallEnablesLingersAndRestarts(t *testing.T) {
	f := &fakeRunner{}
	s := newSystemd(t, f)
	if err := s.Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	wantCalls(t, f, reloadCmd, enableCmd, lingerCmd, restartCmd)
	if _, err := os.Stat(s.unitPath); err != nil {
		t.Errorf("unit file not written: %v", err)
	}
}

func TestSystemd_InstallStopsAtTheFirstRequiredFailure(t *testing.T) {
	f := &fakeRunner{fail: map[string]error{enableCmd: errCommand}}
	if err := newSystemd(t, f).Install(); err == nil {
		t.Fatal("Install should fail when enable fails")
	}
	wantCalls(t, f, reloadCmd, enableCmd)
}

func TestSystemd_InstallIgnoresLingerFailure(t *testing.T) {
	f := &fakeRunner{fail: map[string]error{lingerCmd: errCommand}}
	if err := newSystemd(t, f).Install(); err != nil {
		t.Fatalf("Install: %v", err)
	}
	wantCalls(t, f, reloadCmd, enableCmd, lingerCmd, restartCmd)
}

func TestSystemd_InstallRefusesGoRunBinary(t *testing.T) {
	f := &fakeRunner{}
	s := newSystemd(t, f)
	s.p.ExecPath = "/tmp/go-build123/b001/exe/token-advisor"
	if err := s.Install(); err == nil {
		t.Fatal("Install should refuse a temporary binary")
	}
	wantCalls(t, f)
}

func TestSystemd_StartAndStop(t *testing.T) {
	f := &fakeRunner{}
	s := newSystemd(t, f)
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	wantCalls(t, f, systemctl+"start "+systemdUnitName, systemctl+"stop "+systemdUnitName)
}

func TestSystemd_UninstallRemovesTheUnitEvenIfDisableFails(t *testing.T) {
	disable := systemctl + "disable --now " + systemdUnitName
	f := &fakeRunner{fail: map[string]error{disable: errCommand}}
	s := newSystemd(t, f)
	installSystemdFiles(t, s)
	if err := s.Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	wantCalls(t, f, disable, reloadCmd)
	if _, err := os.Stat(s.unitPath); !os.IsNotExist(err) {
		t.Errorf("unit file should be gone: %v", err)
	}
}

func TestSystemd_CheckAutostart(t *testing.T) {
	t.Run("not installed", func(t *testing.T) {
		err := newSystemd(t, &fakeRunner{}).CheckAutostart()
		if err == nil || !strings.Contains(err.Error(), "isn't installed") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("binary gone", func(t *testing.T) {
		s := newSystemd(t, &fakeRunner{})
		installSystemdFiles(t, s)
		if err := os.Remove(s.p.ExecPath); err != nil {
			t.Fatal(err)
		}
		if err := s.CheckAutostart(); err == nil || !strings.Contains(err.Error(), "no longer exists") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("disabled, reported with a non-zero exit", func(t *testing.T) {
		s := newSystemd(t, &fakeRunner{
			output: map[string]string{isEnabled: "disabled\n"},
			fail:   map[string]error{isEnabled: errCommand},
		})
		installSystemdFiles(t, s)
		if err := s.CheckAutostart(); err == nil || !strings.Contains(err.Error(), "is disabled") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("binary path has a space", func(t *testing.T) {
		s := newSystemd(t, &fakeRunner{output: map[string]string{isEnabled: "enabled\n"}})
		s.p.ExecPath = filepath.Join(t.TempDir(), "my tools", "token-advisor")
		if err := os.MkdirAll(filepath.Dir(s.p.ExecPath), 0o755); err != nil {
			t.Fatal(err)
		}
		installSystemdFiles(t, s)
		if err := s.CheckAutostart(); err != nil {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("enabled", func(t *testing.T) {
		s := newSystemd(t, &fakeRunner{output: map[string]string{isEnabled: "enabled\n"}})
		installSystemdFiles(t, s)
		if err := s.CheckAutostart(); err != nil {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("systemctl unreachable", func(t *testing.T) {
		s := newSystemd(t, &fakeRunner{fail: map[string]error{isEnabled: errCommand}})
		installSystemdFiles(t, s)
		if err := s.CheckAutostart(); err != nil {
			t.Errorf("err = %v", err)
		}
	})
}

func TestSystemd_Status(t *testing.T) {
	cases := []struct {
		name    string
		runner  *fakeRunner
		running bool
		detail  string
	}{
		{"active", &fakeRunner{output: map[string]string{isActive: "active\n"}}, true, "active"},
		{"inactive", &fakeRunner{
			output: map[string]string{isActive: "inactive\n"},
			fail:   map[string]error{isActive: errCommand},
		}, false, "inactive"},
		{"failed", &fakeRunner{
			output: map[string]string{isActive: "failed\n"},
			fail:   map[string]error{isActive: errCommand},
		}, false, "failed"},
		{"unreachable", &fakeRunner{fail: map[string]error{isActive: errCommand}}, false, "installed, systemctl unreachable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newSystemd(t, tc.runner)
			installSystemdFiles(t, s)
			st, err := s.Status()
			if err != nil {
				t.Fatalf("Status: %v", err)
			}
			if !st.Installed || st.Running != tc.running || st.Detail != tc.detail {
				t.Errorf("Status = %+v, want running=%v detail=%q", st, tc.running, tc.detail)
			}
		})
	}

	st, err := newSystemd(t, &fakeRunner{}).Status()
	if err != nil || st.Installed {
		t.Errorf("with no unit file: %+v, %v", st, err)
	}
}
