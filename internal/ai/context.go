// Package ai implements ccotel's optional AI Mode: a Claude Haiku call that
// proposes recommendations on top of the same aggregated telemetry the rest
// of the dashboard already exposes. This is ccotel's one deliberate
// network-egress exception (see internal/api/ai.go) — everything in this
// package works only with numbers, names, and ids already vetted by the
// existing derived views and insight checks; it never touches raw prompt
// text, response text, tool input/output, file paths, or commands.
package ai

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Row caps per source view — "worst/most-costly first" (matching each
// view's natural sort elsewhere in the codebase), not an arbitrary prefix,
// so a capped payload still carries the highest-signal rows.
const (
	maxPromptRows  = 15
	maxWasteRows   = 15
	maxToolRows    = 25
	maxMCPRows     = 25
	maxSessionRows = 15
	maxModelRows   = 25
	maxPayloadRows = 25
	maxRetryRows   = 15
)

// InsightFact mirrors the fields of api.Insight the model is allowed to
// see. Deliberately excludes Link (a frontend route, not a fact) and ID (an
// internal key). Defined here rather than imported from internal/api to
// avoid an import cycle — internal/api/ai.go builds these by hand from the
// []api.Insight it already has via computeInsights.
type InsightFact struct {
	Category string `json:"category"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Detail   string `json:"detail"`
}

// PromptFact is one row of v_prompt_efficiency, highest cost first. Every
// field is a number, ratio, boolean, or an opaque id — no prompt/response
// content.
type PromptFact struct {
	PromptID            string   `json:"prompt_id"`
	PromptLength        *int64   `json:"prompt_length"`
	TotalCostUSD        float64  `json:"total_cost_usd"`
	TotalTokens         int64    `json:"total_tokens"`
	LLMTurnCount        int64    `json:"llm_turn_count"`
	CacheHitRatio       *float64 `json:"cache_hit_ratio"`
	ToolCallCount       int64    `json:"tool_call_count"`
	RejectedCount       int64    `json:"rejected_count"`
	ToolErrorCount      int64    `json:"tool_error_count"`
	BlockedOnUserMs     int64    `json:"blocked_on_user_ms"`
	TriggeredCompaction bool     `json:"triggered_compaction"`
	HitMaxTokens        bool     `json:"hit_max_tokens"`
}

// WasteFact is one row of v_wasted_spend, highest wasted cost first.
type WasteFact struct {
	SessionID         string  `json:"session_id"`
	Day               string  `json:"day"`
	WastedCostUSD     float64 `json:"wasted_cost_usd"`
	RejectedToolCount int64   `json:"rejected_tool_count"`
}

// ToolFact is one row of v_permission_friction, highest call volume first.
type ToolFact struct {
	ToolName             string   `json:"tool_name"`
	MCPServerName        *string  `json:"mcp_server_name"`
	MCPToolName          *string  `json:"mcp_tool_name"`
	TotalCalls           int64    `json:"total_calls"`
	UserTemporaryCount   int64    `json:"user_temporary_count"`
	UserPermanentCount   int64    `json:"user_permanent_count"`
	TotalBlockedOnUserMs int64    `json:"total_blocked_on_user_ms"`
	SuccessCount         int64    `json:"success_count"`
	ErrorCount           int64    `json:"error_count"`
	AvgDurationMs        *float64 `json:"avg_duration_ms"`
}

// MCPFact is one row of v_mcp_inventory, most connections first.
// last_connected_at is deliberately dropped: a timestamp adds no
// decision-relevant signal here and only bloats the payload.
type MCPFact struct {
	ServerName          string `json:"server_name"`
	ConnectionCount     int64  `json:"connection_count"`
	ToolCallCount       int64  `json:"tool_call_count"`
	DistinctToolsCalled int64  `json:"distinct_tools_called"`
}

// SessionFact is one row of v_session_summary, highest cost first.
// lines_added/removed, commit/PR counts, and active-time are dropped: not
// cost/friction-relevant to a recommendation engine, and dropping them
// keeps the payload smaller and more focused.
type SessionFact struct {
	SessionID    string  `json:"session_id"`
	DurationMs   int64   `json:"duration_ms"`
	TotalCostUSD float64 `json:"total_cost_usd"`
	// TotalTokens is a float64, not int64 like PromptFact's: it's summed
	// from metric_points.value (declared REAL), so SQLite's driver always
	// hands back a float64 here even though the values happen to be whole
	// numbers — scanning into an int64 fails with exactly that mismatch.
	TotalTokens     float64 `json:"total_tokens"`
	ToolCallCount   int64   `json:"tool_call_count"`
	CompactionCount int64   `json:"compaction_count"`
}

// ModelFact is one row of v_model_performance, most requests first.
type ModelFact struct {
	Model         string   `json:"model"`
	Effort        *string  `json:"effort"`
	Speed         *string  `json:"speed"`
	TotalRequests int64    `json:"total_requests"`
	ErrorRate     *float64 `json:"error_rate"`
	P50TTFTMs     *float64 `json:"p50_ttft_ms"`
	P95TTFTMs     *float64 `json:"p95_ttft_ms"`
	P50DurationMs *float64 `json:"p50_duration_ms"`
	P95DurationMs *float64 `json:"p95_duration_ms"`
}

// PayloadFact is one row of v_tool_payload, largest result payload first.
type PayloadFact struct {
	ToolName         string   `json:"tool_name"`
	MCPServerName    *string  `json:"mcp_server_name"`
	CallCount        int64    `json:"call_count"`
	TotalInputBytes  int64    `json:"total_input_bytes"`
	TotalResultBytes int64    `json:"total_result_bytes"`
	AvgResultBytes   *float64 `json:"avg_result_bytes"`
}

// RetryFact is one row of v_retry_waste, highest retried cost first.
type RetryFact struct {
	SessionID       string  `json:"session_id"`
	Day             string  `json:"day"`
	RetriedRequests int64   `json:"retried_requests"`
	RetriedCostUSD  float64 `json:"retried_cost_usd"`
}

// Rollups are small pre-computed sums across the (possibly row-capped)
// facts above, given to the model directly so it can cite a provided total
// rather than doing its own arithmetic over rows it can only partially
// see, and so ValidateEvidence has an anchor for "total X" style claims.
type Rollups struct {
	TotalWastedSpendUSD    float64 `json:"total_wasted_spend_usd_in_range"`
	TotalRejectedToolCalls int64   `json:"total_rejected_tool_calls_in_range"`
	TotalRetriedCostUSD    float64 `json:"total_retried_cost_usd_in_range"`
	SessionCountInRange    int64   `json:"session_count_in_range"`
}

// Context is the full JSON payload sent as the sole user-turn message to
// Anthropic. Every field is numeric, categorical, a name, or an opaque id —
// never prompt text, response text, tool input/output, file paths, or
// commands.
type Context struct {
	SinceRange string        `json:"since_range"`
	Insights   []InsightFact `json:"insights"`
	TopPrompts []PromptFact  `json:"top_cost_prompts"`
	Waste      []WasteFact   `json:"wasted_spend_by_session_day"`
	Tools      []ToolFact    `json:"tool_permission_friction"`
	MCP        []MCPFact     `json:"mcp_inventory"`
	Sessions   []SessionFact `json:"top_cost_sessions"`
	Models     []ModelFact   `json:"model_performance"`
	Payload    []PayloadFact `json:"tool_payload_sizes"`
	Retries    []RetryFact   `json:"retry_waste_by_session_day"`
	Rollups    Rollups       `json:"rollups"`
	Caveats    []string      `json:"data_caveats"`
}

// dataCaveats restates ccotel's README "gotchas" as instructions the model
// must respect, so it doesn't state false precision. Fixed, not
// data-dependent, so it never perturbs Context.Hash.
func dataCaveats() []string {
	return []string{
		"Cost figures are approximate estimates, not exact Anthropic billing amounts.",
		"Metric totals (tokens, cost, lines, active time) are sums of delta-temporality points, already aggregated for you — you are not shown a raw event stream.",
		"Session duration can include idle/orphaned time, not just active work.",
		"Only prompts/sessions with API-request data have a per-prompt cost figure.",
		"You have NOT been shown any prompt text, response text, tool input, tool output, file paths, or commands — only aggregate counts, costs, ratios, and durations.",
	}
}

// sinceFilter returns "" or " AND <col> >= ?" plus the cutoff arg, given a
// cutoffMs that's 0 when sinceRange is "" (all-time, no filter).
func sinceFilter(col string, cutoffMs int64, haveCutoff bool) (string, []any) {
	if !haveCutoff {
		return "", nil
	}
	return " AND " + col + " >= ?", []any{cutoffMs}
}

// BuildContext assembles the compact JSON bundle for one Generate call.
// insights is the already-computed, already-vetted fact list (converted
// from api.computeInsights's []Insight by the caller) — reused, never
// recomputed here, so this package inherits the same deterministic
// "don't fabricate" checks the rest of ccotel relies on. cutoffMs/haveCutoff
// mirror the ?since= cutoff every other view/insight query already uses.
func BuildContext(ctx context.Context, db *sql.DB, sinceRange string, cutoffMs int64, haveCutoff bool, insights []InsightFact) (Context, error) {
	c := Context{
		SinceRange: sinceRange,
		Insights:   insights,
		Caveats:    dataCaveats(),
	}

	if err := loadPrompts(ctx, db, cutoffMs, haveCutoff, &c.TopPrompts); err != nil {
		return Context{}, err
	}
	if err := loadWaste(ctx, db, cutoffMs, haveCutoff, &c.Waste); err != nil {
		return Context{}, err
	}
	if err := loadTools(ctx, db, &c.Tools); err != nil {
		return Context{}, err
	}
	if err := loadMCP(ctx, db, &c.MCP); err != nil {
		return Context{}, err
	}
	if err := loadSessions(ctx, db, cutoffMs, haveCutoff, &c.Sessions); err != nil {
		return Context{}, err
	}
	if err := loadModels(ctx, db, &c.Models); err != nil {
		return Context{}, err
	}
	if err := loadPayload(ctx, db, &c.Payload); err != nil {
		return Context{}, err
	}
	if err := loadRetries(ctx, db, cutoffMs, haveCutoff, &c.Retries); err != nil {
		return Context{}, err
	}
	if err := loadRollups(ctx, db, cutoffMs, haveCutoff, &c.Rollups); err != nil {
		return Context{}, err
	}

	return c, nil
}

func loadPrompts(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool, out *[]PromptFact) error {
	filter, args := sinceFilter("started_at", cutoffMs, haveCutoff)
	query := `SELECT prompt_id, prompt_length, total_cost_usd, total_tokens, llm_turn_count,
		cache_hit_ratio, tool_call_count, rejected_count, tool_error_count, blocked_on_user_ms,
		triggered_compaction, hit_max_tokens
		FROM v_prompt_efficiency WHERE 1=1` + filter + ` ORDER BY total_cost_usd DESC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, append(args, maxPromptRows)...)
	if err != nil {
		return fmt.Errorf("load prompt facts: %w", err)
	}
	defer rows.Close()

	facts := []PromptFact{}
	for rows.Next() {
		var f PromptFact
		var triggeredCompaction, hitMaxTokens int64
		if err := rows.Scan(&f.PromptID, &f.PromptLength, &f.TotalCostUSD, &f.TotalTokens, &f.LLMTurnCount,
			&f.CacheHitRatio, &f.ToolCallCount, &f.RejectedCount, &f.ToolErrorCount, &f.BlockedOnUserMs,
			&triggeredCompaction, &hitMaxTokens); err != nil {
			return fmt.Errorf("scan prompt fact: %w", err)
		}
		f.TriggeredCompaction = triggeredCompaction != 0
		f.HitMaxTokens = hitMaxTokens != 0
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadWaste(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool, out *[]WasteFact) error {
	// v_wasted_spend.day is a date string ("YYYY-MM-DD"), not the epoch-ms
	// column the other views filter on — compare against the same cutoff
	// rendered as a date so the "?since=" semantics stay consistent.
	filter, args := "", []any(nil)
	if haveCutoff {
		filter = " AND day >= date(?/1000, 'unixepoch')"
		args = []any{cutoffMs}
	}
	query := `SELECT session_id, day, wasted_cost_usd, rejected_tool_count
		FROM v_wasted_spend WHERE 1=1` + filter + ` ORDER BY wasted_cost_usd DESC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, append(args, maxWasteRows)...)
	if err != nil {
		return fmt.Errorf("load waste facts: %w", err)
	}
	defer rows.Close()

	facts := []WasteFact{}
	for rows.Next() {
		var f WasteFact
		if err := rows.Scan(&f.SessionID, &f.Day, &f.WastedCostUSD, &f.RejectedToolCount); err != nil {
			return fmt.Errorf("scan waste fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadTools(ctx context.Context, db *sql.DB, out *[]ToolFact) error {
	rows, err := db.QueryContext(ctx, `SELECT tool_name, mcp_server_name, mcp_tool_name, total_calls,
		user_temporary_count, user_permanent_count, total_blocked_on_user_ms, success_count, error_count,
		avg_duration_ms
		FROM v_permission_friction ORDER BY total_calls DESC LIMIT ?`, maxToolRows)
	if err != nil {
		return fmt.Errorf("load tool facts: %w", err)
	}
	defer rows.Close()

	facts := []ToolFact{}
	for rows.Next() {
		var f ToolFact
		if err := rows.Scan(&f.ToolName, &f.MCPServerName, &f.MCPToolName, &f.TotalCalls,
			&f.UserTemporaryCount, &f.UserPermanentCount, &f.TotalBlockedOnUserMs, &f.SuccessCount,
			&f.ErrorCount, &f.AvgDurationMs); err != nil {
			return fmt.Errorf("scan tool fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadMCP(ctx context.Context, db *sql.DB, out *[]MCPFact) error {
	rows, err := db.QueryContext(ctx, `SELECT server_name, connection_count, tool_call_count, distinct_tools_called
		FROM v_mcp_inventory ORDER BY connection_count DESC LIMIT ?`, maxMCPRows)
	if err != nil {
		return fmt.Errorf("load mcp facts: %w", err)
	}
	defer rows.Close()

	facts := []MCPFact{}
	for rows.Next() {
		var f MCPFact
		if err := rows.Scan(&f.ServerName, &f.ConnectionCount, &f.ToolCallCount, &f.DistinctToolsCalled); err != nil {
			return fmt.Errorf("scan mcp fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadSessions(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool, out *[]SessionFact) error {
	filter, args := sinceFilter("started_at", cutoffMs, haveCutoff)
	query := `SELECT session_id, duration_ms, total_cost_usd, total_tokens, tool_call_count, compaction_count
		FROM v_session_summary WHERE 1=1` + filter + ` ORDER BY total_cost_usd DESC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, append(args, maxSessionRows)...)
	if err != nil {
		return fmt.Errorf("load session facts: %w", err)
	}
	defer rows.Close()

	facts := []SessionFact{}
	for rows.Next() {
		var f SessionFact
		if err := rows.Scan(&f.SessionID, &f.DurationMs, &f.TotalCostUSD, &f.TotalTokens, &f.ToolCallCount,
			&f.CompactionCount); err != nil {
			return fmt.Errorf("scan session fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadModels(ctx context.Context, db *sql.DB, out *[]ModelFact) error {
	rows, err := db.QueryContext(ctx, `SELECT model, effort, speed, total_requests, error_rate,
		p50_ttft_ms, p95_ttft_ms, p50_duration_ms, p95_duration_ms
		FROM v_model_performance ORDER BY total_requests DESC LIMIT ?`, maxModelRows)
	if err != nil {
		return fmt.Errorf("load model facts: %w", err)
	}
	defer rows.Close()

	facts := []ModelFact{}
	for rows.Next() {
		var f ModelFact
		if err := rows.Scan(&f.Model, &f.Effort, &f.Speed, &f.TotalRequests, &f.ErrorRate,
			&f.P50TTFTMs, &f.P95TTFTMs, &f.P50DurationMs, &f.P95DurationMs); err != nil {
			return fmt.Errorf("scan model fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadPayload(ctx context.Context, db *sql.DB, out *[]PayloadFact) error {
	rows, err := db.QueryContext(ctx, `SELECT tool_name, mcp_server_name, call_count, total_input_bytes,
		total_result_bytes, avg_result_bytes
		FROM v_tool_payload ORDER BY total_result_bytes DESC LIMIT ?`, maxPayloadRows)
	if err != nil {
		return fmt.Errorf("load payload facts: %w", err)
	}
	defer rows.Close()

	facts := []PayloadFact{}
	for rows.Next() {
		var f PayloadFact
		if err := rows.Scan(&f.ToolName, &f.MCPServerName, &f.CallCount, &f.TotalInputBytes,
			&f.TotalResultBytes, &f.AvgResultBytes); err != nil {
			return fmt.Errorf("scan payload fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadRetries(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool, out *[]RetryFact) error {
	filter, args := "", []any(nil)
	if haveCutoff {
		filter = " AND day >= date(?/1000, 'unixepoch')"
		args = []any{cutoffMs}
	}
	query := `SELECT session_id, day, retried_requests, retried_cost_usd
		FROM v_retry_waste WHERE 1=1` + filter + ` ORDER BY retried_cost_usd DESC LIMIT ?`
	rows, err := db.QueryContext(ctx, query, append(args, maxRetryRows)...)
	if err != nil {
		return fmt.Errorf("load retry facts: %w", err)
	}
	defer rows.Close()

	facts := []RetryFact{}
	for rows.Next() {
		var f RetryFact
		if err := rows.Scan(&f.SessionID, &f.Day, &f.RetriedRequests, &f.RetriedCostUSD); err != nil {
			return fmt.Errorf("scan retry fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	*out = facts
	return nil
}

func loadRollups(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool, out *Rollups) error {
	wasteFilter, wasteArgs := "", []any(nil)
	if haveCutoff {
		wasteFilter = " WHERE day >= date(?/1000, 'unixepoch')"
		wasteArgs = []any{cutoffMs}
	}
	var wastedTotal sql.NullFloat64
	var rejectedTotal sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT SUM(wasted_cost_usd), SUM(rejected_tool_count) FROM v_wasted_spend`+wasteFilter,
		wasteArgs...,
	).Scan(&wastedTotal, &rejectedTotal); err != nil {
		return fmt.Errorf("load waste rollup: %w", err)
	}

	retryFilter, retryArgs := "", []any(nil)
	if haveCutoff {
		retryFilter = " WHERE day >= date(?/1000, 'unixepoch')"
		retryArgs = []any{cutoffMs}
	}
	var retriedTotal sql.NullFloat64
	if err := db.QueryRowContext(ctx,
		`SELECT SUM(retried_cost_usd) FROM v_retry_waste`+retryFilter,
		retryArgs...,
	).Scan(&retriedTotal); err != nil {
		return fmt.Errorf("load retry rollup: %w", err)
	}

	sessionFilter, sessionArgs := "", []any(nil)
	if haveCutoff {
		sessionFilter = " WHERE started_at >= ?"
		sessionArgs = []any{cutoffMs}
	}
	var sessionCount sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM v_session_summary`+sessionFilter,
		sessionArgs...,
	).Scan(&sessionCount); err != nil {
		return fmt.Errorf("load session rollup: %w", err)
	}

	*out = Rollups{
		TotalWastedSpendUSD:    wastedTotal.Float64,
		TotalRejectedToolCalls: rejectedTotal.Int64,
		TotalRetriedCostUSD:    retriedTotal.Float64,
		SessionCountInRange:    sessionCount.Int64,
	}
	return nil
}

// Hash returns a stable sha256 hex digest of c's canonical JSON encoding —
// a change-detection fingerprint for the cache key, not a security
// boundary. Deterministic because every slice field is populated by a
// fixed ORDER BY.
func (c Context) Hash() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("hash context: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
