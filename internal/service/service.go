// Package service manages infrapal-token-advisor's background collector as a per-user OS
// service: a launchd agent on macOS, a systemd user unit on Linux. Neither
// requires root — both are installed entirely inside the user's home
// directory, matching infrapal-token-advisor's single-user, no-elevated-privileges design.
package service

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ErrUnsupported is returned by New on platforms with no supported service
// manager (anything but macOS/Linux). Callers should fall back to printing
// manual instructions rather than failing outright.
var ErrUnsupported = errors.New("service: no supported service manager on this platform")

// Status reports whether the service is installed (its unit/plist file
// exists) and, if so, whether it's currently running.
type Status struct {
	Installed bool
	Running   bool
	Detail    string // human-readable state, e.g. "running (pid 1234)", "not loaded", "failed"
}

// Service manages infrapal-token-advisor's own background collector process.
type Service interface {
	// Install writes the service definition and starts it immediately.
	Install() error
	// Uninstall stops the service and removes its definition.
	Uninstall() error
	Start() error
	Stop() error
	Status() (Status, error)
	// CheckAutostart returns nil if the service will come back after a
	// restart, or an error saying why it won't.
	CheckAutostart() error
}

// checkExecPath refuses a binary that won't outlive this command. `go run`
// deletes its binary on exit, leaving a service that can never start.
func checkExecPath(path string) error {
	if strings.Contains(path, "/go-build") {
		return fmt.Errorf("can't install a service for a temporary `go run` binary (%s): run `go build ./cmd/token-advisor`, then run init from that binary", path)
	}
	return nil
}

// Paths bundles the filesystem locations a Service implementation needs:
// the infrapal-token-advisor binary to invoke, and where to send its stdout/stderr since a
// backgrounded service has no terminal to write to.
type Paths struct {
	ExecPath  string
	StdoutLog string
	StderrLog string
	Home      string
}

// New returns the Service implementation for the current platform.
func New(p Paths) (Service, error) {
	return newForGOOS(runtime.GOOS, p)
}

// newForGOOS is New with the platform passed explicitly, so platform
// dispatch is testable without needing to run on every OS.
func newForGOOS(goos string, p Paths) (Service, error) {
	switch goos {
	case "darwin":
		return &launchdService{p: p, plistPath: launchdPlistPath(p.Home)}, nil
	case "linux":
		return &systemdService{p: p, unitPath: systemdUnitPath(p.Home)}, nil
	default:
		return nil, ErrUnsupported
	}
}

// runCommand runs an external command and folds its combined output into
// the error, since launchctl/systemctl failures are otherwise opaque exit
// codes.
func runCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(out))
		if detail == "" {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, detail)
	}
	return nil
}
