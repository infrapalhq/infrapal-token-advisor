package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const systemdUnitName = "infrapal-token-advisor.service"

func systemdUnitPath(home string) string {
	return filepath.Join(home, ".config", "systemd", "user", systemdUnitName)
}

// systemdUnitFile renders the user unit. Restart=always + RestartSec pair
// with launchd's KeepAlive equivalent (spec gotcha 7: a dead collector
// silently loses exports, so it must come back on its own).
func systemdUnitFile(p Paths) []byte {
	return []byte(fmt.Sprintf(`[Unit]
Description=infrapal-token-advisor local Claude Code telemetry collector

[Service]
ExecStart=%s start
Restart=always
RestartSec=2
StandardOutput=append:%s
StandardError=append:%s

[Install]
WantedBy=default.target
`, p.ExecPath, p.StdoutLog, p.StderrLog))
}

type systemdService struct {
	p        Paths
	unitPath string
}

func (s *systemdService) Install() error {
	if err := os.MkdirAll(filepath.Dir(s.unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(s.unitPath, systemdUnitFile(s.p), 0o644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	if err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	return runCommand("systemctl", "--user", "enable", "--now", systemdUnitName)
}

func (s *systemdService) Uninstall() error {
	// Best-effort: disable --now may fail if the unit was already stopped
	// or systemd --user isn't reachable (e.g. no active session); the file
	// removal below is what makes uninstall itself succeed.
	_ = runCommand("systemctl", "--user", "disable", "--now", systemdUnitName)
	if err := os.Remove(s.unitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit file: %w", err)
	}
	_ = runCommand("systemctl", "--user", "daemon-reload")
	return nil
}

func (s *systemdService) Start() error {
	return runCommand("systemctl", "--user", "start", systemdUnitName)
}

func (s *systemdService) Stop() error {
	return runCommand("systemctl", "--user", "stop", systemdUnitName)
}

func (s *systemdService) Status() (Status, error) {
	if _, err := os.Stat(s.unitPath); os.IsNotExist(err) {
		return Status{Installed: false, Detail: "not installed"}, nil
	} else if err != nil {
		return Status{}, err
	}

	out, err := exec.Command("systemctl", "--user", "is-active", systemdUnitName).Output()
	state := strings.TrimSpace(string(out))
	if state == "" {
		state = "unknown"
	}
	if err != nil && state == "unknown" {
		return Status{Installed: true, Running: false, Detail: "installed, systemctl unreachable"}, nil
	}
	return Status{Installed: true, Running: state == "active", Detail: state}, nil
}
