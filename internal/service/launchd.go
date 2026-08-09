package service

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// launchdLabel is the launchd service identifier: the plist filename (sans
// extension) and the Label key inside it, and what `launchctl list` looks
// it up by.
const launchdLabel = "com.ccotel.collector"

func launchdPlistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist")
}

// launchdPlistXML renders the LaunchAgent property list. RunAtLoad +
// KeepAlive together mean launchd starts the collector on login and
// restarts it if it ever exits — the spec calls this out explicitly
// (gotcha 7: Claude Code doesn't buffer exports to disk, so a dead
// collector silently loses data).
func launchdPlistXML(p Paths) []byte {
	esc := xml.EscapeText
	var execPath, stdoutLog, stderrLog strings.Builder
	esc(&execPath, []byte(p.ExecPath))
	esc(&stdoutLog, []byte(p.StdoutLog))
	esc(&stderrLog, []byte(p.StderrLog))

	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>start</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, launchdLabel, execPath.String(), stdoutLog.String(), stderrLog.String()))
}

type launchdService struct {
	p         Paths
	plistPath string
}

func (s *launchdService) Install() error {
	if err := os.MkdirAll(filepath.Dir(s.plistPath), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}
	if err := os.WriteFile(s.plistPath, launchdPlistXML(s.p), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}
	return runCommand("launchctl", "load", "-w", s.plistPath)
}

func (s *launchdService) Uninstall() error {
	// Best-effort unload: if it's already unloaded (e.g. after a crash),
	// launchctl exits non-zero and that's fine — the file removal below is
	// what actually matters for uninstall to succeed.
	_ = runCommand("launchctl", "unload", "-w", s.plistPath)
	if err := os.Remove(s.plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

func (s *launchdService) Start() error {
	return runCommand("launchctl", "load", "-w", s.plistPath)
}

func (s *launchdService) Stop() error {
	return runCommand("launchctl", "unload", "-w", s.plistPath)
}

func (s *launchdService) Status() (Status, error) {
	if _, err := os.Stat(s.plistPath); os.IsNotExist(err) {
		return Status{Installed: false, Detail: "not installed"}, nil
	} else if err != nil {
		return Status{}, err
	}

	out, err := exec.Command("launchctl", "list", launchdLabel).Output()
	if err != nil {
		// launchctl exits non-zero when the label isn't loaded.
		return Status{Installed: true, Running: false, Detail: "installed but not loaded"}, nil
	}

	pid := parseLaunchctlPID(string(out))
	if pid == "" || pid == "0" {
		return Status{Installed: true, Running: false, Detail: "loaded but not running"}, nil
	}
	return Status{Installed: true, Running: true, Detail: "running (pid " + pid + ")"}, nil
}

// parseLaunchctlPID extracts the "PID" field from `launchctl list <label>`'s
// plist-like key-value output, e.g. a line reading `		"PID" = 1234;`. Returns
// "" if absent, and "0" (a real, meaningful launchctl value) when the job is
// loaded but not currently running.
func parseLaunchctlPID(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, `"PID"`) {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		val = strings.TrimSuffix(val, ";")
		if _, err := strconv.Atoi(val); err != nil {
			continue
		}
		return val
	}
	return ""
}
