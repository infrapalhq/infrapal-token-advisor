package api

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// seedDB builds a temp SQLite DB with one session ("s1"), one interaction
// ("p1") and its assistant response, an api_request, a tool_call, a span,
// an MCP server connection, and the metric_points needed for
// v_session_summary to have non-zero aggregates. Every handler test in this
// package reads from this one fixture.
func seedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "ccotel.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`INSERT INTO sessions (session_id, started_at, ended_at, start_type, app_version, terminal_type)
			VALUES ('s1', 1000, 5000, 'cli', '2.1.212', 'iterm')`,
		`INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text, command_name)
			VALUES ('p1', 's1', 1, 1000, 200, 'hello world', NULL)`,
		`INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, ts, model, response_text)
			VALUES ('m1', 'p1', 's1', 1100, 'claude-3-opus', 'hi there')`,
		`INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, ttft_ms, duration_ms, success)
			VALUES ('req-1', 'p1', 's1', 1000, 'claude-3-opus', 100000, 50, 100, 1)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
			VALUES ('tu-1', 'p1', 's1', 1050, 'Bash', 'accept', 1)`,
		`INSERT INTO spans (span_id, trace_id, span_type, name, start_ns, end_ns, session_id, prompt_id)
			VALUES ('sp-1', 't-1', 'tool', 'claude_code.tool', 1000000000, 2000000000, 's1', 'p1')`,
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

// newTestServer builds a Server over db and wraps it in an httptest.Server.
// Cleans itself up via t.Cleanup.
func newTestServer(t *testing.T, db *sql.DB) *httptest.Server {
	t.Helper()
	srv := NewServer("127.0.0.1:0", db, nil)
	ts := httptest.NewServer(srv.http.Handler)
	t.Cleanup(ts.Close)
	return ts
}
