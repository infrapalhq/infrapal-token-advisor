package service

import (
	"encoding/xml"
	"strings"
	"testing"
)

// The unit file has to give back exactly the path it was given, whatever the
// path holds: doctor reads it to check the binary still exists.
func FuzzSystemdExecPathRoundTrip(f *testing.F) {
	for _, seed := range []string{"/usr/local/bin/token-advisor", "/home/a b/ta", `/x/"q"/\b/100%/$HOME/ta`, "", " start", `" start`} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, path string) {
		if strings.ContainsAny(path, "\n\r") {
			t.Skip("a unit file can't hold a line break in a value")
		}
		if got := unitExecPath(string(systemdUnitFile(Paths{ExecPath: path}))); got != path {
			t.Errorf("unitExecPath = %q, want %q", got, path)
		}
	})
}

func FuzzPlistExecPathRoundTrip(f *testing.F) {
	for _, seed := range []string{"/usr/local/bin/token-advisor", "/Users/a&b/ta", "/x/<y>/ta", ""} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, path string) {
		var escaped strings.Builder
		_ = xml.EscapeText(&escaped, []byte(path))
		if strings.ContainsRune(escaped.String(), '\uFFFD') {
			t.Skip("XML has no way to write this character")
		}
		if got := plistExecPath(string(launchdPlistXML(Paths{ExecPath: path}))); got != path {
			t.Errorf("plistExecPath = %q, want %q", got, path)
		}
	})
}
