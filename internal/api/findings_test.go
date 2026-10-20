package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mustExec runs a write query against db, failing the test on error.
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// getFindingsForTest hits GET /api/findings and decodes the response.
func getFindingsForTest(t *testing.T, ts *httptest.Server) []Finding {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/findings")
	if err != nil {
		t.Fatalf("GET /api/findings: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var findings []Finding
	if err := json.NewDecoder(resp.Body).Decode(&findings); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return findings
}

func findFinding(findings []Finding, kind string) *Finding {
	for i := range findings {
		if findings[i].Kind == kind {
			return &findings[i]
		}
	}
	return nil
}

// TestHandleFindings_EveryFindingHasAFix is the hard rule as a test: every
// Finding this endpoint ever returns must carry a non-empty Fix — a
// runnable command or config diff, never advice. computeFindings already
// enforces this at runtime (it errors if a detector violates it); this
// confirms the enforcement actually holds across a realistic mixed
// fixture, not just that it doesn't panic.
func TestHandleFindings_EveryFindingHasAFix(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, blocked_on_user_ms)
		VALUES ('tu-blocked', 'p1', 's1', 2000, 'Read', 'accept', 1, 70000)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command)
		VALUES ('tu-temp', 'p1', 's1', 2100, 'Bash', 'accept', 'user_temporary', 1, 'npm run build')`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r1', 'p1', 's1', 2200, 'Edit', 'reject', 1)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r2', 'p1', 's1', 2300, 'Edit', 'reject', 1)`)
	for i, id := range []string{"tu-fp1", "tu-fp2", "tu-fp3"} {
		mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, result_size_bytes)
			VALUES (?, 'p1', 's1', ?, 'Read', 'accept', 1, 40000)`, id, 2400+i)
	}

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	if len(findings) == 0 {
		t.Fatal("expected at least one finding from this fixture")
	}
	for _, f := range findings {
		if f.Fix == "" {
			t.Errorf("finding %q has no fix — every returned finding must be actionable: %+v", f.ID, f)
		}
		if f.FixLang == "" {
			t.Errorf("finding %q has a fix but no fix_lang", f.ID)
		}
	}
}

func TestDeadMCPFinding_Present(t *testing.T) {
	// seedDB's fixture already has one "github" mcp_server_connections row
	// with no matching tool_calls row (the fixture's one tool_call is plain
	// Bash) — already a qualifying dead server with no extra rows needed.
	db := seedDB(t)
	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)

	f := findFinding(findings, "dead_mcp")
	if f == nil {
		t.Fatalf("expected a dead_mcp finding, got none: %+v", findings)
	}
	if f.ID != "dead_mcp" || f.Link != "/spend" {
		t.Errorf("id/link = %q/%q, want dead_mcp//spend", f.ID, f.Link)
	}
	if f.Title != "1 MCP server you never call" {
		t.Errorf("title = %q, want the news, not a category label", f.Title)
	}
	if !strings.Contains(f.Evidence, "github") {
		t.Errorf("evidence = %q, want it to mention github", f.Evidence)
	}
	if !strings.Contains(f.Fix, `claude mcp remove "github"`) {
		t.Errorf("fix = %q, want a runnable claude mcp remove command", f.Fix)
	}
	if f.Viz == nil || len(f.Viz.Points) != 1 || f.Viz.Points[0].Label != "github" {
		t.Errorf("viz = %+v, want one point for github", f.Viz)
	}
	if f.Impact.TokensEst != nil {
		t.Errorf("tokens_est = %v, want nil — MCP definition tokens aren't separately reported", f.Impact.TokensEst)
	}
	if f.State != "open" {
		t.Errorf("state = %q, want open", f.State)
	}
}

func TestDeadMCPFinding_AbsentWhenUsed(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, mcp_server_name, mcp_tool_name, decision, success)
		VALUES ('tu-mcp', 'p1', 's1', 2000, 'github__search', 'github', 'search', 'accept', 1)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	if f := findFinding(findings, "dead_mcp"); f != nil {
		t.Errorf("expected no dead_mcp finding once github has a tool call, got %+v", f)
	}
}

func TestPermissionFrictionFinding_Present(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, blocked_on_user_ms)
		VALUES ('tu-blocked', 'p1', 's1', 2000, 'Read', 'accept', 'user_temporary', 1, 70000)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)

	f := findFinding(findings, "permission_friction")
	if f == nil {
		t.Fatalf("expected a permission_friction finding, got none: %+v", findings)
	}
	if f.Impact.Minutes == nil || *f.Impact.Minutes < 1.16 {
		t.Errorf("minutes = %v, want at least ~70s worth", f.Impact.Minutes)
	}
	if f.Title != "You're the bottleneck — 1 min" {
		t.Errorf("title = %q, want the rounded headline form", f.Title)
	}
	if f.FixLang != "settings-json" {
		t.Errorf("fix_lang = %q, want settings-json", f.FixLang)
	}
	var patch permissionsPatch
	if err := json.Unmarshal([]byte(f.Fix), &patch); err != nil {
		t.Fatalf("fix isn't valid JSON: %v\nfix: %s", err, f.Fix)
	}
	if len(patch.Permissions.Allow) != 1 || patch.Permissions.Allow[0] != "Read" {
		t.Errorf("fix allow list = %+v, want [\"Read\"] (the tool was already hand-approved once)", patch.Permissions.Allow)
	}
}

// TestPermissionFrictionFinding_ExcludesBlockByDesignTools guards the
// specific data-honesty rule: AskUserQuestion/ExitPlanMode block by design,
// so their blocked_on_user_ms must not count toward this finding at all.
func TestPermissionFrictionFinding_ExcludesBlockByDesignTools(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, blocked_on_user_ms)
		VALUES ('tu-ask', 'p1', 's1', 2000, 'AskUserQuestion', 'accept', 1, 300000)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	if f := findFinding(findings, "permission_friction"); f != nil {
		t.Errorf("expected no permission_friction finding from block-by-design tools alone, got %+v", f)
	}
}

// TestPermissionFrictionFinding_FixNeverSuggestsAllowingBlockByDesignTools
// guards a real bug caught against live data: a block-by-design tool
// (AskUserQuestion, ExitPlanMode) can pick up a one-off decision_source =
// 'user_temporary' row (the user approved one specific instance), but
// promoting that to a permanent allow rule would defeat the tool's whole
// purpose of always interrupting. The suggested fix must never include it,
// even when a real, separate qualifying pattern (a Bash prefix) exists to
// carry the finding.
func TestPermissionFrictionFinding_FixNeverSuggestsAllowingBlockByDesignTools(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, blocked_on_user_ms)
		VALUES ('tu-ask', 'p1', 's1', 2000, 'AskUserQuestion', 'accept', 'user_temporary', 1, 70000)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-bash', 'p1', 's1', 2100, 'Bash', 'accept', 'user_temporary', 1, 'npm run build', 65000)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)

	f := findFinding(findings, "permission_friction")
	if f == nil {
		t.Fatalf("expected a permission_friction finding (the Bash pattern qualifies), got none: %+v", findings)
	}
	var patch permissionsPatch
	if err := json.Unmarshal([]byte(f.Fix), &patch); err != nil {
		t.Fatalf("fix isn't valid JSON: %v\nfix: %s", err, f.Fix)
	}
	for _, entry := range patch.Permissions.Allow {
		if entry == "AskUserQuestion" || entry == "ExitPlanMode" {
			t.Errorf("fix suggests permanently allowing %q, which defeats the tool's purpose: %+v", entry, patch.Permissions.Allow)
		}
	}
}

// TestPermissionFrictionFinding_AbsentWithNoTempAllowPattern guards the
// hard rule directly: real blocked time with nothing ever hand-approved
// (decision_source never 'user_temporary') has no rule to suggest, so the
// finding must not fire at all rather than falling back to vague advice.
func TestPermissionFrictionFinding_AbsentWithNoTempAllowPattern(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, blocked_on_user_ms)
		VALUES ('tu-blocked', 'p1', 's1', 2000, 'Read', 'accept', 'config', 1, 70000)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	if f := findFinding(findings, "permission_friction"); f != nil {
		t.Errorf("expected no permission_friction finding with nothing ever hand-approved, got %+v", f)
	}
}

func TestPermissionFrictionFinding_BashPrefixInFix(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-b1', 'p1', 's1', 2000, 'Bash', 'accept', 'user_temporary', 1, 'npm run build --watch', 40000)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, decision_source, success, full_command, blocked_on_user_ms)
		VALUES ('tu-b2', 'p1', 's1', 2100, 'Bash', 'accept', 'user_temporary', 1, 'npm run test', 30000)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)

	f := findFinding(findings, "permission_friction")
	if f == nil {
		t.Fatalf("expected a permission_friction finding, got none: %+v", findings)
	}
	var patch permissionsPatch
	if err := json.Unmarshal([]byte(f.Fix), &patch); err != nil {
		t.Fatalf("fix isn't valid JSON: %v\nfix: %s", err, f.Fix)
	}
	if len(patch.Permissions.Allow) != 1 || patch.Permissions.Allow[0] != "Bash(npm run:*)" {
		t.Errorf("allow list = %+v, want a single deduped Bash(npm run:*) entry", patch.Permissions.Allow)
	}
}

func TestRejectedWorkFinding_Present(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r1', 'p1', 's1', 2000, 'Edit', 'reject', 1)`)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r2', 'p1', 's1', 2100, 'Edit', 'reject', 1)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)

	f := findFinding(findings, "rejected_work")
	if f == nil {
		t.Fatalf("expected a rejected_work finding, got none: %+v", findings)
	}
	if f.Impact.USD == nil {
		t.Errorf("expected a USD impact, got nil")
	}
	if f.Title != "You keep rejecting Edit" {
		t.Errorf("title = %q, want the news headline form", f.Title)
	}
	var patch permissionsPatch
	if err := json.Unmarshal([]byte(f.Fix), &patch); err != nil {
		t.Fatalf("fix isn't valid JSON: %v\nfix: %s", err, f.Fix)
	}
	if len(patch.Permissions.Deny) != 1 || patch.Permissions.Deny[0] != "Edit" {
		t.Errorf("deny list = %+v, want [\"Edit\"]", patch.Permissions.Deny)
	}
}

func TestRejectedWorkFinding_AbsentBelowMinCount(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success)
		VALUES ('tu-r1', 'p1', 's1', 2000, 'Edit', 'reject', 1)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	if f := findFinding(findings, "rejected_work"); f != nil {
		t.Errorf("expected no rejected_work finding below min count, got %+v", f)
	}
}

func TestFatPayloadFinding_Present(t *testing.T) {
	db := seedDB(t)
	for i, id := range []string{"tu-fp1", "tu-fp2", "tu-fp3"} {
		mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, result_size_bytes)
			VALUES (?, 'p1', 's1', ?, 'Read', 'accept', 1, 40000)`, id, 2000+i)
	}
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cost_usd_micros, input_tokens, success)
		VALUES ('req-rate', 'p1', 's1', 1900, 'claude-3-opus', 1000000, 100000, 1)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)

	f := findFinding(findings, "fat_payload")
	if f == nil {
		t.Fatalf("expected a fat_payload finding, got none: %+v", findings)
	}
	if !f.Impact.Estimated {
		t.Errorf("expected Impact.Estimated = true (bytes->tokens is an estimate)")
	}
	if f.Impact.TokensEst == nil || *f.Impact.TokensEst != 30000 {
		t.Errorf("tokens_est = %v, want 30000 (120000 bytes / 4)", f.Impact.TokensEst)
	}
	if f.Impact.USD == nil || *f.Impact.USD <= 0 {
		t.Errorf("usd = %v, want a positive derived figure from this install's blended rate", f.Impact.USD)
	}
	if !strings.Contains(f.Title, "Read is dumping") {
		t.Errorf("title = %q, want it to name the tool and the news", f.Title)
	}
	if f.FixLang != "claude-md" {
		t.Errorf("fix_lang = %q, want claude-md", f.FixLang)
	}
	if !strings.Contains(f.Fix, "CLAUDE.md") {
		t.Errorf("fix = %q, want a CLAUDE.md snippet", f.Fix)
	}
}

func TestFatPayloadFinding_AbsentBelowThreshold(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, result_size_bytes)
		VALUES ('tu-fp1', 'p1', 's1', 2000, 'Read', 'accept', 1, 500)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	if f := findFinding(findings, "fat_payload"); f != nil {
		t.Errorf("expected no fat_payload finding below the avg-bytes threshold, got %+v", f)
	}
}

// TestFindings_SessionOpenModelMixCacheInvalidationNotFindings confirms the
// three signals with no runnable fix (a long-idle session, background
// calls on an expensive model, a poor cache-hit session) never come back
// as Finding kinds at all — they're computed by v_session_cache_health /
// v_ancillary_model_spend for the Spend page instead, not routed through
// this endpoint.
func TestFindings_SessionOpenModelMixCacheInvalidationNotFindings(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s2', 10000, 20000000)`)
	mustExec(t, db, `INSERT INTO interactions (prompt_id, session_id, seq, started_at, duration_ms, prompt_text)
		VALUES ('p2', 's2', 1, 10500, 100, 'first')`)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, query_source, cost_usd_micros, success)
		VALUES ('req-mm1', 'p1', 's1', 2000, 'claude-sonnet-5', 'away_summary', 600000, 1)`)
	mustExec(t, db, `INSERT INTO api_requests (request_id, prompt_id, session_id, ts, model, cache_read_tokens, cache_creation_tokens, success)
		VALUES ('req-ci1', 'p1', 's1', 2100, 'claude-3-opus', 10000, 15000, 1)`)

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	for _, kind := range []string{"session_open", "model_mix", "cache_invalidation", "retry_loop"} {
		if f := findFinding(findings, kind); f != nil {
			t.Errorf("kind %q should never be returned by /api/findings, got %+v", kind, f)
		}
	}
}

func TestHandleFindings_Shape(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)

	for _, f := range findings {
		if f.ID == "" || f.Kind == "" || f.Severity == "" || f.Title == "" || f.Evidence == "" || f.State == "" {
			t.Errorf("finding missing a required field: %+v", f)
		}
	}
}

func TestHandlePostFindingState_DismissPersists(t *testing.T) {
	db := seedDB(t) // dead_mcp qualifies via the fixture's github connection.
	ts := newTestServer(t, db)

	before := getFindingsForTest(t, ts)
	f := findFinding(before, "dead_mcp")
	if f == nil || f.State != "open" {
		t.Fatalf("expected an open dead_mcp finding before dismissing, got %+v", f)
	}

	resp, err := http.Post(ts.URL+"/api/findings/dead_mcp/state", "application/json",
		strings.NewReader(`{"state":"dismissed"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	after := getFindingsForTest(t, ts)
	f2 := findFinding(after, "dead_mcp")
	if f2 == nil || f2.State != "dismissed" {
		t.Fatalf("expected dead_mcp to read back as dismissed, got %+v", f2)
	}
}

func TestHandlePostFindingState_LapsedSnoozeReturnsToOpen(t *testing.T) {
	db := seedDB(t)
	mustExec(t, db, `INSERT INTO finding_state (finding_id, state, snoozed_until, updated_at)
		VALUES ('dead_mcp', 'snoozed', 1, 1)`) // snoozed_until in the deep past

	ts := newTestServer(t, db)
	findings := getFindingsForTest(t, ts)
	f := findFinding(findings, "dead_mcp")
	if f == nil || f.State != "open" {
		t.Fatalf("expected a lapsed snooze to read back as open, got %+v", f)
	}
}

func TestHandlePostFindingState_InvalidState(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Post(ts.URL+"/api/findings/dead_mcp/state", "application/json",
		strings.NewReader(`{"state":"bogus"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestHandleFindingsActivity_CountsFixedAndDismissed(t *testing.T) {
	db := seedDB(t)
	now := time.Now().UnixMilli()
	mustExec(t, db, `INSERT INTO finding_state (finding_id, state, snoozed_until, updated_at) VALUES ('dead_mcp', 'fixed', NULL, ?)`, now)
	mustExec(t, db, `INSERT INTO finding_state (finding_id, state, snoozed_until, updated_at) VALUES ('fat_payload', 'dismissed', NULL, ?)`, now)
	mustExec(t, db, `INSERT INTO finding_state (finding_id, state, snoozed_until, updated_at) VALUES ('rejected_work', 'fixed', NULL, 1)`) // ancient, outside the window

	ts := newTestServer(t, db)
	resp, err := http.Get(ts.URL + "/api/findings/activity?since=7d")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Fixed     int64 `json:"fixed"`
		Dismissed int64 `json:"dismissed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Fixed != 1 {
		t.Errorf("fixed = %d, want 1 (the ancient row falls outside the 7d window)", body.Fixed)
	}
	if body.Dismissed != 1 {
		t.Errorf("dismissed = %d, want 1", body.Dismissed)
	}
}

// TestHandleFindingsActivity_EmptyTable guards a real bug caught against
// live data: SUM() over zero matching rows returns SQL NULL, not 0 — every
// fresh install with nothing ever dismissed/fixed hits this on the very
// first load, and it must return zeroes, not a 500.
func TestHandleFindingsActivity_EmptyTable(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/findings/activity?since=7d")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Fixed     int64 `json:"fixed"`
		Dismissed int64 `json:"dismissed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Fixed != 0 || body.Dismissed != 0 {
		t.Errorf("fixed/dismissed = %d/%d, want 0/0 on an empty finding_state table", body.Fixed, body.Dismissed)
	}
}

func TestHandleFindingsActivity_InvalidSince(t *testing.T) {
	db := seedDB(t)
	ts := newTestServer(t, db)

	resp, err := http.Get(ts.URL + "/api/findings/activity?since=not-a-duration")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
