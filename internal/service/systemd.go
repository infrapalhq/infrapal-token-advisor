package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
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
	if err := checkExecPath(s.p.ExecPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.unitPath), 0o755); err != nil {
		return fmt.Errorf("create systemd user dir: %w", err)
	}
	if err := os.WriteFile(s.unitPath, systemdUnitFile(s.p), 0o644); err != nil {
		return fmt.Errorf("write unit file: %w", err)
	}
	if err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return err
	}
	if err := runCommand("systemctl", "--user", "enable", systemdUnitName); err != nil {
		return err
	}
	// Best-effort: without linger, systemd stops user services at logout and
	// doesn't start them until the next login. doctor reports it if this fails.
	_ = runCommand("loginctl", "enable-linger")
	// restart, not start, so a repeat init picks up the current binary.
	return runCommand("systemctl", "--user", "restart", systemdUnitName)
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

// CheckAutostart looks for what stops systemd starting the unit at login.
func (s *systemdService) CheckAutostart() error {
	data, err := os.ReadFile(s.unitPath)
	if os.IsNotExist(err) {
		return errors.New("service isn't installed")
	}
	if err != nil {
		return err
	}
	if path := unitExecPath(string(data)); path == "" {
		return errors.New("can't read the binary path from " + s.unitPath)
	} else if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("the service runs %s, which no longer exists", path)
	}
	out, _ := exec.Command("systemctl", "--user", "is-enabled", systemdUnitName).Output()
	if state := strings.TrimSpace(string(out)); state != "" && state != "enabled" {
		return fmt.Errorf("the service is %s in systemd, so it won't start at login", state)
	}
	return nil
}

// unitExecPath returns the binary a unit file runs, or "" if it can't be found.
func unitExecPath(unit string) string {
	for _, line := range strings.Split(unit, "\n") {
		if cmd, ok := strings.CutPrefix(line, "ExecStart="); ok {
			return strings.TrimSuffix(cmd, " start")
		}
	}
	return ""
}

// LingerEnabled reports whether systemd keeps this user's services running
// with no login session. Without it they start at login and stop at logout.
func LingerEnabled() (bool, error) {
	u, err := user.Current()
	if err != nil {
		return false, err
	}
	out, err := exec.Command("loginctl", "show-user", u.Username, "--property=Linger").Output()
	if err != nil {
		return false, err
	}
	return parseLinger(string(out)), nil
}

// parseLinger reads `loginctl show-user --property=Linger` output.
func parseLinger(out string) bool {
	return strings.TrimSpace(out) == "Linger=yes"
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
