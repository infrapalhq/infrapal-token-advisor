package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShamikMukhopadhyay/ccotel/internal/store"
)

// mustExec runs a write query against db, failing the test on error.
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// getInsightsForTest hits GET /api/insights with no ?since= and decodes the
// response.
func getInsightsForTest(t *testing.T, ts *httptest.Server) []Insight {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/insights")
	if err != nil {
		t.Fatalf("GET /api/insights: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var insights []Insight
	if err := json.NewDecoder(resp.Body).Decode(&insights); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return insights
}

func findInsight(insights []Insight, category string) *Insight {
	for i := range insights {
		if insights[i].Category == category {
			return &insights[i]
		}
	}
	return nil
}

// findInsightByID finds the first insight whose ID starts with idPrefix —
// needed once a category holds more than one insight type (e.g. "waste" now
// covers bash_allow_rule, frequently_rejected_tool, and long_approval_wait).
func findInsightByID(insights []Insight, idPrefix string) *Insight {
	for i := range insights {
		if strings.HasPrefix(insights[i].ID, idPrefix) {
			return &insights[i]
		}
	}
	return nil
}

func TestTopCostPromptInsight_Present(t *testing.T) {
	db := seedDB(t) // p1 already costs $0.10 — p2 below should outrank it.
	mustExec(t, db, `INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s2', 2000, 3000)`)
	mustExec(t, db, `INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
		VALUES ('p2', 's2', 1, 2000, 500, 'expensive prompt')`)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, input_tokens, output_tokens, success)
		VALUES ('req-2a', 'p2', 's2', 2000, 'claude-3-opus', 5000000, 1000, 2000, 1)`)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, input_tokens, output_tokens, success)
		VALUES ('req-2b', 'p2', 's2', 2100, 'claude-3-opus', 2000000, 500, 500, 1)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-2', 'p2', 's2', 2050, 'Bash', 'reject', 1)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsight(insights, "efficiency")
	if ins == nil {
		t.Fatalf("expected an efficiency insight, got none: %+v", insights)
	}
	if ins.Link != "/waterfall?session=s2&prompt=p2" {
		t.Errorf("link = %q, want /waterfall?session=s2&prompt=p2", ins.Link)
	}
	if !strings.Contains(ins.Detail, "$7.00") {
		t.Errorf("detail = %q, want it to mention $7.00", ins.Detail)
	}
	if !strings.Contains(ins.Detail, "2 turns") {
		t.Errorf("detail = %q, want it to mention 2 turns", ins.Detail)
	}
	if !strings.Contains(ins.Detail, "1 tool call rejected") {
		t.Errorf("detail = %q, want it to mention the rejected tool call", ins.Detail)
	}
}

func TestTopCostPromptInsight_AbsentWhenNoCost(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "ccotel.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s1', 1000, 2000)`)
	mustExec(t, db, `INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
		VALUES ('p1', 's1', 1, 1000, 200, 'free prompt')`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsight(insights, "efficiency"); ins != nil {
		t.Errorf("expected no efficiency insight when no prompt has cost, got %+v", ins)
	}
}

func TestLongSessionInsight_Present(t *testing.T) {
	db := seedDB(t) // s1 runs only 4s — far below the threshold.
	longEndedAt := int64(10000) + orphanSessionThresholdMs + 1000
	mustExec(t, db, `INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s2', 10000, ?)`, longEndedAt)
	// Two interactions on s2, out of seq order, to confirm the lowest-seq one wins.
	mustExec(t, db, `INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
		VALUES ('p2-second', 's2', 2, 11000, 100, 'second')`)
	mustExec(t, db, `INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
		VALUES ('p2-first', 's2', 1, 10500, 100, 'first')`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsight(insights, "sessions")
	if ins == nil {
		t.Fatalf("expected a sessions insight, got none: %+v", insights)
	}
	if ins.Severity != "warn" {
		t.Errorf("severity = %q, want warn", ins.Severity)
	}
	if ins.Link != "/waterfall?session=s2&prompt=p2-first" {
		t.Errorf("link = %q, want /waterfall?session=s2&prompt=p2-first (lowest seq)", ins.Link)
	}
}

func TestLongSessionInsight_AbsentBelowThreshold(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsight(insights, "sessions"); ins != nil {
		t.Errorf("expected no sessions insight for a short session, got %+v", ins)
	}
}

func TestBashAllowRuleInsight_Present(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-b1', 'p1', 's1', 2000, 'Bash', 'accept', 'user_temporary', 1, 'npm run build --watch', 5000)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-b2', 'p1', 's1', 2100, 'Bash', 'accept', 'user_temporary', 1, 'npm run test', 3000)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-b3', 'p1', 's1', 2200, 'Bash', 'accept', 'user_temporary', 1, 'git status', 0)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsight(insights, "waste")
	if ins == nil {
		t.Fatalf("expected a waste insight, got none: %+v", insights)
	}
	if !strings.Contains(ins.Detail, "Bash(npm run:*)") {
		t.Errorf("detail = %q, want it to suggest Bash(npm run:*)", ins.Detail)
	}
	if !strings.Contains(ins.Detail, "2 times") {
		t.Errorf("detail = %q, want it to mention 2 occurrences", ins.Detail)
	}
	if !strings.Contains(ins.Detail, "waiting 8s total") {
		t.Errorf("detail = %q, want the summed blocked_on_user_ms formatted as 8s", ins.Detail)
	}
}

// TestBashAllowRuleInsight_PathArgumentDoesNotBecomeTheRule guards against a
// heuristic bug found against real data: "cd" into a path containing a
// shell-escaped space (e.g. a directory named "Agentic IDE Team") must not
// produce a suggested rule keyed on that one-off, unbounded path.
func TestBashAllowRuleInsight_PathArgumentDoesNotBecomeTheRule(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-c1', 'p1', 's1', 2000, 'Bash', 'accept', 'user_temporary', 1, 'cd /Users/s.mukhopadhyay/Documents/Agentic\ IDE\ Team/sherlock', 1000)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-c2', 'p1', 's1', 2100, 'Bash', 'accept', 'user_temporary', 1, 'cd /somewhere/else && ls', 500)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsight(insights, "waste")
	if ins == nil {
		t.Fatalf("expected a waste insight, got none: %+v", insights)
	}
	if !strings.Contains(ins.Detail, "Bash(cd:*)") {
		t.Errorf("detail = %q, want it to suggest the generic Bash(cd:*), not a path-specific rule", ins.Detail)
	}
	if strings.Contains(ins.Detail, "Agentic") || strings.Contains(ins.Detail, "somewhere") {
		t.Errorf("detail = %q, leaked a one-off path into the suggested rule", ins.Detail)
	}
}

func TestBashAllowRuleInsight_AbsentBelowMinCount(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-b1', 'p1', 's1', 2000, 'Bash', 'accept', 'user_temporary', 1, 'npm run build', 1000)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsight(insights, "waste"); ins != nil {
		t.Errorf("expected no waste insight below the min-count threshold, got %+v", ins)
	}
}

func TestHandleInsights_SinceFilter(t *testing.T) {
	db := seedDB(t) // p1's api_request/interaction both sit at ts=1000 (1970) — ancient.
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/insights?since=1h")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var narrowed []Insight
	if err := json.NewDecoder(resp.Body).Decode(&narrowed); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ins := findInsight(narrowed, "efficiency"); ins != nil {
		t.Errorf("expected ?since=1h to exclude the fixture's ancient prompt, got %+v", ins)
	}

	all := getInsightsForTest(t, ts)
	if ins := findInsight(all, "efficiency"); ins == nil {
		t.Errorf("expected an efficiency insight with no ?since= filter")
	}
}

func TestHandleInsights_InvalidSince(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/insights?since=not-a-duration")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleInsights_Shape(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	for _, ins := range insights {
		if ins.ID == "" || ins.Category == "" || ins.Severity == "" || ins.Title == "" || ins.Detail == "" || ins.Link == "" {
			t.Errorf("insight missing a required field: %+v", ins)
		}
	}
}

func TestHitMaxTokensInsight_Present(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, stop_reason, success)
		VALUES ('req-mt', 'p1', 's1', 1500, 'claude-3-opus', 0, 'max_tokens', 1)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsightByID(insights, "hit_max_tokens:")
	if ins == nil {
		t.Fatalf("expected a hit_max_tokens insight, got none: %+v", insights)
	}
	if ins.Category != "efficiency" || ins.Severity != "warn" {
		t.Errorf("category/severity = %q/%q, want efficiency/warn", ins.Category, ins.Severity)
	}
	if ins.Link != "/waterfall?session=s1&prompt=p1" {
		t.Errorf("link = %q, want /waterfall?session=s1&prompt=p1", ins.Link)
	}
}

func TestHitMaxTokensInsight_AbsentWhenNoneHit(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsightByID(insights, "hit_max_tokens:"); ins != nil {
		t.Errorf("expected no hit_max_tokens insight, got %+v", ins)
	}
}

func TestCompactionInsight_Present(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, query_source, success)
		VALUES ('req-cp', 'p1', 's1', 1500, 'claude-3-opus', 0, 'compact', 1)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsightByID(insights, "compaction:")
	if ins == nil {
		t.Fatalf("expected a compaction insight, got none: %+v", insights)
	}
	if ins.Category != "efficiency" {
		t.Errorf("category = %q, want efficiency", ins.Category)
	}
}

func TestCompactionInsight_AbsentWhenNoneTriggered(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsightByID(insights, "compaction:"); ins != nil {
		t.Errorf("expected no compaction insight, got %+v", ins)
	}
}

func TestLongApprovalWaitInsight_Present(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s2', 2000, 3000)`)
	mustExec(t, db, `INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
		VALUES ('p2', 's2', 1, 2000, 500, 'blocked a lot')`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, blocked_on_user_ms)
		VALUES ('tu-2', 'p2', 's2', 2050, 'Bash', 'accept', 1, 90000)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsightByID(insights, "long_approval_wait:")
	if ins == nil {
		t.Fatalf("expected a long_approval_wait insight, got none: %+v", insights)
	}
	if !strings.Contains(ins.Detail, "1m 30s") {
		t.Errorf("detail = %q, want it to mention 1m 30s", ins.Detail)
	}
	if ins.Link != "/waterfall?session=s2&prompt=p2" {
		t.Errorf("link = %q, want /waterfall?session=s2&prompt=p2", ins.Link)
	}
}

func TestLongApprovalWaitInsight_AbsentBelowThreshold(t *testing.T) {
	db := seedDB(t) // tu-1's blocked_on_user_ms is unset — far under the threshold.
	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsightByID(insights, "long_approval_wait:"); ins != nil {
		t.Errorf("expected no long_approval_wait insight, got %+v", ins)
	}
}

func TestFrequentlyRejectedToolInsight_Present(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r1', 'p1', 's1', 2000, 'Edit', 'reject', 1)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r2', 'p1', 's1', 2100, 'Edit', 'reject', 1)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsightByID(insights, "frequently_rejected_tool:")
	if ins == nil {
		t.Fatalf("expected a frequently_rejected_tool insight, got none: %+v", insights)
	}
	if !strings.Contains(ins.Detail, "`Edit` 2 times") {
		t.Errorf("detail = %q, want it to mention Edit rejected 2 times", ins.Detail)
	}
}

func TestFrequentlyRejectedToolInsight_AbsentBelowMinCount(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r1', 'p1', 's1', 2000, 'Edit', 'reject', 1)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsightByID(insights, "frequently_rejected_tool:"); ins != nil {
		t.Errorf("expected no frequently_rejected_tool insight below min count, got %+v", ins)
	}
}

// TestDeadMCPServerInsight_Present relies on seedDB's own fixture: it already
// seeds one "github" mcp_server_connections row with no matching tool_calls
// row (the fixture's one tool_call is plain Bash) — already a qualifying
// dead server with no extra rows needed.
func TestDeadMCPServerInsight_Present(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	ins := findInsightByID(insights, "dead_mcp_server:github")
	if ins == nil {
		t.Fatalf("expected a dead_mcp_server insight for github, got none: %+v", insights)
	}
	if ins.Category != "tools" || ins.Link != "/tools" {
		t.Errorf("category/link = %q/%q, want tools//tools", ins.Category, ins.Link)
	}
}

func TestDeadMCPServerInsight_AbsentWhenUsed(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, mcp_server_name, mcp_tool_name, decision, success)
		VALUES ('tu-mcp', 'p1', 's1', 2000, 'github__search', 'github', 'search', 'accept', 1)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)
	if ins := findInsightByID(insights, "dead_mcp_server:github"); ins != nil {
		t.Errorf("expected no dead_mcp_server insight once github has a tool call, got %+v", ins)
	}
}

// TestHandleInsights_MultipleInsightsPerCategoryStack confirms the API can
// return more than one insight in the same category — the frontend switched
// from find() to filter()+map() specifically to stack these rather than
// silently dropping all but the first.
func TestHandleInsights_MultipleInsightsPerCategoryStack(t *testing.T) {
	db := seedDB(t)
	// bash_allow_rule
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command)
		VALUES ('tu-b1', 'p1', 's1', 2000, 'Bash', 'accept', 'user_temporary', 1, 'npm run build')`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command)
		VALUES ('tu-b2', 'p1', 's1', 2100, 'Bash', 'accept', 'user_temporary', 1, 'npm run test')`)
	// frequently_rejected_tool
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r1', 'p1', 's1', 2200, 'Edit', 'reject', 1)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r2', 'p1', 's1', 2300, 'Edit', 'reject', 1)`)
	// long_approval_wait
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, blocked_on_user_ms)
		VALUES ('tu-w1', 'p1', 's1', 2400, 'Read', 'accept', 1, 120000)`)

	ts := newTestServer(t, db)
	insights := getInsightsForTest(t, ts)

	wasteCount := 0
	for _, ins := range insights {
		if ins.Category == "waste" {
			wasteCount++
		}
	}
	if wasteCount != 3 {
		t.Fatalf("expected 3 stacked waste insights, got %d: %+v", wasteCount, insights)
	}
}
