package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/durfmt"
)

// This file loads the raw rows the per-session detectors (insight_detect.go)
// work on. Everything is loaded once per request, scoped to a time window by
// session start time, and kept in plain Go slices so the detectors stay pure
// functions over data: no SQL logic is duplicated per detector.

// promptRow is one user prompt (interactions).
type promptRow struct {
	PromptID  string
	SessionID string
	Seq       int64
	StartedAt int64
	Text      string
}

// toolRow is one tool call, flattened from tool_calls with its parsed input.
type toolRow struct {
	ID             string
	PromptID       string
	SessionID      string
	Ts             int64
	Name           string
	MCPServer      string
	File           string
	Command        string
	Decision       string
	DecisionSource string
	ResultBytes    int64
	Success        int64 // 1 ok, 0 failed, -1 unknown
	Offset         int64 // 0 when the read was not paginated
	Limit          int64 // 0 when the read was not paginated (whole file)
}

// reqRow is one API request (api_requests).
type reqRow struct {
	PromptID    string
	SessionID   string
	Ts          int64
	Model       string
	QuerySource string
	Input       int64
	Output      int64
	CacheRead   int64
	CacheCreate int64
	Context     int64 // input + cache read + cache creation: what the model read
	CostUSD     float64
}

// Tokens is every token the request was billed for, output included.
func (r reqRow) Tokens() int64 {
	return r.Input + r.Output + r.CacheRead + r.CacheCreate
}

// compactRow is one compaction (compactions, migration 0013).
type compactRow struct {
	ID        int64
	SessionID string
	Ts        int64
	Pre       int64
	Post      int64
}

// sessionRow is one session in scope.
type sessionRow struct {
	ID         string
	StartedAt  int64
	EndedAt    int64
	ProjectDir string
}

// waitRow is one hand-approval wait, from v_user_waits.
type waitRow struct {
	ToolUseID string
	SessionID string
	WaitMs    int64
	IdleMs    int64
}

// insightData is everything the detectors read for one request.
type insightData struct {
	cutoffMs    int64
	sessions    map[string]sessionRow
	prompts     []promptRow // ordered by session, seq
	tools       []toolRow   // ordered by ts
	toolByID    map[string]toolRow
	reqs        []reqRow
	compactions []compactRow
	waits       map[string]waitRow // keyed by tool_use_id
	blendedRate float64            // USD per token, this install's own rate
}

// scopeClause restricts a table's session_id column to sessions started at or
// after cutoffMs. cutoffMs == 0 means all time.
func scopeClause(cutoffMs int64) (string, []any) {
	if cutoffMs == 0 {
		return "1=1", nil
	}
	return "session_id IN (SELECT session_id FROM sessions WHERE started_at >= ?)", []any{cutoffMs}
}

// loadInsightData reads every table the detectors need for sessions started
// at or after cutoffMs.
func loadInsightData(ctx context.Context, db *sql.DB, cutoffMs int64) (*insightData, error) {
	d := &insightData{
		cutoffMs: cutoffMs,
		sessions: map[string]sessionRow{},
		toolByID: map[string]toolRow{},
		waits:    map[string]waitRow{},
	}
	scope, args := scopeClause(cutoffMs)

	// sessions
	sessWhere, sessArgs := "1=1", []any{}
	if cutoffMs != 0 {
		sessWhere, sessArgs = "started_at >= ?", []any{cutoffMs}
	}
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, COALESCE(started_at,0), COALESCE(ended_at,0), COALESCE(project_dir,'')
		FROM sessions WHERE `+sessWhere, sessArgs...)
	if err != nil {
		return nil, fmt.Errorf("load sessions: %w", err)
	}
	for rows.Next() {
		var s sessionRow
		if err := rows.Scan(&s.ID, &s.StartedAt, &s.EndedAt, &s.ProjectDir); err != nil {
			rows.Close()
			return nil, err
		}
		d.sessions[s.ID] = s
	}
	rows.Close()

	// prompts
	rows, err = db.QueryContext(ctx, `
		SELECT prompt_id, session_id, COALESCE(seq,0), COALESCE(started_at,0), COALESCE(prompt_text,'')
		FROM interactions WHERE `+scope+` ORDER BY session_id, seq`, args...)
	if err != nil {
		return nil, fmt.Errorf("load prompts: %w", err)
	}
	for rows.Next() {
		var p promptRow
		if err := rows.Scan(&p.PromptID, &p.SessionID, &p.Seq, &p.StartedAt, &p.Text); err != nil {
			rows.Close()
			return nil, err
		}
		d.prompts = append(d.prompts, p)
	}
	rows.Close()

	// tools
	rows, err = db.QueryContext(ctx, `
		SELECT tool_use_id, COALESCE(prompt_id,''), COALESCE(session_id,''), COALESCE(ts,0),
			COALESCE(tool_name,''), COALESCE(mcp_server_name,''), COALESCE(file_path,''),
			COALESCE(full_command,''), COALESCE(tool_input,''), COALESCE(decision,''), COALESCE(decision_source,''),
			COALESCE(result_size_bytes,0), COALESCE(success,-1)
		FROM tool_calls
		WHERE tool_name IS NOT NULL AND `+scope+`
		ORDER BY ts, tool_use_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("load tools: %w", err)
	}
	for rows.Next() {
		var t toolRow
		var input string
		if err := rows.Scan(&t.ID, &t.PromptID, &t.SessionID, &t.Ts, &t.Name, &t.MCPServer,
			&t.File, &t.Command, &input, &t.Decision, &t.DecisionSource, &t.ResultBytes, &t.Success); err != nil {
			rows.Close()
			return nil, err
		}
		parseToolInput(input, &t)
		d.tools = append(d.tools, t)
		d.toolByID[t.ID] = t
	}
	rows.Close()

	// api requests
	rows, err = db.QueryContext(ctx, `
		SELECT COALESCE(prompt_id,''), COALESCE(session_id,''), COALESCE(ts,0), COALESCE(model,''),
			COALESCE(query_source,''), COALESCE(input_tokens,0), COALESCE(output_tokens,0),
			COALESCE(cache_read_tokens,0), COALESCE(cache_creation_tokens,0),
			COALESCE(cost_usd_micros,0)/1000000.0
		FROM api_requests WHERE `+scope+` ORDER BY ts`, args...)
	if err != nil {
		return nil, fmt.Errorf("load requests: %w", err)
	}
	var usd float64
	var tokens int64
	for rows.Next() {
		var r reqRow
		if err := rows.Scan(&r.PromptID, &r.SessionID, &r.Ts, &r.Model, &r.QuerySource,
			&r.Input, &r.Output, &r.CacheRead, &r.CacheCreate, &r.CostUSD); err != nil {
			rows.Close()
			return nil, err
		}
		r.Context = r.Input + r.CacheRead + r.CacheCreate
		d.reqs = append(d.reqs, r)
		usd += r.CostUSD
		tokens += r.Tokens()
	}
	rows.Close()
	if tokens > 0 {
		d.blendedRate = usd / float64(tokens)
	}

	// compactions
	rows, err = db.QueryContext(ctx, `
		SELECT id, session_id, ts, COALESCE(pre_tokens,0), COALESCE(post_tokens,0)
		FROM compactions WHERE `+scope+` ORDER BY ts`, args...)
	if err != nil {
		return nil, fmt.Errorf("load compactions: %w", err)
	}
	for rows.Next() {
		var c compactRow
		if err := rows.Scan(&c.ID, &c.SessionID, &c.Ts, &c.Pre, &c.Post); err != nil {
			rows.Close()
			return nil, err
		}
		d.compactions = append(d.compactions, c)
	}
	rows.Close()

	// hand-approval waits, from the view so the rule lives in one place
	rows, err = db.QueryContext(ctx, `
		SELECT tool_use_id, session_id, wait_ms, idle_ms FROM v_user_waits WHERE `+scope, args...)
	if err != nil {
		return nil, fmt.Errorf("load waits: %w", err)
	}
	for rows.Next() {
		var w waitRow
		if err := rows.Scan(&w.ToolUseID, &w.SessionID, &w.WaitMs, &w.IdleMs); err != nil {
			rows.Close()
			return nil, err
		}
		d.waits[w.ToolUseID] = w
	}
	rows.Close()

	return d, nil
}

// parseToolInput fills the file path and pagination window of a tool call
// from its raw JSON input. A column value wins over the parsed one.
func parseToolInput(raw string, t *toolRow) {
	if raw == "" {
		return
	}
	var in struct {
		FilePath string `json:"file_path"`
		Offset   *int64 `json:"offset"`
		Limit    *int64 `json:"limit"`
	}
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return
	}
	if t.File == "" {
		t.File = in.FilePath
	}
	if in.Offset != nil {
		t.Offset = *in.Offset
	}
	if in.Limit != nil {
		t.Limit = *in.Limit
	}
}

// durationSince maps the dashboard's range tokens to a cutoff in epoch ms.
// Accepted: "" or "all" (0 = no cutoff), "today", "7d", "30d", "3m" (90
// days), and "install" (the first session ever captured; see ui_state).
func durationSince(ctx context.Context, db *sql.DB, token string) (int64, error) {
	now := time.Now()
	switch token {
	case "", "all":
		return 0, nil
	case "today":
		y, m, dd := now.Date()
		return time.Date(y, m, dd, 0, 0, 0, 0, now.Location()).UnixMilli(), nil
	case "3m":
		return now.Add(-90 * 24 * time.Hour).UnixMilli(), nil
	case "install":
		var v sql.NullString
		err := db.QueryRowContext(ctx, `SELECT value FROM ui_state WHERE key='install_at'`).Scan(&v)
		if err == sql.ErrNoRows || !v.Valid {
			return 0, nil
		}
		if err != nil {
			return 0, err
		}
		var ms int64
		if _, err := fmt.Sscan(v.String, &ms); err != nil {
			return 0, nil
		}
		return ms, nil
	}
	d, err := durfmt.ParseSince(token)
	if err != nil {
		return 0, fmt.Errorf("unknown range %q (use today, 7d, 30d, 3m, install, all)", token)
	}
	return now.Add(-d).UnixMilli(), nil
}
