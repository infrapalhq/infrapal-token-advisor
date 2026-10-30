package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func openViewsTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "token-advisor.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func exec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestView_PromptEfficiency(t *testing.T) {
	db := openViewsTestDB(t)

	exec(t, db, `INSERT INTO interactions (prompt_id, session_id, started_at, duration_ms) VALUES ('p1', 's1', 1000, 500)`)
	exec(t, db, `
		INSERT INTO api_requests (prompt_id, session_id, ts, model, query_source, stop_reason,
			input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd_micros)
		VALUES ('p1', 's1', 1000, 'claude-3-opus', 'compact', NULL, 100, 200, 50, 50, 1000000)
	`)
	exec(t, db, `
		INSERT INTO api_requests (prompt_id, session_id, ts, model, query_source, stop_reason,
			input_tokens, output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd_micros)
		VALUES ('p1', 's1', 1100, 'claude-3-opus', NULL, 'max_tokens', 10, 20, 0, 0, 500000)
	`)
	exec(t, db, `
		INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, blocked_on_user_ms)
		VALUES ('tu-1', 'p1', 's1', 1000, 'Bash', 'reject', 0, 300)
	`)
	exec(t, db, `
		INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, tool_name, decision, success, blocked_on_user_ms)
		VALUES ('tu-2', 'p1', 's1', 1050, 'Edit', 'accept', 1, 0)
	`)

	var llmTurns, totalTokens, toolCallCount, rejectedCount, toolErrorCount, blockedMs int64
	var totalCost, cacheHitRatio float64
	var triggeredCompaction, hitMaxTokens int64
	err := db.QueryRow(`
		SELECT llm_turn_count, total_tokens, total_cost_usd, cache_hit_ratio,
			tool_call_count, rejected_count, tool_error_count, blocked_on_user_ms,
			triggered_compaction, hit_max_tokens
		FROM v_prompt_efficiency WHERE prompt_id = 'p1'
	`).Scan(&llmTurns, &totalTokens, &totalCost, &cacheHitRatio,
		&toolCallCount, &rejectedCount, &toolErrorCount, &blockedMs,
		&triggeredCompaction, &hitMaxTokens)
	if err != nil {
		t.Fatalf("query v_prompt_efficiency: %v", err)
	}

	if llmTurns != 2 {
		t.Errorf("llm_turn_count = %d, want 2", llmTurns)
	}
	if totalTokens != 430 { // (100+200+50+50) + (10+20+0+0)
		t.Errorf("total_tokens = %d, want 430", totalTokens)
	}
	if totalCost != 1.5 {
		t.Errorf("total_cost_usd = %v, want 1.5", totalCost)
	}
	if cacheHitRatio != 0.5 { // 50 / (50+50)
		t.Errorf("cache_hit_ratio = %v, want 0.5", cacheHitRatio)
	}
	if toolCallCount != 2 {
		t.Errorf("tool_call_count = %d, want 2", toolCallCount)
	}
	if rejectedCount != 1 {
		t.Errorf("rejected_count = %d, want 1", rejectedCount)
	}
	if toolErrorCount != 1 {
		t.Errorf("tool_error_count = %d, want 1", toolErrorCount)
	}
	if blockedMs != 300 {
		t.Errorf("blocked_on_user_ms = %d, want 300", blockedMs)
	}
	if triggeredCompaction != 1 {
		t.Errorf("triggered_compaction = %d, want 1", triggeredCompaction)
	}
	if hitMaxTokens != 1 {
		t.Errorf("hit_max_tokens = %d, want 1", hitMaxTokens)
	}
}

func TestView_WastedSpend(t *testing.T) {
	db := openViewsTestDB(t)

	// p1: has a rejected tool call, two api_requests rows costing 1.0 + 0.5 usd.
	exec(t, db, `INSERT INTO interactions (prompt_id, session_id, started_at) VALUES ('p1', 's1', 1000)`)
	exec(t, db, `INSERT INTO api_requests (prompt_id, session_id, ts, cost_usd_micros) VALUES ('p1', 's1', 1000, 1000000)`)
	exec(t, db, `INSERT INTO api_requests (prompt_id, session_id, ts, cost_usd_micros) VALUES ('p1', 's1', 1100, 500000)`)
	exec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, decision) VALUES ('tu-1', 'p1', 's1', 1000, 'reject')`)

	// p2: no rejected tool call — must not show up as wasted.
	exec(t, db, `INSERT INTO interactions (prompt_id, session_id, started_at) VALUES ('p2', 's1', 1000)`)
	exec(t, db, `INSERT INTO api_requests (prompt_id, session_id, ts, cost_usd_micros) VALUES ('p2', 's1', 1000, 2000000)`)
	exec(t, db, `INSERT INTO tool_calls (tool_use_id, prompt_id, session_id, ts, decision) VALUES ('tu-2', 'p2', 's1', 1000, 'accept')`)

	var wastedCost float64
	var rejectedCount int64
	err := db.QueryRow(`SELECT wasted_cost_usd, rejected_tool_count FROM v_wasted_spend WHERE session_id = 's1'`).
		Scan(&wastedCost, &rejectedCount)
	if err != nil {
		t.Fatalf("query v_wasted_spend: %v", err)
	}
	if wastedCost != 1.5 {
		t.Errorf("wasted_cost_usd = %v, want 1.5 (p1's cost only, not p2's)", wastedCost)
	}
	if rejectedCount != 1 {
		t.Errorf("rejected_tool_count = %d, want 1", rejectedCount)
	}
}

func TestView_PermissionFriction(t *testing.T) {
	db := openViewsTestDB(t)

	exec(t, db, `
		INSERT INTO tool_calls (tool_use_id, tool_name, decision_source, blocked_on_user_ms)
		VALUES ('tu-1', 'Bash', 'user_temporary', 100)
	`)
	exec(t, db, `
		INSERT INTO tool_calls (tool_use_id, tool_name, decision_source, blocked_on_user_ms)
		VALUES ('tu-2', 'Bash', 'user_permanent', 200)
	`)
	exec(t, db, `
		INSERT INTO tool_calls (tool_use_id, tool_name, decision_source, blocked_on_user_ms)
		VALUES ('tu-3', 'Bash', NULL, 0)
	`)

	var total, userTemp, userPerm, blockedMs int64
	err := db.QueryRow(`
		SELECT total_calls, user_temporary_count, user_permanent_count, total_blocked_on_user_ms
		FROM v_permission_friction WHERE tool_name = 'Bash'
	`).Scan(&total, &userTemp, &userPerm, &blockedMs)
	if err != nil {
		t.Fatalf("query v_permission_friction: %v", err)
	}
	if total != 3 {
		t.Errorf("total_calls = %d, want 3", total)
	}
	if userTemp != 1 {
		t.Errorf("user_temporary_count = %d, want 1", userTemp)
	}
	if userPerm != 1 {
		t.Errorf("user_permanent_count = %d, want 1", userPerm)
	}
	if blockedMs != 300 {
		t.Errorf("total_blocked_on_user_ms = %d, want 300", blockedMs)
	}
}

func TestView_MCPInventory(t *testing.T) {
	db := openViewsTestDB(t)

	// github: connected once, called once.
	exec(t, db, `INSERT INTO mcp_server_connections (ts, server_name, status) VALUES (1000, 'github', 'connected')`)
	exec(t, db, `INSERT INTO tool_calls (tool_use_id, mcp_server_name, mcp_tool_name) VALUES ('tu-1', 'github', 'search')`)

	// ghost: called but never connected (e.g. connection event dropped/missed).
	exec(t, db, `INSERT INTO tool_calls (tool_use_id, mcp_server_name, mcp_tool_name) VALUES ('tu-2', 'ghost', 'fetch')`)

	rows, err := db.Query(`SELECT server_name, connection_count, tool_call_count, distinct_tools_called FROM v_mcp_inventory ORDER BY server_name`)
	if err != nil {
		t.Fatalf("query v_mcp_inventory: %v", err)
	}
	defer rows.Close()

	type row struct {
		name                                  string
		connections, toolCalls, distinctTools int64
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.name, &r.connections, &r.toolCalls, &r.distinctTools); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}

	if len(got) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(got), got)
	}
	if got[0].name != "ghost" || got[0].connections != 0 || got[0].toolCalls != 1 {
		t.Errorf("ghost row = %+v, want connections=0 tool_calls=1", got[0])
	}
	if got[1].name != "github" || got[1].connections != 1 || got[1].toolCalls != 1 || got[1].distinctTools != 1 {
		t.Errorf("github row = %+v, want connections=1 tool_calls=1 distinct_tools=1", got[1])
	}
}

func TestView_SessionSummary(t *testing.T) {
	db := openViewsTestDB(t)

	exec(t, db, `INSERT INTO sessions (session_id, started_at, ended_at) VALUES ('s2', 1000, 5000)`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id) VALUES (1000, 'claude_code.cost.usage', 1.5, 's2')`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id) VALUES (1000, 'claude_code.token.usage', 500, 's2')`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id, attrs) VALUES (1000, 'claude_code.lines_of_code.count', 10, 's2', '{"type":"added"}')`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id, attrs) VALUES (1000, 'claude_code.lines_of_code.count', 3, 's2', '{"type":"removed"}')`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id) VALUES (1000, 'claude_code.commit.count', 2, 's2')`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id) VALUES (1000, 'claude_code.pull_request.count', 1, 's2')`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id, attrs) VALUES (1000, 'claude_code.active_time.total', 60, 's2', '{"type":"user"}')`)
	exec(t, db, `INSERT INTO metric_points (ts, name, value, session_id, attrs) VALUES (1000, 'claude_code.active_time.total', 30, 's2', '{"type":"cli"}')`)
	exec(t, db, `INSERT INTO tool_calls (tool_use_id, session_id) VALUES ('tu-1', 's2')`)
	exec(t, db, `INSERT INTO api_requests (session_id, ts, query_source) VALUES ('s2', 1000, 'compact')`)

	var durationMs, totalTokens, linesAdded, linesRemoved, commits, prs, activeUser, activeCLI, toolCalls, compactions int64
	var totalCost float64
	err := db.QueryRow(`
		SELECT duration_ms, total_cost_usd, total_tokens, lines_added, lines_removed,
			commit_count, pull_request_count, active_time_user_s, active_time_cli_s,
			tool_call_count, compaction_count
		FROM v_session_summary WHERE session_id = 's2'
	`).Scan(&durationMs, &totalCost, &totalTokens, &linesAdded, &linesRemoved,
		&commits, &prs, &activeUser, &activeCLI, &toolCalls, &compactions)
	if err != nil {
		t.Fatalf("query v_session_summary: %v", err)
	}

	if durationMs != 4000 {
		t.Errorf("duration_ms = %d, want 4000", durationMs)
	}
	if totalCost != 1.5 {
		t.Errorf("total_cost_usd = %v, want 1.5", totalCost)
	}
	if totalTokens != 500 {
		t.Errorf("total_tokens = %d, want 500", totalTokens)
	}
	if linesAdded != 10 || linesRemoved != 3 {
		t.Errorf("lines_added/removed = %d/%d, want 10/3", linesAdded, linesRemoved)
	}
	if commits != 2 || prs != 1 {
		t.Errorf("commits/prs = %d/%d, want 2/1", commits, prs)
	}
	if activeUser != 60 || activeCLI != 30 {
		t.Errorf("active_time_user_s/cli_s = %d/%d, want 60/30", activeUser, activeCLI)
	}
	if toolCalls != 1 {
		t.Errorf("tool_call_count = %d, want 1", toolCalls)
	}
	if compactions != 1 {
		t.Errorf("compaction_count = %d, want 1", compactions)
	}
}

func TestView_ModelPerformance(t *testing.T) {
	db := openViewsTestDB(t)

	ttfts := []int64{100, 200, 300, 400}
	successes := []int64{1, 1, 1, 0}
	for i, ttft := range ttfts {
		exec(t, db, `
			INSERT INTO api_requests (model, ttft_ms, duration_ms, success)
			VALUES ('claude-3-opus', ?, ?, ?)
		`, ttft, ttft*2, successes[i])
	}

	var totalRequests, errorCount int64
	var errorRate, p50TTFT, p95TTFT, p50Duration, p95Duration float64
	err := db.QueryRow(`
		SELECT total_requests, error_count, error_rate, p50_ttft_ms, p95_ttft_ms, p50_duration_ms, p95_duration_ms
		FROM v_model_performance WHERE model = 'claude-3-opus'
	`).Scan(&totalRequests, &errorCount, &errorRate, &p50TTFT, &p95TTFT, &p50Duration, &p95Duration)
	if err != nil {
		t.Fatalf("query v_model_performance: %v", err)
	}

	if totalRequests != 4 {
		t.Errorf("total_requests = %d, want 4", totalRequests)
	}
	if errorCount != 1 {
		t.Errorf("error_count = %d, want 1", errorCount)
	}
	if errorRate != 0.25 {
		t.Errorf("error_rate = %v, want 0.25", errorRate)
	}
	if p50TTFT != 300 {
		t.Errorf("p50_ttft_ms = %v, want 300", p50TTFT)
	}
	if p95TTFT != 400 {
		t.Errorf("p95_ttft_ms = %v, want 400", p95TTFT)
	}
	if p50Duration != 600 {
		t.Errorf("p50_duration_ms = %v, want 600", p50Duration)
	}
	if p95Duration != 800 {
		t.Errorf("p95_duration_ms = %v, want 800", p95Duration)
	}
}
