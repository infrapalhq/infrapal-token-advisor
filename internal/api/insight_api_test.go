package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// insightDB builds one realistic session: a file read three times in one
// prompt, a compaction, two hand approvals of ls, and an Opus turn, plus the
// generated title and the project directory.
func insightDB(t *testing.T) func(method, path, body string) (int, map[string]any) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "ccotel.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	stmts := []string{
		`INSERT INTO sessions (session_id, started_at, ended_at, project_dir) VALUES ('s1', 1000, 900000, '/work/infra-repo')`,
		`INSERT INTO interactions (prompt_id, session_id, seq, started_at, prompt_text) VALUES ('p1', 's1', 1, 1000, 'migrate the rds module to aurora')`,
		`INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, ts, query_source, response_text)
			VALUES ('title1', 'p1', 's1', 1100, 'generate_session_title', '{"title": "Migrate the RDS module"}')`,
		`INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, ts, query_source, response_text)
			VALUES ('m1', 'p1', 's1', 1200, 'repl_main_thread', 'Swapping the instance for a cluster.')`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, tool_input, decision_source, result_size_bytes, success)
			VALUES ('r1', 'p1', 's1', 2000, 'Read', '{"file_path":"/work/infra-repo/main.tf"}', 'config', 31000, 1)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, tool_input, decision_source, result_size_bytes, success)
			VALUES ('r2', 'p1', 's1', 3000, 'Read', '{"file_path":"/work/infra-repo/main.tf"}', 'config', 31000, 1)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, tool_input, decision_source, result_size_bytes, success)
			VALUES ('r3', 'p1', 's1', 4000, 'Read', '{"file_path":"/work/infra-repo/main.tf"}', 'config', 31000, 1)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, full_command, decision_source, blocked_on_user_ms, success)
			VALUES ('a1', 'p1', 's1', 5000, 'Bash', 'ls -la modules/db', 'user_temporary', 40000, 1)`,
		`INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, full_command, decision_source, blocked_on_user_ms, success)
			VALUES ('a2', 'p1', 's1', 6000, 'Bash', 'ls -la modules', 'user_temporary', 35000, 1)`,
		`INSERT INTO compactions (session_id, prompt_id, ts, pre_tokens, post_tokens, trigger, duration_ms)
			VALUES ('s1', 'p1', 4500, 223912, 24940, 'auto', 66877)`,
		`INSERT INTO api_requests (prompt_id, session_id, ts, model, query_source, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd_micros)
			VALUES ('p1', 's1', 1500, 'claude-opus-5-5', 'repl_main_thread', 1000, 300, 50000, 2000, 800000)`,
		`INSERT INTO api_requests (prompt_id, session_id, ts, model, query_source, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd_micros)
			VALUES ('p1', 's1', 4600, 'claude-opus-5-5', 'repl_main_thread', 2000, 500, 90000, 0, 1200000)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}

	ts := newTestServer(t, db)
	call := func(method, path, body string) (int, map[string]any) {
		t.Helper()
		var req *http.Request
		if body != "" {
			req, _ = http.NewRequest(method, ts.URL+path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req, _ = http.NewRequest(method, ts.URL+path, nil)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		var out map[string]any
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}
	return call
}

func TestSessionCardsCarryProjectTitleAndPriority(t *testing.T) {
	call := insightDB(t)
	code, body := call("GET", "/api/sessions/cards", "")
	if code != 200 {
		t.Fatalf("status %d: %v", code, body)
	}
	cards := body["sessions"].([]any)
	if len(cards) != 1 {
		t.Fatalf("want 1 card, got %d", len(cards))
	}
	c := cards[0].(map[string]any)
	if c["project"] != "infra-repo" {
		t.Errorf("project: want infra-repo, got %v", c["project"])
	}
	if c["title"] != "Migrate the RDS module" {
		t.Errorf("title should be the generated one, got %v", c["title"])
	}
	if c["priority"] != "now" {
		t.Errorf("3 reads in one prompt is a full-severity context issue; priority want now, got %v", c["priority"])
	}
	syms := c["symptoms"].([]any)
	if !containsAny(syms, "compacted") || !containsAny(syms, "repeats") {
		t.Errorf("want compacted and repeats symptoms, got %v", syms)
	}
	if c["wait_ms"].(float64) != 75000 {
		t.Errorf("wait should sum the two hand approvals: 75000 ms, got %v", c["wait_ms"])
	}
}

func TestOverviewTotalsAndFixFirst(t *testing.T) {
	call := insightDB(t)
	code, body := call("GET", "/api/overview", "")
	if code != 200 {
		t.Fatalf("status %d: %v", code, body)
	}
	if spend := body["spend_usd"].(float64); spend < 1.99 || spend > 2.01 {
		t.Errorf("spend should be $0.80 + $1.20 = $2.00, got %v", spend)
	}
	if body["avoidable_usd"].(float64) <= 0 {
		t.Errorf("open issues should price some avoidable spend, got %v", body["avoidable_usd"])
	}
	first, _ := body["fix_first"].([]any)
	if len(first) == 0 {
		t.Fatalf("fix_first should not be empty")
	}
	kinds := map[string]bool{}
	for _, it := range first {
		kinds[it.(map[string]any)["kind"].(string)] = true
	}
	if !kinds["re_read"] {
		t.Errorf("the re-read should be among the top fixes, got %v", kinds)
	}
	if body["waiting_ms"].(float64) != 75000 {
		t.Errorf("waiting_ms: want 75000, got %v", body["waiting_ms"])
	}
}

func TestFixesGroupsAndMarkingMovesItem(t *testing.T) {
	call := insightDB(t)
	_, before := call("GET", "/api/fixes", "")
	toFix := before["to_fix"].([]any)
	var key string
	for _, it := range toFix {
		m := it.(map[string]any)
		if m["kind"] == "re_read" {
			key = m["key"].(string)
		}
	}
	if key == "" {
		t.Fatalf("re_read should be a to-fix item: %v", before)
	}

	code, res := call("POST", "/api/fixes/"+url.PathEscape(key)+"/state", `{"state":"fixed"}`)
	if code != 200 || res["state"] != "fixed" {
		t.Fatalf("mark fixed: status %d body %v", code, res)
	}

	_, after := call("GET", "/api/fixes", "")
	for _, it := range after["to_fix"].([]any) {
		if it.(map[string]any)["key"] == key {
			t.Fatalf("a marked fix must leave the to-fix list")
		}
	}
	marked := after["marked"].([]any)
	if len(marked) != 1 || marked[0].(map[string]any)["key"] != key {
		t.Fatalf("marked list should hold the fix, got %v", marked)
	}

	code, _ = call("POST", "/api/fixes/"+url.PathEscape(key)+"/state", `{"state":"reverted"}`)
	if code != 200 {
		t.Fatalf("revert status %d", code)
	}
	_, reverted := call("GET", "/api/fixes", "")
	if len(reverted["marked"].([]any)) != 0 {
		t.Fatalf("reverting should return the item to open")
	}
}

func TestUnknownFixIsNotFound(t *testing.T) {
	call := insightDB(t)
	code, _ := call("POST", "/api/fixes/"+url.PathEscape("re_read|/nope")+"/state", `{"state":"fixed"}`)
	if code != http.StatusNotFound {
		t.Fatalf("unknown fix should be 404, got %d", code)
	}
}

func TestTimelineRowsFlagsAndContext(t *testing.T) {
	call := insightDB(t)
	code, body := call("GET", "/api/sessions/s1/timeline", "")
	if code != 200 {
		t.Fatalf("status %d: %v", code, body)
	}
	rows := body["rows"].([]any)
	kinds := map[string]int{}
	var flagged, withCtx int
	for _, r := range rows {
		m := r.(map[string]any)
		kinds[m["kind"].(string)]++
		if m["flag"] != nil && m["flag"] != "" {
			flagged++
		}
		if m["context_tokens"] != nil {
			withCtx++
		}
	}
	if kinds["div"] != 1 || kinds["you"] != 1 || kinds["claude"] != 1 || kinds["tool"] != 5 || kinds["event"] != 1 {
		t.Errorf("unexpected row mix: %v", kinds)
	}
	if flagged == 0 {
		t.Errorf("rows covered by an occurrence should carry a flag")
	}
	if body["peak_context"].(float64) < 90000 {
		t.Errorf("peak context should be the larger request's input side, got %v", body["peak_context"])
	}
	if body["compactions"].(float64) != 1 {
		t.Errorf("compactions: want 1, got %v", body["compactions"])
	}
}

func TestTimelineUnknownSession(t *testing.T) {
	call := insightDB(t)
	if code, _ := call("GET", "/api/sessions/nope/timeline", ""); code != 404 {
		t.Fatalf("want 404, got %d", code)
	}
}

func TestVisitRecordsLastVisitAndMarksNew(t *testing.T) {
	call := insightDB(t)
	code, first := call("POST", "/api/ui/visit", "")
	if code != 200 {
		t.Fatalf("visit status %d", code)
	}
	if first["previous_visit_at"].(float64) != 0 {
		t.Fatalf("first visit has no previous visit")
	}
	_, state := call("GET", "/api/ui/state", "")
	if state["last_visit_at"] == nil {
		t.Fatalf("state should include last_visit_at: %v", state)
	}
	_, cards := call("GET", "/api/sessions/cards", "")
	c := cards["sessions"].([]any)[0].(map[string]any)
	if c["is_new"] != false {
		t.Errorf("a session started before the visit is not new")
	}
}

func TestBadRangeIsBadRequest(t *testing.T) {
	call := insightDB(t)
	if code, _ := call("GET", "/api/overview?since=whenever", ""); code != 400 {
		t.Fatalf("want 400 for an unknown range, got %d", code)
	}
}

func containsAny(list []any, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func TestProgressWeeksAndStatus(t *testing.T) {
	call := insightDB(t)
	code, body := call("GET", "/api/progress?since=all", "")
	if code != 200 {
		t.Fatalf("progress status %d: %v", code, body)
	}
	weeks, _ := body["weeks"].([]any)
	if len(weeks) == 0 {
		t.Fatalf("progress should have at least one week")
	}
	total := 0.0
	for _, w := range weeks {
		total += w.(map[string]any)["sessions"].(float64)
	}
	if total != 1 {
		t.Errorf("one session in the fixture, weeks should sum to 1, got %v", total)
	}

	code, st := call("GET", "/api/status", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	if _, ok := st["collector_live"]; !ok {
		t.Errorf("status should report collector_live: %v", st)
	}
}

func TestEmptySessionIsListedButNotCounted(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "ccotel.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, stmt := range []string{
		`INSERT INTO sessions (session_id, started_at, ended_at, project_dir) VALUES ('full', 1000, 2000, '/work/repo')`,
		`INSERT INTO sessions (session_id, started_at, ended_at, project_dir) VALUES ('blank', 1000, 2000, '/work/repo')`,
		`INSERT INTO api_requests (prompt_id, session_id, ts, model, query_source, input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd_micros)
			VALUES ('p', 'full', 1500, 'claude-opus-5-5', 'repl_main_thread', 100, 10, 0, 0, 1000)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("seed %q: %v", stmt, err)
		}
	}
	ts := newTestServer(t, db)
	resp, err := http.Get(ts.URL + "/api/sessions/cards")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		CountAll int              `json:"count_all"`
		Sessions []map[string]any `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.CountAll != 1 || len(out.Sessions) != 2 {
		t.Fatalf("want 1 counted of 2 listed: %d of %d", out.CountAll, len(out.Sessions))
	}
}
