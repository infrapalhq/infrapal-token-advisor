package cli

import (
	"database/sql"
	"testing"
	"time"
)

// seedPurgeRow inserts one row into every table purgeTables (and the
// session-scoped tables it references) knows about, tagged with sessionID
// and tsMs, so tests can seed an "old"/"new" or "s1"/"s2" pair and assert
// exactly one side gets deleted.
func seedPurgeRow(t *testing.T, db *sql.DB, sessionID string, tsMs int64) {
	t.Helper()
	startNs := tsMs * int64(time.Millisecond/time.Nanosecond)
	stmts := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO events_raw (ts, session_id, event_name, body) VALUES (?, ?, 'x', '{}')`,
			[]any{tsMs, sessionID}},
		{`INSERT INTO sessions (session_id, started_at) VALUES (?, ?)`,
			[]any{sessionID, tsMs}},
		{`INSERT INTO interactions (prompt_id, session_id, seq, started_at) VALUES (?, ?, 1, ?)`,
			[]any{sessionID + "-p1", sessionID, tsMs}},
		{`INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, ts, model, response_text) VALUES (?, ?, ?, ?, 'm', 'r')`,
			[]any{sessionID + "-m1", sessionID + "-p1", sessionID, tsMs}},
		{`INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model) VALUES (?, ?, ?, ?, 'm')`,
			[]any{sessionID + "-req1", sessionID + "-p1", sessionID, tsMs}},
		{`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success) VALUES (?, ?, ?, ?, 'Bash', 'accept', 1)`,
			[]any{sessionID + "-tu1", sessionID + "-p1", sessionID, tsMs}},
		{`INSERT INTO spans (span_id, trace_id, span_type, name, start_ns, session_id, prompt_id) VALUES (?, ?, 'tool', 'claude_code.tool', ?, ?, ?)`,
			[]any{sessionID + "-sp1", sessionID + "-t1", startNs, sessionID, sessionID + "-p1"}},
		{`INSERT INTO metric_points (ts, name, value, session_id) VALUES (?, 'claude_code.cost.usage', 1.0, ?)`,
			[]any{tsMs, sessionID}},
		{`INSERT INTO ingest_errors (ts, signal, message) VALUES (?, 'logs', ?)`,
			[]any{tsMs, sessionID + "-err"}},
		{`INSERT INTO mcp_server_connections (ts, session_id, server_name, status) VALUES (?, ?, 'github', 'connected')`,
			[]any{tsMs, sessionID}},
	}
	for _, s := range stmts {
		if _, err := db.Exec(s.query, s.args...); err != nil {
			t.Fatalf("seed exec %q: %v", s.query, err)
		}
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// allPurgeableTables lists every table seedPurgeRow inserts into, for
// asserting row counts across all of them in one loop.
var allPurgeableTables = []string{
	"events_raw", "sessions", "interactions", "assistant_responses",
	"api_requests", "tool_calls", "spans", "metric_points",
	"ingest_errors", "mcp_server_connections",
}

func TestRunPurge_RequiresExactlyOneFlag(t *testing.T) {
	withFakeHome(t)
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	db.Close()

	if err := runPurge(nil); err == nil {
		t.Error("expected error with no flags, got nil")
	}
	if err := runPurge([]string{"--before", "1d", "--all"}); err == nil {
		t.Error("expected error with two flags, got nil")
	}
	if err := runPurge([]string{"--before", "1d", "--session", "s1", "--all"}); err == nil {
		t.Error("expected error with three flags, got nil")
	}
}

func TestRunPurge_BySession(t *testing.T) {
	withFakeHome(t)
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	now := time.Now().UnixMilli()
	seedPurgeRow(t, db, "s1", now)
	seedPurgeRow(t, db, "s2", now)
	if _, err := db.Exec(`INSERT INTO pending_interaction_spans (trace_id, span_id, duration_ms) VALUES ('t1', 'sp1', 100)`); err != nil {
		t.Fatalf("seed pending_interaction_spans: %v", err)
	}
	db.Close()

	if err := runPurge([]string{"--session", "s1"}); err != nil {
		t.Fatalf("runPurge: %v", err)
	}

	db, _, err = openDB()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	for _, table := range allPurgeableTables {
		want := 1 // s2's row always survives
		if table == "ingest_errors" {
			// ingest_errors has no session_id column, so --session never
			// touches it — both s1's and s2's rows survive.
			want = 2
		}
		if got := countRows(t, db, table); got != want {
			t.Errorf("%s: %d rows after --session s1, want %d", table, got, want)
		}
	}

	// staging tables (no session_id column) are untouched by --session —
	// they only clear on --all.
	if got := countRows(t, db, "pending_interaction_spans"); got != 1 {
		t.Errorf("pending_interaction_spans: %d rows after --session, want 1", got)
	}
}

func TestRunPurge_ByBefore(t *testing.T) {
	withFakeHome(t)
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	now := time.Now().UnixMilli()
	old := now - (10 * 24 * 60 * 60 * 1000) // 10 days ago
	seedPurgeRow(t, db, "old", old)
	seedPurgeRow(t, db, "new", now)
	db.Close()

	if err := runPurge([]string{"--before", "5d"}); err != nil {
		t.Fatalf("runPurge: %v", err)
	}

	db, _, err = openDB()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	for _, table := range allPurgeableTables {
		if got := countRows(t, db, table); got != 1 {
			t.Errorf("%s: %d rows after --before 5d, want 1 (only the recent row)", table, got)
		}
	}
	// Confirm the surviving row is the "new" one, not "old".
	var sessionID string
	if err := db.QueryRow(`SELECT session_id FROM sessions`).Scan(&sessionID); err != nil {
		t.Fatalf("query surviving session: %v", err)
	}
	if sessionID != "new" {
		t.Errorf("surviving session = %q, want %q", sessionID, "new")
	}
}

func TestRunPurge_All(t *testing.T) {
	withFakeHome(t)
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	seedPurgeRow(t, db, "s1", time.Now().UnixMilli())
	if _, err := db.Exec(`INSERT INTO pending_interaction_spans (trace_id, span_id, duration_ms) VALUES ('t1', 'sp1', 100)`); err != nil {
		t.Fatalf("seed pending_interaction_spans: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO pending_blocked_on_user_spans (parent_span_id, duration_ms) VALUES ('sp1', 100)`); err != nil {
		t.Fatalf("seed pending_blocked_on_user_spans: %v", err)
	}
	db.Close()

	if err := runPurge([]string{"--all"}); err != nil {
		t.Fatalf("runPurge: %v", err)
	}

	db, _, err = openDB()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	for _, table := range allPurgeableTables {
		if got := countRows(t, db, table); got != 0 {
			t.Errorf("%s: %d rows after --all, want 0", table, got)
		}
	}
	for _, table := range purgeOnlyOnAllTables {
		if got := countRows(t, db, table); got != 0 {
			t.Errorf("%s: %d rows after --all, want 0", table, got)
		}
	}
}

func TestRunPurge_BeforeLeavesStagingTablesAlone(t *testing.T) {
	withFakeHome(t)
	db, _, err := openDB()
	if err != nil {
		t.Fatalf("openDB: %v", err)
	}
	seedPurgeRow(t, db, "old", time.Now().UnixMilli()-(10*24*60*60*1000))
	if _, err := db.Exec(`INSERT INTO pending_interaction_spans (trace_id, span_id, duration_ms) VALUES ('t1', 'sp1', 100)`); err != nil {
		t.Fatalf("seed pending_interaction_spans: %v", err)
	}
	db.Close()

	if err := runPurge([]string{"--before", "1d"}); err != nil {
		t.Fatalf("runPurge: %v", err)
	}

	db, _, err = openDB()
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()

	if got := countRows(t, db, "pending_interaction_spans"); got != 1 {
		t.Errorf("pending_interaction_spans: %d rows after --before, want 1 (staging tables only clear on --all)", got)
	}
}
