package claudecode

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/agenttest"
)

var (
	_ agent.Agent   = (*Agent)(nil)
	_ agent.Advisor = (*Agent)(nil)
	_ agent.MCPHost = (*Agent)(nil)
)

func TestToolCategory(t *testing.T) {
	a := New(t.TempDir())
	cases := map[string]agent.ToolCategory{
		"Read":                  agent.ToolRead,
		"Grep":                  agent.ToolSearch,
		"Glob":                  agent.ToolSearch,
		"Edit":                  agent.ToolEdit,
		"Write":                 agent.ToolEdit,
		"MultiEdit":             agent.ToolEdit,
		"NotebookEdit":          agent.ToolEdit,
		"Bash":                  agent.ToolShell,
		"mcp__github__get_repo": agent.ToolMCP,
		"AskUserQuestion":       agent.ToolOther,
		"":                      agent.ToolOther,
	}
	for name, want := range cases {
		if got := a.ToolCategory(name); got != want {
			t.Errorf("ToolCategory(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestConnect_KeepsTheFirstBackupAndRestoresIt(t *testing.T) {
	home := t.TempDir()
	a := New(home)
	original := `{"theme":"dark"}`
	if err := os.MkdirAll(filepath.Dir(a.settingsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.settingsPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	conn, err := a.Connect(&out, agent.ConnectOptions{OTLPPort: 4318})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if conn.BackupPath == "" {
		t.Fatal("Connect should record a backup of the original file")
	}

	again, err := a.Connect(&out, agent.ConnectOptions{OTLPPort: 5000, Previous: conn})
	if err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	if again.BackupPath != conn.BackupPath {
		t.Errorf("BackupPath = %q, want the first %q", again.BackupPath, conn.BackupPath)
	}

	if err := a.Disconnect(&out, again); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	got, err := os.ReadFile(a.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("settings.json = %s, want the original back", got)
	}
}

func TestConnect_RawBodies(t *testing.T) {
	a := New(t.TempDir())
	var out bytes.Buffer
	if _, err := a.Connect(&out, agent.ConnectOptions{OTLPPort: 4318, RawBodiesDir: "/data/bodies"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !strings.Contains(out.String(), "warning: --raw-bodies") {
		t.Errorf("output should warn about raw bodies:\n%s", out.String())
	}
	data, err := os.ReadFile(a.settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "file:/data/bodies") {
		t.Errorf("settings.json should point at the bodies dir:\n%s", data)
	}
}

func TestDisconnect_WithNothingToRestore(t *testing.T) {
	a := New(t.TempDir())
	for name, conn := range map[string]agent.Connection{
		"no backup":      {},
		"backup missing": {BackupPath: filepath.Join(t.TempDir(), "gone")},
	} {
		var out bytes.Buffer
		if err := a.Disconnect(&out, conn); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if !strings.Contains(out.String(), "left as-is") {
			t.Errorf("%s: output = %q", name, out.String())
		}
	}
}

func TestCheck_Settings(t *testing.T) {
	a := New(t.TempDir())
	checks, err := a.Check(4318)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if checks[0].Pass || !strings.Contains(checks[0].Fix, "does not exist") {
		t.Errorf("before Connect: %+v", checks[0])
	}

	var out bytes.Buffer
	if _, err := a.Connect(&out, agent.ConnectOptions{OTLPPort: 4318}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if checks, _ = a.Check(4318); !checks[0].Pass {
		t.Errorf("after Connect: %+v", checks[0])
	}
	if checks, _ = a.Check(9999); checks[0].Pass || !strings.Contains(checks[0].Fix, "ENDPOINT") {
		t.Errorf("wrong port: %+v", checks[0])
	}
}

func TestCheckShellEnv(t *testing.T) {
	if c := checkShellEnv([]string{"PATH=/bin"}); !c.Pass {
		t.Errorf("clean shell: %+v", c)
	}
	c := checkShellEnv([]string{"PATH=/bin", "OTEL_SERVICE_NAME=x", "CLAUDE_CODE_USE_BEDROCK=1"})
	if c.Pass || !strings.Contains(c.Fix, "OTEL_SERVICE_NAME, CLAUDE_CODE_USE_BEDROCK") {
		t.Errorf("conflicting shell: %+v", c)
	}
}

func TestManagedSettingsPath(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		if managedSettingsPath(goos) == "" {
			t.Errorf("no managed settings path for %s", goos)
		}
	}
	if c := checkManagedSettings("windows"); !c.Skipped {
		t.Errorf("windows: %+v, want skipped", c)
	}
}

func TestPremiumModel(t *testing.T) {
	a := New(t.TempDir())
	if !a.PremiumModel("claude-opus-5-5") {
		t.Error("Opus should be premium")
	}
	if a.PremiumModel("claude-sonnet-5-5") {
		t.Error("Sonnet shouldn't be premium")
	}
}

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/local/bin/tool":   "/usr/local/bin/tool",
		"/Users/a b/tool":       "'/Users/a b/tool'",
		"/Users/o'neil/tool":    `'/Users/o'\''neil/tool'`,
		"/tmp/$(touch pwned)/x": "'/tmp/$(touch pwned)/x'",
	} {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMCPRegistered(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	for _, tc := range []struct {
		name string
		path string
		want bool
	}{
		{"missing file", filepath.Join(dir, "absent.json"), false},
		{"invalid json", write("bad.json", "{"), false},
		{"other servers", write("other.json", `{"mcpServers":{"github":{}}}`), false},
		{"registered", write("yes.json", `{"mcpServers":{"token-advisor":{"command":"x"}}}`), true},
	} {
		if got := mcpRegistered(tc.path, "token-advisor"); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestAdvice_MatchesClaudeCodesFormats(t *testing.T) {
	a := New(t.TempDir())
	if got := a.ShellPattern("git status"); got != "Bash(git status:*)" {
		t.Errorf("ShellPattern = %q", got)
	}
	if f := a.AllowFix("Bash(ls:*)"); !strings.Contains(f.Text, `"Bash(ls:*)"`) || f.Where != whereSettings {
		t.Errorf("AllowFix = %+v", f)
	}
	if f := a.RepeatCommandFix("go test", "go"); !strings.Contains(f.Text, "after editing .go files") {
		t.Errorf("RepeatCommandFix = %+v", f)
	}
	if f := a.RepeatCommandFix("go test", ""); !strings.Contains(f.Text, "unless something it depends on") {
		t.Errorf("RepeatCommandFix with no edits = %+v", f)
	}
	if f := a.RemoveMCPFix("gh"); f.Text != `claude mcp remove "gh"` || f.Where != whereTerminal {
		t.Errorf("RemoveMCPFix = %+v", f)
	}
}

func TestMCPSetup(t *testing.T) {
	a := New(t.TempDir())
	s := a.MCPSetup("token-advisor", "/my tools/ta")
	if s.Enabled {
		t.Error("nothing is registered yet")
	}
	if want := "claude mcp add --scope user token-advisor -- '/my tools/ta' mcp"; s.Command != want {
		t.Errorf("Command = %q, want %q", s.Command, want)
	}
}

func TestContract(t *testing.T) {
	agenttest.Run(t, agenttest.Suite{
		New:          func(home string) agent.Agent { return New(home) },
		NativeIngest: true,
		Tools: map[string]agent.ToolCategory{
			"Read":                  agent.ToolRead,
			"Grep":                  agent.ToolSearch,
			"Edit":                  agent.ToolEdit,
			"Bash":                  agent.ToolShell,
			"mcp__github__get_repo": agent.ToolMCP,
		},
	})
}
