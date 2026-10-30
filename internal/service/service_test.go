package service

import (
	"strings"
	"testing"
)

func TestNewForGOOS_Dispatch(t *testing.T) {
	p := Paths{ExecPath: "/usr/local/bin/token-advisor", Home: "/home/u"}

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
		ExecPath:  "/usr/local/bin/token-advisor",
		StdoutLog: "/home/u/.infrapal-token-advisor/collector.log",
		StderrLog: "/home/u/.infrapal-token-advisor/collector.err.log",
		Home:      "/home/u",
	}
	xml := string(launchdPlistXML(p))

	for _, want := range []string{
		launchdLabel,
		"<string>/usr/local/bin/token-advisor</string>",
		"<string>start</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<key>KeepAlive</key>",
		"/home/u/.infrapal-token-advisor/collector.log",
		"/home/u/.infrapal-token-advisor/collector.err.log",
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("plist XML missing %q\n---\n%s", want, xml)
		}
	}
}

func TestSystemdUnitFile_ContainsExecAndRestart(t *testing.T) {
	p := Paths{
		ExecPath:  "/usr/local/bin/token-advisor",
		StdoutLog: "/home/u/.infrapal-token-advisor/collector.log",
		StderrLog: "/home/u/.infrapal-token-advisor/collector.err.log",
	}
	unit := string(systemdUnitFile(p))

	for _, want := range []string{
		"ExecStart=/usr/local/bin/token-advisor start",
		"Restart=always",
		"WantedBy=default.target",
		"/home/u/.infrapal-token-advisor/collector.log",
		"/home/u/.infrapal-token-advisor/collector.err.log",
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
		{"running", "{\n\t\"PID\" = 4242;\n\t\"Label\" = \"com.infrapal.token-advisor\";\n}", "4242"},
		{"loaded-not-running", "{\n\t\"PID\" = 0;\n\t\"LastExitStatus\" = 0;\n}", "0"},
		{"no-pid-field", "{\n\t\"Label\" = \"com.infrapal.token-advisor\";\n}", ""},
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
	if got, want := launchdPlistPath("/home/u"), "/home/u/Library/LaunchAgents/com.infrapal.token-advisor.plist"; got != want {
		t.Errorf("launchdPlistPath = %q, want %q", got, want)
	}
	if got, want := systemdUnitPath("/home/u"), "/home/u/.config/systemd/user/infrapal-token-advisor.service"; got != want {
		t.Errorf("systemdUnitPath = %q, want %q", got, want)
	}
}

func TestParseLinger(t *testing.T) {
	for out, want := range map[string]bool{"Linger=yes\n": true, "Linger=no\n": false, "": false} {
		if got := parseLinger(out); got != want {
			t.Errorf("parseLinger(%q) = %v, want %v", out, got, want)
		}
	}
}

func TestPlistExecPath(t *testing.T) {
	p := Paths{ExecPath: "/Users/a&b/bin/token-advisor"}
	if got := plistExecPath(string(launchdPlistXML(p))); got != p.ExecPath {
		t.Errorf("plistExecPath = %q, want %q", got, p.ExecPath)
	}
	if got := plistExecPath("<plist></plist>"); got != "" {
		t.Errorf("plistExecPath of an empty plist = %q, want empty", got)
	}
}

func TestUnitExecPath(t *testing.T) {
	p := Paths{ExecPath: "/usr/local/bin/token-advisor"}
	if got := unitExecPath(string(systemdUnitFile(p))); got != p.ExecPath {
		t.Errorf("unitExecPath = %q, want %q", got, p.ExecPath)
	}
}

func TestLaunchdDisabled(t *testing.T) {
	cases := map[string]bool{
		`"com.infrapal.token-advisor" => disabled`: true,
		`"com.infrapal.token-advisor" => true`:     true,
		`"com.infrapal.token-advisor" => enabled`:  false,
		`"com.other.job" => disabled`:              false,
		"":                                         false,
	}
	for out, want := range cases {
		if got := launchdDisabled(out); got != want {
			t.Errorf("launchdDisabled(%q) = %v, want %v", out, got, want)
		}
	}
}

func TestCheckExecPath(t *testing.T) {
	if err := checkExecPath("/var/folders/x/T/go-build123/b001/exe/token-advisor"); err == nil {
		t.Error("checkExecPath accepted a go run binary")
	}
	if err := checkExecPath("/usr/local/bin/token-advisor"); err != nil {
		t.Errorf("checkExecPath: %v", err)
	}
}
