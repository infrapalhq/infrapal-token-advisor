package service

import (
	"strings"
	"testing"
)

func TestNewForGOOS_Dispatch(t *testing.T) {
	p := Paths{ExecPath: "/usr/local/bin/ccotel", Home: "/home/u"}

	darwin, err := newForGOOS("darwin", p)
	if err != nil {
		t.Fatalf("darwin: %v", err)
	}
	if _, ok := darwin.(*launchdService); !ok {
		t.Errorf("darwin: got %T, want *launchdService", darwin)
	}

	linux, err := newForGOOS("linux", p)
	if err != nil {
		t.Fatalf("linux: %v", err)
	}
	if _, ok := linux.(*systemdService); !ok {
		t.Errorf("linux: got %T, want *systemdService", linux)
	}

	if _, err := newForGOOS("windows", p); err != ErrUnsupported {
		t.Errorf("windows: err = %v, want ErrUnsupported", err)
	}
}

func TestLaunchdPlistXML_ContainsExecAndLogPaths(t *testing.T) {
	p := Paths{
		ExecPath:  "/usr/local/bin/ccotel",
		StdoutLog: "/home/u/.ccotel/collector.log",
		StderrLog: "/home/u/.ccotel/collector.err.log",
		Home:      "/home/u",
	}
	xml := string(launchdPlistXML(p))

	for _, want := range []string{
		launchdLabel,
		"<string>/usr/local/bin/ccotel</string>",
		"<string>start</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"/home/u/.ccotel/collector.log",
		"/home/u/.ccotel/collector.err.log",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("plist XML missing %q\n---\n%s", want, xml)
		}
	}
}

func TestSystemdUnitFile_ContainsExecAndRestart(t *testing.T) {
	p := Paths{
		ExecPath:  "/usr/local/bin/ccotel",
		StdoutLog: "/home/u/.ccotel/collector.log",
		StderrLog: "/home/u/.ccotel/collector.err.log",
	}
	unit := string(systemdUnitFile(p))

	for _, want := range []string{
		"ExecStart=/usr/local/bin/ccotel start",
		"Restart=always",
		"WantedBy=default.target",
		"/home/u/.ccotel/collector.log",
		"/home/u/.ccotel/collector.err.log",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit file missing %q\n---\n%s", want, unit)
		}
	}
}

func TestParseLaunchctlPID(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"running", "{\n\t\"PID\" = 4242;\n\t\"Label\" = \"com.ccotel.collector\";\n}", "4242"},
		{"loaded-not-running", "{\n\t\"PID\" = 0;\n\t\"LastExitStatus\" = 0;\n}", "0"},
		{"no-pid-field", "{\n\t\"Label\" = \"com.ccotel.collector\";\n}", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseLaunchctlPID(tc.in); got != tc.want {
				t.Errorf("parseLaunchctlPID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLaunchdPlistPath_AndSystemdUnitPath(t *testing.T) {
	if got, want := launchdPlistPath("/home/u"), "/home/u/Library/LaunchAgents/com.ccotel.collector.plist"; got != want {
		t.Errorf("launchdPlistPath = %q, want %q", got, want)
	}
	if got, want := systemdUnitPath("/home/u"), "/home/u/.config/systemd/user/ccotel.service"; got != want {
		t.Errorf("systemdUnitPath = %q, want %q", got, want)
	}
}
