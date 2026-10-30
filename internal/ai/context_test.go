package ai

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// sensitiveFixtureStrings are values seeded below that must NEVER appear in
// a BuildContext payload — they stand in for raw prompt/response/tool
// content, which this package must never send to Anthropic.
var sensitiveFixtureStrings = []string{
	"how do I center a div in css please",
	"use flexbox and a wrapper div",
	"rm -rf /some/secret/path --force",
	"/home/user/private-project/config.yaml",
}

// seedDB builds a temp SQLite DB with one session/interaction/tool_call
// containing the sensitive fixture strings above, plus enough rows for
// every view BuildContext reads to have at least one row.
func seedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "token-advisor.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`INSERT INTO sessions (session_id, started_at, ended_at, start_type, app_version, terminal_type)
			VALUES ('s1', 1000, 5000, 'cli', '2.1.212', 'iterm')`,
		`INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text, command_name)
			VALUES ('p1', 's1', 1, 1000, 200, 'how do I center a div in css please', NULL)`,
		`INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, ts, model, response_text)
			VALUES ('m1', 'p1', 's1', 1100, 'claude-3-opus', 'use flexbox and a wrapper div')`,
		`INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, input_tokens, output_tokens, ttft_ms, duration_ms, success)
			VALUES ('req-1', 'p1', 's1', 1000, 'claude-3-opus', 100000, 100, 200, 50, 100, 1)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success,
			full_command, input_size_bytes, result_size_bytes, blocked_on_user_ms)
			VALUES ('tu-1', 'p1', 's1', 1050, 'Bash', 'accept', 'user_temporary', 1,
			'rm -rf /some/secret/path --force', 512, 2048, 100)`,
		`INSERT INTO mcp_server_connections (ts, session_id, server_name, status)
			VALUES (1000, 's1', 'github', 'connected')`,
		`INSERT INTO metric_points (ts, name, value, session_id) VALUES (1000, 'claude_code.cost.usage', 0.1, 's1')`,
		`INSERT INTO metric_points (ts, name, value, session_id) VALUES (1000, 'claude_code.token.usage', 300, 's1')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed exec %q: %v", stmt, err)
		}
	}
	return db
}

func TestBuildContext_NeverLeaksRawContent(t *testing.T) {
	db := seedDB(t)
	c, err := BuildContext(context.Background(), db, "", 0, false, nil)
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal context: %v", err)
	}
	payload := string(data)
	for _, sensitive := range sensitiveFixtureStrings {
		if strings.Contains(payload, sensitive) {
			t.Errorf("context payload leaked raw content %q", sensitive)
		}
	}
}

func TestBuildContext_RowCaps(t *testing.T) {
	db := seedDB(t)
	for i := 0; i < maxToolRows+5; i++ {
		mustExecAI(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
			VALUES (?, 'p1', 's1', ?, ?, 'accept', 1)`, "tu-cap-"+strconv.Itoa(i), 2000+i, "Tool"+strconv.Itoa(i))
	}

	c, err := BuildContext(context.Background(), db, "", 0, false, nil)
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if len(c.Tools) > maxToolRows {
		t.Errorf("len(Tools) = %d, want <= %d", len(c.Tools), maxToolRows)
	}
}

func TestBuildContext_SinceFilterExcludesOldRows(t *testing.T) {
	db := seedDB(t) // p1 sits at started_at=1000 (1970) — ancient.
	c, err := BuildContext(context.Background(), db, "1h", 9999999999999, true, nil)
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	if len(c.TopPrompts) != 0 {
		t.Errorf("expected no prompts within a recent cutoff, got %+v", c.TopPrompts)
	}
}

func TestContextHash_DeterministicAndSensitiveToChange(t *testing.T) {
	db := seedDB(t)
	c1, err := BuildContext(context.Background(), db, "", 0, false, nil)
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	h1, err := c1.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	h1b, err := c1.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 != h1b {
		t.Errorf("same context hashed differently: %q vs %q", h1, h1b)
	}

	mustExecAI(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-new', 'p1', 's1', 3000, 'Read', 'accept', 1)`)
	c2, err := BuildContext(context.Background(), db, "", 0, false, nil)
	if err != nil {
		t.Fatalf("BuildContext: %v", err)
	}
	h2, err := c2.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	if h1 == h2 {
		t.Errorf("expected hash to change after a data change, got the same hash %q", h1)
	}
}

func mustExecAI(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
