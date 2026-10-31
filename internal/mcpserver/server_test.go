package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

const secret = "SECRET-sk-ant-do-not-leak"

// newTestServer seeds a database with content in every sensitive column and
// returns a Server over a read-only connection to it.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token-advisor.db")
	rw, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	defer rw.Close()
	for _, stmt := range []string{
		`INSERT INTO sessions (session_id, started_at, ended_at, user_email, project_dir)
			VALUES ('s1', 1000, 5000, '` + secret + `', '/home/` + secret + `')`,
		`INSERT INTO interactions (prompt_id, session_id, seq, started_at, prompt_text)
			VALUES ('p1', 's1', 1, 1000, '` + secret + `')`,
		`INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, error, success)
			VALUES ('r1', 'p1', 's1', 1000, 'claude-opus', 2500000, '` + secret + `', 1)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision_source, success, file_path, full_command, tool_input, error)
			VALUES ('t1', 'p1', 's1', 1000, 'Bash', 'user_temporary', 1, '/` + secret + `', 'echo ` + secret + `', '` + secret + `', '` + secret + `')`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision_source, success, full_command, result_size_bytes)
			VALUES ('t2', 'p1', 's1', 1001, 'Bash', 'user_temporary', 1, 'echo ` + secret + `', 100),
			       ('t3', 'p1', 's1', 1002, 'Bash', 'user_temporary', 1, 'echo ` + secret + `', 100)`,
	} {
		if _, err := rw.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	db, err := store.OpenReadOnly(path)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "test")
}

// call runs one tool through the full JSON-RPC path and returns its text.
func call(t *testing.T, s *Server, name string, args map[string]any) (string, bool) {
	t.Helper()
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": name, "arguments": args},
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := s.Serve(context.Background(), bytes.NewReader(append(req, '\n')), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	var resp struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *rpcError `json:"error"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if resp.Error != nil {
		t.Fatalf("rpc error: %+v", resp.Error)
	}
	return resp.Result.Content[0].Text, resp.Result.IsError
}

func TestProtocol(t *testing.T) {
	s := newTestServer(t)
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		``,
		`{"jsonrpc":"2.0","id":"two","method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"nope"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`,
	}, "\n") // the last line deliberately has no trailing newline
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 7 {
		t.Fatalf("want 7 responses (no reply to the notification or blank line), got %d:\n%s", len(lines), out.String())
	}
	for i, want := range []string{
		`"protocolVersion":"2025-06-18"`,
		`"id":"two","result":{}`,
		`"get_findings"`,
		`-32601`,
		`-32700`,
		`-32602`,
		`"protocolVersion":"` + protocolVersions[0] + `"`,
	} {
		if !strings.Contains(lines[i], want) {
			t.Errorf("response %d: want %s in %s", i, want, lines[i])
		}
	}
}

func TestGetSchema_HidesViewsWithContent(t *testing.T) {
	s := newTestServer(t)
	text, isError := call(t, s, "get_schema", nil)
	if isError {
		t.Fatalf("get_schema: %s", text)
	}
	var out struct {
		Views map[string][]string `json:"views"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out.Views["v_user_waits"]; ok {
		t.Error("v_user_waits exposes commands and paths and must be hidden")
	}
	for _, view := range []string{"v_prompt_efficiency", "v_session_summary", "v_api_request_detail", "v_tool_call_detail"} {
		if len(out.Views[view]) == 0 {
			t.Errorf("%s missing from schema", view)
		}
	}
}

// TestViewColumnsAreUsable guards two things as views are added: no exposed
// column is named like a blocked table, which checkQuery would refuse, and
// exactly the expected views are withheld, so that a new view with renamed
// content gets looked at rather than silently exposed or hidden.
func TestViewColumnsAreUsable(t *testing.T) {
	s := newTestServer(t)
	views, blocked, err := s.viewAccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for view, columns := range views {
		for _, c := range columns {
			if err := checkQuery("SELECT "+c+" FROM "+view, blocked); err != nil {
				t.Errorf("%s.%s: %v", view, c, err)
			}
		}
	}
	var hidden []string
	for name := range blocked {
		if strings.HasPrefix(name, "v_") {
			hidden = append(hidden, name)
		}
	}
	if len(hidden) != 1 || hidden[0] != "v_user_waits" {
		t.Errorf("withheld views = %v, want only v_user_waits", hidden)
	}
}

func TestRunSQL_ReadsViews(t *testing.T) {
	s := newTestServer(t)
	text, isError := call(t, s, "run_sql", map[string]any{
		"query": `WITH r AS (SELECT session_id, SUM(cost_usd) AS usd FROM v_api_request_detail GROUP BY session_id)
			SELECT session_id, usd FROM r; -- trailing comment`,
	})
	if isError {
		t.Fatalf("run_sql: %s", text)
	}
	if !strings.Contains(text, `"rows":[["s1",2.5]]`) {
		t.Errorf("unexpected result: %s", text)
	}
}

func TestRunSQL_NoContentReachable(t *testing.T) {
	s := newTestServer(t)
	for _, query := range []string{
		`SELECT prompt_text FROM interactions`,
		`select prompt_text from INTERACTIONS`,
		`SELECT prompt_text FROM main.interactions`,
		`SELECT prompt_text FROM "interactions"`,
		`SELECT prompt_text FROM [interactions]`,
		"SELECT prompt_text FROM `interactions`",
		`SELECT prompt_text FROM 'interactions'`,
		`SELECT prompt_text FROM/**/interactions`,
		"SELECT prompt_text FROM --x\n interactions",
		`SELECT 1 WHERE EXISTS (SELECT 1 FROM interactions WHERE prompt_text LIKE 'S%')`,
		`SELECT session_id FROM v_session_summary WHERE (SELECT user_email FROM sessions) LIKE 'S%'`,
		`WITH x AS (SELECT * FROM tool_calls) SELECT full_command FROM x`,
		`SELECT 1 UNION SELECT body FROM events_raw`,
		`SELECT * FROM v_user_waits`,
		`SELECT full_command FROM "V_USER_WAITS"`,
		`SELECT * FROM interactions_fts`,
		`SELECT * FROM interactions_fts_content`,
		`SELECT sql FROM sqlite_master`,
		`SELECT sql FROM sqlite_schema`,
		`SELECT * FROM pragma_table_info('interactions')`,
		`SELECT * FROM pragma_database_list`,
		`SELECT * FROM dbstat`,
		`PRAGMA table_info(interactions)`,
		`ATTACH DATABASE '/tmp/x.db' AS x`,
		`EXPLAIN SELECT 1`,
		`SELECT 1; SELECT prompt_text FROM interactions`,
		`SELECT 1; DROP VIEW v_session_summary`,
		`DELETE FROM interactions`,
		`/* SELECT */ DELETE FROM ui_state`,
		"SELECT 'unterminated",
		"SELECT 1\x00; SELECT prompt_text FROM interactions",
		`;`,
		``,
		`-- only a comment`,
	} {
		text, isError := call(t, s, "run_sql", map[string]any{"query": query})
		if !isError {
			t.Errorf("query was allowed: %q returned %s", query, text)
		}
		if strings.Contains(text, secret) {
			t.Errorf("query leaked content: %q returned %s", query, text)
		}
	}
}

// TestRunSQL_ConnectionRejectsWrites checks the second layer: even a statement
// that gets past checkQuery can't change anything.
func TestRunSQL_ConnectionRejectsWrites(t *testing.T) {
	s := newTestServer(t)
	text, isError := call(t, s, "run_sql", map[string]any{"query": `WITH c AS (SELECT 1) DELETE FROM v_session_summary`})
	if !isError {
		t.Errorf("write was allowed: %s", text)
	}
	if _, err := s.db.Exec(`INSERT INTO ui_state (key, value) VALUES ('x', 'y')`); err == nil {
		t.Error("read-only connection accepted a write")
	}
}

func TestRunSQL_AllowsNamesInsideComments(t *testing.T) {
	s := newTestServer(t)
	text, isError := call(t, s, "run_sql", map[string]any{"query": `SELECT 1 /* interactions */ -- sessions`})
	if isError {
		t.Errorf("comment should not trigger the block: %s", text)
	}
}

func TestRunSQL_CapsRows(t *testing.T) {
	s := newTestServer(t)
	text, isError := call(t, s, "run_sql", map[string]any{
		"query": `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 1000) SELECT x FROM c`,
	})
	if isError {
		t.Fatalf("run_sql: %s", text)
	}
	var out struct {
		RowCount  int  `json:"row_count"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	if out.RowCount != maxRows || !out.Truncated {
		t.Errorf("row_count=%d truncated=%v, want %d and true", out.RowCount, out.Truncated, maxRows)
	}
}

func TestRunSQL_CapsBytes(t *testing.T) {
	s := newTestServer(t)
	text, isError := call(t, s, "run_sql", map[string]any{
		"query": `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 400) SELECT x, printf('%.*c', 5000, 'a') FROM c`,
	})
	if isError {
		t.Fatalf("run_sql: %s", text)
	}
	if len(text) > maxBytes+4096 || !strings.Contains(text, `"truncated":true`) {
		t.Errorf("result is %d bytes, want it capped and marked truncated", len(text))
	}
	if strings.Contains(text, strings.Repeat("a", maxCellRune+1)) {
		t.Error("cell was not capped")
	}
}

func TestRunSQL_TimesOut(t *testing.T) {
	s := newTestServer(t)
	s.queryTimeout = 100 * time.Millisecond
	start := time.Now()
	text, isError := call(t, s, "run_sql", map[string]any{
		"query": `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT COUNT(*) FROM c`,
	})
	if !isError || !strings.Contains(text, "timed out") {
		t.Errorf("want a timeout, got %s", text)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("took %s to stop", elapsed)
	}
	if text, isError := call(t, s, "run_sql", map[string]any{"query": `SELECT 1`}); isError {
		t.Errorf("connection unusable after timeout: %s", text)
	}
}

func TestGetFindings_WithholdsContent(t *testing.T) {
	s := newTestServer(t)
	text, isError := call(t, s, "get_findings", map[string]any{"since": "all"})
	if isError {
		t.Fatalf("get_findings: %s", text)
	}
	if !strings.Contains(text, `"kind":"repeat_cmd"`) && !strings.Contains(text, `"kind":"repeat_approval"`) {
		t.Fatalf("seed should produce a command finding: %s", text)
	}
	if strings.Contains(text, "SECRET") || strings.Contains(text, "echo") {
		t.Errorf("findings leaked content: %s", text)
	}
}

func TestGetFindings_RejectsBadRange(t *testing.T) {
	s := newTestServer(t)
	if text, isError := call(t, s, "get_findings", map[string]any{"since": "nonsense"}); !isError {
		t.Errorf("want an error, got %s", text)
	}
}
