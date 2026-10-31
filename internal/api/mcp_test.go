package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
		{"registered", write("yes.json", `{"mcpServers":{"`+MCPServerName+`":{"command":"x"}}}`), true},
	} {
		if got := mcpRegistered(tc.path); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHandleMCPSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	ts := newTestServer(t, seedDB(t))
	resp, err := http.Get(ts.URL + "/api/mcp/setup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var body struct {
		Enabled bool   `json:"enabled"`
		Command string `json:"command"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Enabled {
		t.Error("enabled with no ~/.claude.json")
	}
	if !strings.HasPrefix(body.Command, "claude mcp add --scope user "+MCPServerName+" -- ") || !strings.HasSuffix(body.Command, " mcp") {
		t.Errorf("unexpected command: %s", body.Command)
	}
}

func TestSafeFindings_WithholdsCommands(t *testing.T) {
	db := seedDB(t)
	stmts := []string{
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, full_command, result_size_bytes)
			VALUES ('r1', 'p1', 's1', 2000, 'Bash', 'cat /secret/path', 400),
			       ('r2', 'p1', 's1', 2001, 'Bash', 'cat /secret/path', 400)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	findings, err := SafeFindings(context.Background(), db, "all")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(findings)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"kind":"repeat_cmd"`) {
		t.Fatalf("seed should produce repeat_cmd: %s", encoded)
	}
	if strings.Contains(string(encoded), "/secret/path") {
		t.Errorf("findings leaked a command: %s", encoded)
	}
}
