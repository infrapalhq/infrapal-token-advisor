package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ShamikMukhopadhyay/ccotel/internal/durfmt"
)

// orphanSessionThresholdMs flags sessions running longer than this as likely
// orphaned (left running, not real elapsed work) rather than a genuine long
// session.
const orphanSessionThresholdMs = int64(4 * time.Hour / time.Millisecond)

// minBashAllowRuleCount is the minimum number of temporary-allow occurrences
// of the same command prefix before ccotel suggests it as a permanent
// allow-rule — below this, a single one-off command isn't worth flagging.
const minBashAllowRuleCount = 2

// minRejectedToolCount mirrors minBashAllowRuleCount for the
// frequently-rejected-tool insight: one rejection isn't a pattern.
const minRejectedToolCount = 2

// minApprovalWaitMs is the floor before a single prompt's blocked_on_user_ms
// is worth calling out on its own (distinct from bashAllowRuleInsight, which
// looks at recurring command patterns rather than a single worst prompt).
const minApprovalWaitMs = int64(60 * time.Second / time.Millisecond)

// Insight is a single computed, actionable callout: a specific prompt,
// session, or server flagged as worth a look, with enough detail to act on
// it and a link to inspect it further. Unlike the /api/views/* pass-throughs
// (views.go), this endpoint's queries pick a winner across the whole
// dataset and write a narrative sentence — genuine business logic, kept in
// this one file rather than folded into the "thin API" view handlers.
type Insight struct {
	ID       string `json:"id"`
	Category string `json:"category"` // "efficiency" | "sessions" | "waste" | "tools"
	Severity string `json:"severity"` // "info" | "warn"
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	Link     string `json:"link"` // e.g. "/waterfall?session=..&prompt=.." or "/tools"
}

// waterfallLink builds the deep-link into a specific interaction, the same
// URL shape the frontend used to construct client-side.
func waterfallLink(sessionID, promptID string) string {
	if promptID == "" {
		return "/waterfall?session=" + sessionID
	}
	return "/waterfall?session=" + sessionID + "&prompt=" + promptID
}

// handleInsights handles GET /api/insights?since=, returning zero or more
// computed callouts. Each check below only emits an Insight when a row
// genuinely qualifies — nothing is fabricated when the data doesn't
// support it.
func (s *Server) handleInsights(w http.ResponseWriter, r *http.Request) {
	var cutoffMs int64
	var haveCutoff bool
	if since := r.URL.Query().Get("since"); since != "" {
		d, err := durfmt.ParseSince(since)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cutoffMs = time.Now().Add(-d).UnixMilli()
		haveCutoff = true
	}

	ctx := r.Context()
	insights := []Insight{}

	checks := []func(context.Context, *sql.DB, int64, bool) (*Insight, error){
		topCostPromptInsight,
		hitMaxTokensInsight,
		compactionInsight,
		longSessionInsight,
		longApprovalWaitInsight,
	}
	for _, check := range checks {
		ins, err := check(ctx, s.db, cutoffMs, haveCutoff)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ins != nil {
			insights = append(insights, *ins)
		}
	}

	// The remaining checks aren't time-scoped (bashAllowRuleInsight and
	// frequentlyRejectedToolInsight look at all-time permission decisions;
	// deadMCPServerInsight is a point-in-time inventory snapshot), so they
	// don't take the cutoff args.
	untimedChecks := []func(context.Context, *sql.DB) (*Insight, error){
		bashAllowRuleInsight,
		frequentlyRejectedToolInsight,
		deadMCPServerInsight,
	}
	for _, check := range untimedChecks {
		ins, err := check(ctx, s.db)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ins != nil {
			insights = append(insights, *ins)
		}
	}

	writeJSON(w, http.StatusOK, insights)
}

// topCostPromptInsight flags the single most expensive prompt (by
// v_prompt_efficiency.total_cost_usd) in range, if any prompt has nonzero
// cost.
func topCostPromptInsight(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool) (*Insight, error) {
	query := `SELECT prompt_id, session_id, total_cost_usd, total_tokens, llm_turn_count, rejected_count, tool_error_count
		FROM v_prompt_efficiency WHERE total_cost_usd > 0`
	var args []any
	if haveCutoff {
		query += " AND started_at >= ?"
		args = append(args, cutoffMs)
	}
	query += " ORDER BY total_cost_usd DESC LIMIT 1"

	var promptID, sessionID string
	var costUsd float64
	var totalTokens, turnCount, rejected, toolErrors int64
	err := db.QueryRowContext(ctx, query, args...).
		Scan(&promptID, &sessionID, &costUsd, &totalTokens, &turnCount, &rejected, &toolErrors)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("top cost prompt insight: %w", err)
	}

	detail := fmt.Sprintf("$%.2f across %d turn%s (%s tokens) — your highest-cost prompt in this range.",
		costUsd, turnCount, plural(turnCount), formatWithCommas(totalTokens))
	if rejected > 0 {
		detail += fmt.Sprintf(" %d tool call%s rejected.", rejected, plural(rejected))
	}
	if toolErrors > 0 {
		detail += fmt.Sprintf(" %d tool call%s errored.", toolErrors, plural(toolErrors))
	}

	return &Insight{
		ID:       "top_cost_prompt:" + promptID,
		Category: "efficiency",
		Severity: "info",
		Title:    "Highest-cost prompt",
		Detail:   detail,
		Link:     waterfallLink(sessionID, promptID),
	}, nil
}

// hitMaxTokensInsight flags the most recent prompt whose response was cut
// off by the max-tokens limit — a truncated answer the user may not have
// noticed was incomplete.
func hitMaxTokensInsight(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool) (*Insight, error) {
	query := `SELECT prompt_id, session_id FROM v_prompt_efficiency WHERE hit_max_tokens = 1`
	var args []any
	if haveCutoff {
		query += " AND started_at >= ?"
		args = append(args, cutoffMs)
	}
	query += " ORDER BY started_at DESC LIMIT 1"

	var promptID, sessionID string
	err := db.QueryRowContext(ctx, query, args...).Scan(&promptID, &sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("hit max tokens insight: %w", err)
	}

	return &Insight{
		ID:       "hit_max_tokens:" + promptID,
		Category: "efficiency",
		Severity: "warn",
		Title:    "Response hit the token limit",
		Detail:   "This prompt's response was cut off by the max-tokens limit — consider raising max_tokens or splitting the task into smaller prompts.",
		Link:     waterfallLink(sessionID, promptID),
	}, nil
}

// compactionInsight flags the most recent prompt that triggered
// conversation compaction — a signal the session ran long enough for
// context to be rebuilt, which costs tokens and can lose earlier detail.
func compactionInsight(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool) (*Insight, error) {
	query := `SELECT prompt_id, session_id FROM v_prompt_efficiency WHERE triggered_compaction = 1`
	var args []any
	if haveCutoff {
		query += " AND started_at >= ?"
		args = append(args, cutoffMs)
	}
	query += " ORDER BY started_at DESC LIMIT 1"

	var promptID, sessionID string
	err := db.QueryRowContext(ctx, query, args...).Scan(&promptID, &sessionID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("compaction insight: %w", err)
	}

	return &Insight{
		ID:       "compaction:" + promptID,
		Category: "efficiency",
		Severity: "info",
		Title:    "Conversation triggered compaction",
		Detail:   "This conversation ran long enough to trigger auto-compaction — starting a fresh session for unrelated work avoids repeatedly recompacting the same context.",
		Link:     waterfallLink(sessionID, promptID),
	}, nil
}

// longSessionInsight flags the longest session in range if it exceeds
// orphanSessionThresholdMs — almost always a process left running rather
// than real elapsed work, and a distortion on any duration average. Also
// resolves that session's first interaction (by seq) so the resulting link
// opens something concrete in Waterfall, since Waterfall itself ignores a
// bare ?session= with no ?prompt=.
func longSessionInsight(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool) (*Insight, error) {
	query := `SELECT session_id, duration_ms FROM v_session_summary WHERE duration_ms > ?`
	args := []any{orphanSessionThresholdMs}
	if haveCutoff {
		query += " AND started_at >= ?"
		args = append(args, cutoffMs)
	}
	query += " ORDER BY duration_ms DESC LIMIT 1"

	var sessionID string
	var durationMs int64
	err := db.QueryRowContext(ctx, query, args...).Scan(&sessionID, &durationMs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("long session insight: %w", err)
	}

	var promptID string
	err = db.QueryRowContext(ctx,
		`SELECT prompt_id FROM interactions WHERE session_id = ? ORDER BY seq LIMIT 1`, sessionID,
	).Scan(&promptID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("long session insight prompt lookup: %w", err)
	}

	minutes := durationMs / 60000
	detail := fmt.Sprintf(
		"This session ran %dm — likely left open rather than %dm of real work. Sessions this long skew any duration average.",
		minutes, minutes,
	)

	return &Insight{
		ID:       "long_session:" + sessionID,
		Category: "sessions",
		Severity: "warn",
		Title:    "Session ran unusually long",
		Detail:   detail,
		Link:     waterfallLink(sessionID, promptID),
	}, nil
}

// longApprovalWaitInsight flags the single prompt that spent the most time
// blocked on user approval, if it exceeds minApprovalWaitMs — complements
// bashAllowRuleInsight (which looks at recurring command patterns) by
// surfacing the single worst-affected prompt instead.
func longApprovalWaitInsight(ctx context.Context, db *sql.DB, cutoffMs int64, haveCutoff bool) (*Insight, error) {
	query := `SELECT prompt_id, session_id, blocked_on_user_ms FROM v_prompt_efficiency WHERE blocked_on_user_ms > ?`
	args := []any{minApprovalWaitMs}
	if haveCutoff {
		query += " AND started_at >= ?"
		args = append(args, cutoffMs)
	}
	query += " ORDER BY blocked_on_user_ms DESC LIMIT 1"

	var promptID, sessionID string
	var blockedMs int64
	err := db.QueryRowContext(ctx, query, args...).Scan(&promptID, &sessionID, &blockedMs)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("long approval wait insight: %w", err)
	}

	d := time.Duration(blockedMs) * time.Millisecond
	detail := fmt.Sprintf(
		"This prompt spent %dm %ds waiting on your approval — the longest single wait in this range.",
		int64(d/time.Minute), int64((d%time.Minute)/time.Second),
	)

	return &Insight{
		ID:       "long_approval_wait:" + promptID,
		Category: "waste",
		Severity: "info",
		Title:    "Long approval wait",
		Detail:   detail,
		Link:     waterfallLink(sessionID, promptID),
	}, nil
}

// bashAllowRuleInsight groups every temporarily-allowed Bash call by its
// command prefix (normalizeBashPrefix) and, if the most common prefix
// occurs at least minBashAllowRuleCount times, suggests it as a concrete
// permanent allow-rule to add to settings.json — the drill-down
// v_permission_friction's tool-name-only grouping can't provide (see that
// view's own comment on having no data source finer than tool identity).
func bashAllowRuleInsight(ctx context.Context, db *sql.DB) (*Insight, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT full_command, session_id, prompt_id, COALESCE(blocked_on_user_ms, 0)
		FROM tool_calls
		WHERE tool_name = 'Bash' AND decision_source = 'user_temporary'
			AND full_command IS NOT NULL AND full_command != ''`)
	if err != nil {
		return nil, fmt.Errorf("bash allow rule insight: %w", err)
	}
	defer rows.Close()

	type group struct {
		count     int
		blockedMs int64
		sessionID string
		promptID  string
	}
	groups := map[string]*group{}
	var order []string
	for rows.Next() {
		var fullCommand, sessionID, promptID string
		var blockedMs int64
		if err := rows.Scan(&fullCommand, &sessionID, &promptID, &blockedMs); err != nil {
			return nil, fmt.Errorf("bash allow rule insight scan: %w", err)
		}
		prefix := normalizeBashPrefix(fullCommand)
		if prefix == "" {
			continue
		}
		g, ok := groups[prefix]
		if !ok {
			g = &group{sessionID: sessionID, promptID: promptID}
			groups[prefix] = g
			order = append(order, prefix)
		}
		g.count++
		g.blockedMs += blockedMs
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("bash allow rule insight rows: %w", err)
	}

	var topPrefix string
	var top *group
	for _, prefix := range order {
		g := groups[prefix]
		if top == nil || g.count > top.count {
			top, topPrefix = g, prefix
		}
	}
	if top == nil || top.count < minBashAllowRuleCount {
		return nil, nil
	}

	detail := fmt.Sprintf("You've temporarily allowed `%s ...` %d times%s. Add `Bash(%s:*)` to your permanent allow list.",
		topPrefix, top.count, formatMsRough(top.blockedMs), topPrefix)

	return &Insight{
		ID:       "bash_allow_rule:" + topPrefix,
		Category: "waste",
		Severity: "info",
		Title:    "Allow-rule to add",
		Detail:   detail,
		Link:     waterfallLink(top.sessionID, top.promptID),
	}, nil
}

// frequentlyRejectedToolInsight flags the tool with the most outright
// rejections (tool_calls.decision = 'reject'), if it's been rejected at
// least minRejectedToolCount times — distinct from bashAllowRuleInsight,
// which only looks at accepted-but-temporary Bash calls. Any tool can show
// up here, not just Bash.
func frequentlyRejectedToolInsight(ctx context.Context, db *sql.DB) (*Insight, error) {
	var toolName string
	var count int64
	err := db.QueryRowContext(ctx, `
		SELECT tool_name, COUNT(*) AS n
		FROM tool_calls
		WHERE decision = 'reject' AND tool_name IS NOT NULL
		GROUP BY tool_name
		ORDER BY n DESC LIMIT 1`,
	).Scan(&toolName, &count)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("frequently rejected tool insight: %w", err)
	}
	if count < minRejectedToolCount {
		return nil, nil
	}

	var sessionID, promptID string
	err = db.QueryRowContext(ctx, `
		SELECT session_id, COALESCE(prompt_id, '') FROM tool_calls
		WHERE decision = 'reject' AND tool_name = ?
		ORDER BY ts DESC LIMIT 1`, toolName,
	).Scan(&sessionID, &promptID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("frequently rejected tool insight link lookup: %w", err)
	}

	detail := fmt.Sprintf("You've rejected `%s` %d times — consider a deny rule, or check why Claude keeps proposing it.",
		toolName, count)

	return &Insight{
		ID:       "frequently_rejected_tool:" + toolName,
		Category: "waste",
		Severity: "info",
		Title:    "Frequently rejected tool",
		Detail:   detail,
		Link:     waterfallLink(sessionID, promptID),
	}, nil
}

// deadMCPServerInsight flags the connected-but-never-used MCP server with
// the most connections (the worst offender: repeatedly reconnecting for
// zero actual use) — the same "dead config" signal the Tools & MCP page
// already describes, surfaced as a concrete callout instead of a table row
// the user has to notice themselves.
func deadMCPServerInsight(ctx context.Context, db *sql.DB) (*Insight, error) {
	var serverName string
	var connectionCount int64
	err := db.QueryRowContext(ctx, `
		SELECT server_name, connection_count FROM v_mcp_inventory
		WHERE connection_count > 0 AND tool_call_count = 0
		ORDER BY connection_count DESC LIMIT 1`,
	).Scan(&serverName, &connectionCount)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dead mcp server insight: %w", err)
	}

	detail := fmt.Sprintf("`%s` connected %d time%s but never had a tool called — removing it saves tool-definition tokens on every request.",
		serverName, connectionCount, plural(connectionCount))

	return &Insight{
		ID:       "dead_mcp_server:" + serverName,
		Category: "tools",
		Severity: "info",
		Title:    "Unused MCP server",
		Detail:   detail,
		Link:     "/tools",
	}, nil
}

// normalizeBashPrefix reduces a full Bash command to its first two
// whitespace-separated tokens (e.g. "npm run build --watch" -> "npm run"),
// a heuristic stand-in for a stable command "shape" — not a validated
// Claude Code permission-rule pattern. The second token is only kept when it
// looks like a stable subcommand name (isSubcommandLike) — otherwise (e.g.
// "cd /Users/.../Agentic IDE Team", where the second token is an
// unbounded, possibly space-escaped path) it's dropped, since including a
// one-off path in the suggested rule would make it neither a sensible
// generalization nor a valid copy-pasteable pattern.
func normalizeBashPrefix(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 1 && isSubcommandLike(fields[1]) {
		return fields[0] + " " + fields[1]
	}
	return fields[0]
}

// isSubcommandLike reports whether tok looks like a stable subcommand name
// (letters, digits, '-', '_' only) rather than a path, flag, or other
// unbounded argument.
func isSubcommandLike(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// plural returns "s" for any n != 1, "" otherwise.
func plural(n int64) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// formatWithCommas renders n with thousands separators (e.g. 4893447 ->
// "4,893,447"), since Insight.Detail is plain text with no client-side
// number formatting applied to it.
func formatWithCommas(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// formatMsRough renders a millisecond duration as ", waiting Xm Ys total"
// (or "" for zero), for appending directly into a detail sentence.
func formatMsRough(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Duration(ms) * time.Millisecond
	minutes := int64(d / time.Minute)
	seconds := int64((d % time.Minute) / time.Second)
	if minutes > 0 {
		return fmt.Sprintf(", waiting %dm %ds total", minutes, seconds)
	}
	return fmt.Sprintf(", waiting %ds total", seconds)
}
