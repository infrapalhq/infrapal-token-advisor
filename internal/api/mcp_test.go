package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

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

func TestSafeOverviewFor(t *testing.T) {
	db := seedDB(t)
	stmts := []string{
		`UPDATE sessions SET project_dir = '/home/secret-project'`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, full_command, result_size_bytes)
			VALUES ('r1', 'p1', 's1', 2000, 'Bash', 'cat /secret/path', 400),
			       ('r2', 'p1', 's1', 2001, 'Bash', 'cat /secret/path', 400)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	o, err := SafeOverviewFor(context.Background(), db, "all")
	if err != nil {
		t.Fatal(err)
	}
	if o.Range != "all" || o.SpendUSD != 0.1 || o.Sessions != 1 || len(o.ByDay) != 1 || len(o.Models) != 1 {
		t.Errorf("unexpected totals: %+v", o)
	}
	if o.Models[0].Model != "claude-3-opus" || o.Models[0].Share != 1 {
		t.Errorf("models = %+v", o.Models)
	}
	if o.OpenFindings == 0 || len(o.TopFindings) == 0 || len(o.TopFindings) > maxOverviewFindings {
		t.Errorf("want between 1 and %d findings, got %d of %d", maxOverviewFindings, len(o.TopFindings), o.OpenFindings)
	}
	encoded, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"/secret/path", "secret-project"} {
		if strings.Contains(string(encoded), leak) {
			t.Errorf("overview leaked %q: %s", leak, encoded)
		}
	}

	if _, err := SafeOverviewFor(context.Background(), db, "nonsense"); err == nil {
		t.Error("bad range: want an error")
	}
}
