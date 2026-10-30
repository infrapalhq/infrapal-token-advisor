package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/infrapalhq/infrapal-token-advisor/internal/durfmt"
)

// minRejectedToolCount is the floor before the rejected-work finding fires
// at all: one rejection isn't a pattern.
const minRejectedToolCount = 2

// minApprovalBlockedMs is the floor of total non-block-by-design blocked
// time before the permission_friction finding fires at all.
const minApprovalBlockedMs = int64(60 * time.Second / time.Millisecond)

// minFatPayloadBytes is the average per-call result size above which a
// tool is flagged as returning a "fat" payload.
const minFatPayloadBytes = 20 * 1024

// maxAllowRuleEntries caps how many patterns go into one suggested
// settings.json diff — past this it stops being a diff you'd actually
// paste in and review.
const maxAllowRuleEntries = 10

// blockByDesignTools are tools whose entire purpose is to wait on the user
// (a plan approval, a clarifying question) — their blocked_on_user_ms is
// the tool working as intended, not friction, so they're excluded from the
// permission_friction finding.
var blockByDesignTools = map[string]bool{
	"AskUserQuestion": true,
	"ExitPlanMode":    true,
}

// Impact prices a Finding in whichever units are actually knowable —
// every field is a pointer so a finding that can't honestly be priced in
// tokens/minutes/dollars renders no figure for that unit rather than a
// fabricated zero.
type Impact struct {
	TokensEst *int64   `json:"tokens_est,omitempty"`
	Minutes   *float64 `json:"minutes,omitempty"`
	USD       *float64 `json:"usd,omitempty"`
	Estimated bool     `json:"estimated"`
	Note      string   `json:"note,omitempty"`
}

// VizPoint is one bar/segment in a Finding's inline mini-visualization.
type VizPoint struct {
	Label string  `json:"label"`
	A     float64 `json:"a"`
	B     float64 `json:"b,omitempty"`
}

// Viz is a Finding's inline, ~60px-tall breakdown — never a full chart.
type Viz struct {
	Type   string     `json:"type"` // "stacked" | "bar"
	Unit   string     `json:"unit"` // "bytes" | "minutes" | "count" — how to format each point's value
	Points []VizPoint `json:"points"`
}

// Finding is one computed, actionable callout on the Fix page: an evidence
// line, a priced impact, a copy-pasteable fix, and enough detail to act on
// it. ID is stable per finding kind (never per row) — the Fix page shows at
// most one card per kind, so finding_state can key off it directly.
type Finding struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Severity string `json:"severity"` // "bad" | "warn" | "good"
	Title    string `json:"title"`
	Evidence string `json:"evidence"`
	Impact   Impact `json:"impact"`
	Fix      string `json:"fix,omitempty"`
	FixLang  string `json:"fix_lang,omitempty"`
	Link     string `json:"link,omitempty"`
	Viz      *Viz   `json:"viz,omitempty"`
	State    string `json:"state"` // "open" | "dismissed" | "snoozed" | "fixed"
}

// traceLink builds the deep-link into a specific interaction on the Trace
// page.
func traceLink(sessionID, promptID string) string {
	if promptID == "" {
		return "/trace?session=" + sessionID
	}
	return "/trace?session=" + sessionID + "&prompt=" + promptID
}

// handleFindings handles GET /api/findings, returning every finding this
// install can back with a runnable fix — a shell command, a settings.json
// permission diff, or a CLAUDE.md snippet — with its persisted state joined
// in. A signal that's real but has no concrete lever (which model handled
// a background call, an aggregate cache-hit ratio) isn't a Finding at all;
// it's a Spend-page stat computed straight off a view instead. Every
// detector here queries all-time data — none of the remaining checks are
// meaningfully scoped by a date range, so unlike the rest of the API this
// endpoint takes no ?since=.
func (s *Server) handleFindings(w http.ResponseWriter, r *http.Request) {
	findings, err := computeFindings(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	states, err := loadFindingStates(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now().UnixMilli()
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		f.State = resolveFindingState(states[f.ID], now)
		out = append(out, f)
	}

	sortFindingsByImpact(out)
	writeJSON(w, http.StatusOK, out)
}

// handlePostFindingState handles POST /api/findings/{id}/state, body
// {"state": "dismissed"|"snoozed"|"fixed", "snooze_days": 7}. This is
// infrapal-token-advisor's first write endpoint over an otherwise read-only API — see the
// package doc.
func (s *Server) handlePostFindingState(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing finding id")
		return
	}

	var body struct {
		State      string `json:"state"`
		SnoozeDays int    `json:"snooze_days"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}

	var snoozedUntil any
	switch body.State {
	case "dismissed", "fixed":
		snoozedUntil = nil
	case "snoozed":
		days := body.SnoozeDays
		if days <= 0 {
			days = 7
		}
		snoozedUntil = time.Now().Add(time.Duration(days) * 24 * time.Hour).UnixMilli()
	default:
		writeError(w, http.StatusBadRequest, "state must be one of: dismissed, snoozed, fixed")
		return
	}

	_, err := s.db.ExecContext(r.Context(), `
		INSERT INTO finding_state (finding_id, state, snoozed_until, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(finding_id) DO UPDATE SET
			state = excluded.state,
			snoozed_until = excluded.snoozed_until,
			updated_at = excluded.updated_at`,
		id, body.State, snoozedUntil, time.Now().UnixMilli(),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "state": body.State})
}

// handleFindingsActivity handles GET /api/findings/activity?since=7d,
// returning how many findings were dismissed vs. actually marked fixed in
// the window — a plain, verifiable count. This replaces an earlier
// "recoverable tokens over time" line chart that, in practice, just
// plotted how much you'd used the product recently: it fell left-to-right
// on every install regardless of whether anything got fixed, because it
// summed token impact by the week it was *incurred*, and recent weeks
// always have less accumulated history. A real fix doesn't make the past
// disappear, so a chart that implied it could was worse than no chart.
func (s *Server) handleFindingsActivity(w http.ResponseWriter, r *http.Request) {
	since := r.URL.Query().Get("since")
	if since == "" {
		since = "7d"
	}
	d, err := durfmt.ParseSince(since)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	cutoffMs := time.Now().Add(-d).UnixMilli()

	// SUM() over zero matching rows returns SQL NULL, not 0 — a fresh
	// install with nothing dismissed/fixed yet hits this every time, so
	// both columns scan into nullable ints rather than plain int64.
	var fixed, dismissed sql.NullInt64
	err = s.db.QueryRowContext(r.Context(), `
		SELECT
			SUM(CASE WHEN state = 'fixed' THEN 1 ELSE 0 END),
			SUM(CASE WHEN state = 'dismissed' THEN 1 ELSE 0 END)
		FROM finding_state
		WHERE updated_at >= ?`, cutoffMs,
	).Scan(&fixed, &dismissed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"fixed": fixed.Int64, "dismissed": dismissed.Int64})
}

// loadFindingStates reads every persisted finding_state row into a map
// keyed by finding_id.
func loadFindingStates(ctx context.Context, db *sql.DB) (map[string]findingStateRow, error) {
	rows, err := db.QueryContext(ctx, `SELECT finding_id, state, snoozed_until FROM finding_state`)
	if err != nil {
		return nil, fmt.Errorf("load finding states: %w", err)
	}
	defer rows.Close()
	out := map[string]findingStateRow{}
	for rows.Next() {
		var id, state string
		var snoozedUntil sql.NullInt64
		if err := rows.Scan(&id, &state, &snoozedUntil); err != nil {
			return nil, fmt.Errorf("scan finding state: %w", err)
		}
		out[id] = findingStateRow{state: state, snoozedUntil: snoozedUntil}
	}
	return out, rows.Err()
}

type findingStateRow struct {
	state        string
	snoozedUntil sql.NullInt64
}

// resolveFindingState turns a persisted row into the state a finding
// renders with — a lapsed snooze reverts to "open" rather than staying
// snoozed forever.
func resolveFindingState(row findingStateRow, nowMs int64) string {
	if row.state == "" {
		return "open"
	}
	if row.state == "snoozed" && row.snoozedUntil.Valid && row.snoozedUntil.Int64 <= nowMs {
		return "open"
	}
	return row.state
}

// sortFindingsByImpact orders findings worst-first: by USD impact, then
// minutes, then tokens, so the single highest-priced finding across
// whichever units it happens to be priced in leads the page.
func sortFindingsByImpact(findings []Finding) {
	score := func(f Finding) float64 {
		var s float64
		if f.Impact.USD != nil {
			s += *f.Impact.USD * 1000
		}
		if f.Impact.Minutes != nil {
			s += *f.Impact.Minutes
		}
		if f.Impact.TokensEst != nil {
			s += float64(*f.Impact.TokensEst) / 1000
		}
		return s
	}
	for i := 1; i < len(findings); i++ {
		for j := i; j > 0 && score(findings[j-1]) < score(findings[j]); j-- {
			findings[j-1], findings[j] = findings[j], findings[j-1]
		}
	}
}

// computeFindings runs every registered detector and returns the
// qualifying Findings, state not yet resolved. Shared by handleFindings
// and AI Mode's context builder (internal/api/ai.go).
func computeFindings(ctx context.Context, db *sql.DB) ([]Finding, error) {
	findings := []Finding{}

	// Every detector here is required to leave Fix empty when it can't back
	// itself with a real command or config diff — see each function's own
	// comment for what "real" means for that check. A detector that can't
	// meet that bar isn't registered here at all; its signal, if still
	// worth showing, is computed by a SQL view the Spend page reads
	// directly (v_session_cache_health, v_ancillary_model_spend — migration
	// 0011) instead of being routed through the Finding/Impact machinery.
	checks := []func(context.Context, *sql.DB) (*Finding, error){
		deadMCPFinding,
		permissionFrictionFinding,
		rejectedWorkFinding,
		fatPayloadFinding,
	}
	for _, check := range checks {
		f, err := check(ctx, db)
		if err != nil {
			return nil, err
		}
		if f != nil {
			if f.Fix == "" {
				return nil, fmt.Errorf("finding %q qualified with no fix — every registered detector must set Fix or return nil", f.ID)
			}
			findings = append(findings, *f)
		}
	}

	return findings, nil
}

// deadMCPFinding flags every connected-but-never-used MCP server as one
// aggregate finding — their tool definitions ride along on every request
// regardless of whether any one of them is called.
func deadMCPFinding(ctx context.Context, db *sql.DB) (*Finding, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT server_name, connection_count, tool_call_count
		FROM v_mcp_inventory
		WHERE connection_count > 0 AND tool_call_count = 0
		ORDER BY connection_count DESC`)
	if err != nil {
		return nil, fmt.Errorf("dead mcp finding: %w", err)
	}
	defer rows.Close()

	var servers []string
	var points []VizPoint
	for rows.Next() {
		var name string
		var connections, calls int64
		if err := rows.Scan(&name, &connections, &calls); err != nil {
			return nil, fmt.Errorf("dead mcp finding scan: %w", err)
		}
		servers = append(servers, name)
		points = append(points, VizPoint{Label: name, A: float64(connections), B: float64(calls)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("dead mcp finding rows: %w", err)
	}
	if len(servers) == 0 {
		return nil, nil
	}

	var fixLines []string
	for _, s := range servers {
		fixLines = append(fixLines, fmt.Sprintf("claude mcp remove %q", s))
	}

	evidence := fmt.Sprintf(
		"Connected but never had a tool called: %s. Each one's tool definitions still ride along on every request.",
		strings.Join(servers, ", "),
	)

	return &Finding{
		ID:       "dead_mcp",
		Kind:     "dead_mcp",
		Severity: "bad",
		Title:    fmt.Sprintf("%d MCP server%s you never call", len(servers), plural(int64(len(servers)))),
		Evidence: evidence,
		Impact: Impact{
			Note: "MCP tool-definition tokens aren't reported separately by Claude Code, so they can't be priced here — shown instead as connections vs. calls actually made.",
		},
		Fix:     strings.Join(fixLines, "\n"),
		FixLang: "bash",
		Link:    "/spend",
		Viz:     &Viz{Type: "stacked", Unit: "count", Points: points},
	}, nil
}

// permissionFrictionFinding aggregates time spent blocked on the user's
// approval across every tool except the ones designed to block (a plan
// approval, a clarifying question) — those aren't friction, they're the
// tool working as intended. Its Fix is always a real settings.json
// permissions diff built from patterns you've already hand-approved at
// least once (buildAllowRuleFix) — if none exist, there is nothing
// concrete to suggest, so the finding doesn't fire at all rather than
// falling back to advice.
func permissionFrictionFinding(ctx context.Context, db *sql.DB) (*Finding, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT tool_name, mcp_server_name, mcp_tool_name, total_blocked_on_user_ms
		FROM v_permission_friction
		WHERE total_blocked_on_user_ms > 0
		ORDER BY total_blocked_on_user_ms DESC`)
	if err != nil {
		return nil, fmt.Errorf("permission friction finding: %w", err)
	}
	defer rows.Close()

	type row struct {
		label     string
		blockedMs int64
	}
	var qualifying []row
	var totalMs int64
	for rows.Next() {
		var toolName string
		var mcpServer, mcpTool sql.NullString
		var blockedMs int64
		if err := rows.Scan(&toolName, &mcpServer, &mcpTool, &blockedMs); err != nil {
			return nil, fmt.Errorf("permission friction finding scan: %w", err)
		}
		if blockByDesignTools[toolName] {
			continue
		}
		label := toolName
		if mcpServer.Valid && mcpServer.String != "" {
			tool := mcpTool.String
			if tool == "" {
				tool = "?"
			}
			label = mcpServer.String + " · " + tool
		}
		qualifying = append(qualifying, row{label: label, blockedMs: blockedMs})
		totalMs += blockedMs
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("permission friction finding rows: %w", err)
	}
	if totalMs < minApprovalBlockedMs || len(qualifying) == 0 {
		return nil, nil
	}

	fix, ruleCount, err := buildAllowRuleFix(ctx, db)
	if err != nil {
		return nil, err
	}
	if fix == "" {
		return nil, nil
	}

	minutes := float64(totalMs) / 60000
	evidence := fmt.Sprintf(
		"%s spent waiting on your approval — %d pattern%s below were already hand-approved at least once (excluding tools designed to wait, like plan approval or clarifying questions).",
		formatMinutesRounded(minutes), ruleCount, plural(int64(ruleCount)),
	)

	maxPoints := len(qualifying)
	if maxPoints > 8 {
		maxPoints = 8
	}
	points := make([]VizPoint, 0, maxPoints)
	for _, r := range qualifying[:maxPoints] {
		points = append(points, VizPoint{Label: r.label, A: float64(r.blockedMs) / 60000})
	}

	return &Finding{
		ID:       "permission_friction",
		Kind:     "permission_friction",
		Severity: "warn",
		Title:    fmt.Sprintf("You're the bottleneck — %s", formatMinutesRounded(minutes)),
		Evidence: evidence,
		Impact: Impact{
			Minutes: &minutes,
		},
		Fix:     fix,
		FixLang: "settings-json",
		Link:    "/spend",
		Viz:     &Viz{Type: "bar", Unit: "minutes", Points: points},
	}, nil
}

// permissionsPatch marshals to the exact shape ~/.claude/settings.json
// expects under "permissions" — pasted in directly, not paraphrased into
// prose, per the rule that a Fix has to be something you can run or apply.
type permissionsPatch struct {
	Permissions struct {
		Allow []string `json:"allow,omitempty"`
		Deny  []string `json:"deny,omitempty"`
	} `json:"permissions"`
}

// buildAllowRuleFix collects every pattern the user has already hand-
// approved at least once — Bash calls grouped to a stable command prefix
// (normalizeBashPrefix), any other tool by its exact name, excluding
// blockByDesignTools (permanently allowing AskUserQuestion or ExitPlanMode
// would defeat their whole purpose) — and renders them as one
// settings.json permissions.allow diff. Every entry here was a real
// decision you already made once; promoting it to permanent just stops
// asking you again. Returns "" (no error) when nothing qualifies.
func buildAllowRuleFix(ctx context.Context, db *sql.DB) (fixJSON string, ruleCount int, err error) {
	rows, err := db.QueryContext(ctx, `
		SELECT tool_name, full_command, COALESCE(blocked_on_user_ms, 0)
		FROM tool_calls
		WHERE decision_source = 'user_temporary' AND tool_name IS NOT NULL`)
	if err != nil {
		return "", 0, fmt.Errorf("build allow rule fix: %w", err)
	}
	defer rows.Close()

	type group struct {
		pattern   string
		count     int
		blockedMs int64
	}
	groups := map[string]*group{}
	var order []string
	add := func(pattern string, blockedMs int64) {
		if pattern == "" {
			return
		}
		g, ok := groups[pattern]
		if !ok {
			g = &group{pattern: pattern}
			groups[pattern] = g
			order = append(order, pattern)
		}
		g.count++
		g.blockedMs += blockedMs
	}
	for rows.Next() {
		var toolName string
		var fullCommand sql.NullString
		var blockedMs int64
		if err := rows.Scan(&toolName, &fullCommand, &blockedMs); err != nil {
			return "", 0, fmt.Errorf("build allow rule fix scan: %w", err)
		}
		if blockByDesignTools[toolName] {
			// AskUserQuestion/ExitPlanMode are designed to always interrupt
			// — a one-off temporary allow on either is not a pattern to
			// make permanent, that would defeat the tool's purpose.
			continue
		}
		if toolName == "Bash" {
			if !fullCommand.Valid || fullCommand.String == "" {
				continue
			}
			prefix := normalizeBashPrefix(fullCommand.String)
			if prefix == "" {
				continue
			}
			add(fmt.Sprintf("Bash(%s:*)", prefix), blockedMs)
		} else {
			add(toolName, blockedMs)
		}
	}
	if err := rows.Err(); err != nil {
		return "", 0, fmt.Errorf("build allow rule fix rows: %w", err)
	}
	if len(order) == 0 {
		return "", 0, nil
	}

	// Worst (most blocked time) first, so the diff leads with what actually
	// mattered if it has to be truncated to maxAllowRuleEntries.
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && groups[order[j-1]].blockedMs < groups[order[j]].blockedMs; j-- {
			order[j-1], order[j] = order[j], order[j-1]
		}
	}
	if len(order) > maxAllowRuleEntries {
		order = order[:maxAllowRuleEntries]
	}

	var patch permissionsPatch
	for _, pattern := range order {
		patch.Permissions.Allow = append(patch.Permissions.Allow, pattern)
	}
	encoded, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		return "", 0, fmt.Errorf("build allow rule fix encode: %w", err)
	}
	return string(encoded), len(order), nil
}

// rejectedWorkFinding fires only when the same tool has been rejected
// repeatedly (topCount >= minRejectedToolCount) — a genuine pattern with a
// real deny-rule to suggest, not just an isolated rejection. wasted_cost_usd
// is still summed across every rejected prompt for the evidence line (the
// whole picture), but the fix only names the tool that's actually a repeat
// offender.
func rejectedWorkFinding(ctx context.Context, db *sql.DB) (*Finding, error) {
	var topTool string
	var topCount int64
	err := db.QueryRowContext(ctx, `
		SELECT tool_name, COUNT(*) AS n
		FROM tool_calls
		WHERE decision = 'reject' AND tool_name IS NOT NULL
		GROUP BY tool_name
		ORDER BY n DESC LIMIT 1`,
	).Scan(&topTool, &topCount)
	if err == sql.ErrNoRows || topCount < minRejectedToolCount {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("rejected work finding top tool: %w", err)
	}

	var totalUSD sql.NullFloat64
	var totalRejected sql.NullInt64
	err = db.QueryRowContext(ctx, `
		SELECT SUM(wasted_cost_usd), SUM(rejected_tool_count) FROM v_wasted_spend`,
	).Scan(&totalUSD, &totalRejected)
	if err != nil {
		return nil, fmt.Errorf("rejected work finding: %w", err)
	}

	var sessionID string
	err = db.QueryRowContext(ctx, `
		SELECT session_id FROM tool_calls WHERE decision = 'reject' AND tool_name = ?
		ORDER BY ts DESC LIMIT 1`, topTool,
	).Scan(&sessionID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("rejected work finding session lookup: %w", err)
	}

	var patch permissionsPatch
	patch.Permissions.Deny = []string{topTool}
	encoded, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("rejected work finding encode: %w", err)
	}

	usd := totalUSD.Float64
	evidence := fmt.Sprintf(
		"`%s` alone has been rejected %d times. %s spent across every prompt with a rejected call this install has recorded — the whole turn's API cost is billed even when the proposed action gets turned down.",
		topTool, topCount, formatCostForDetail(usd),
	)

	return &Finding{
		ID:       "rejected_work",
		Kind:     "rejected_work",
		Severity: "warn",
		Title:    fmt.Sprintf("You keep rejecting %s", topTool),
		Evidence: evidence,
		Impact: Impact{
			USD: &usd,
		},
		Fix:     string(encoded),
		FixLang: "settings-json",
		Link:    traceLink(sessionID, ""),
	}, nil
}

// blendedUSDPerToken computes this install's own real cost-per-token —
// SUM(cost)/SUM(tokens) across every api_request that has both — so a
// token estimate derived from a different signal (payload bytes) can be
// converted to a dollar figure without inventing a price. Returns 0 when
// there's no priced traffic to derive a rate from.
func blendedUSDPerToken(ctx context.Context, db *sql.DB) (float64, error) {
	var usd, tokens sql.NullFloat64
	err := db.QueryRowContext(ctx, `
		SELECT
			SUM(COALESCE(cost_usd_micros,0)) / 1000000.0,
			SUM(COALESCE(input_tokens,0)+COALESCE(output_tokens,0)+COALESCE(cache_read_tokens,0)+COALESCE(cache_creation_tokens,0))
		FROM api_requests`,
	).Scan(&usd, &tokens)
	if err != nil {
		return 0, fmt.Errorf("blended usd per token: %w", err)
	}
	if !tokens.Valid || tokens.Float64 == 0 {
		return 0, nil
	}
	return usd.Float64 / tokens.Float64, nil
}

// fatPayloadFinding flags tools whose average per-call result size crosses
// minFatPayloadBytes — the biggest single lever for context bloat once
// dead MCP servers are ruled out.
func fatPayloadFinding(ctx context.Context, db *sql.DB) (*Finding, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT tool_name, mcp_server_name, mcp_tool_name, call_count, total_result_bytes, avg_result_bytes
		FROM v_tool_payload
		WHERE avg_result_bytes >= ?
		ORDER BY total_result_bytes DESC`, minFatPayloadBytes)
	if err != nil {
		return nil, fmt.Errorf("fat payload finding: %w", err)
	}
	defer rows.Close()

	type row struct {
		label      string
		callCount  int64
		totalBytes int64
		avgBytes   float64
	}
	var qualifying []row
	var totalBytes int64
	for rows.Next() {
		var toolName string
		var mcpServer, mcpTool sql.NullString
		var callCount, total int64
		var avg float64
		if err := rows.Scan(&toolName, &mcpServer, &mcpTool, &callCount, &total, &avg); err != nil {
			return nil, fmt.Errorf("fat payload finding scan: %w", err)
		}
		label := toolName
		if mcpServer.Valid && mcpServer.String != "" {
			tool := mcpTool.String
			if tool == "" {
				tool = "?"
			}
			label = mcpServer.String + " · " + tool
		}
		qualifying = append(qualifying, row{label: label, callCount: callCount, totalBytes: total, avgBytes: avg})
		totalBytes += total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fat payload finding rows: %w", err)
	}
	if len(qualifying) == 0 {
		return nil, nil
	}

	top := qualifying[0]
	tokensEst := totalBytes / 4

	rate, err := blendedUSDPerToken(ctx, db)
	if err != nil {
		return nil, err
	}
	usd := float64(tokensEst) * rate

	evidence := fmt.Sprintf(
		"Across %d calls (%s total) — the largest per-call payload of any tool.",
		top.callCount, formatBytesForDetail(float64(top.totalBytes)),
	)

	maxPoints := len(qualifying)
	if maxPoints > 6 {
		maxPoints = 6
	}
	points := make([]VizPoint, 0, maxPoints)
	for _, r := range qualifying[:maxPoints] {
		points = append(points, VizPoint{Label: r.label, A: r.avgBytes})
	}

	fix := fmt.Sprintf(
		"Add to CLAUDE.md:\n\nAvoid reading whole files or dumping full tool output into context — use `%s` with a narrower selector, a bounded offset/limit, or Grep instead.",
		top.label,
	)

	return &Finding{
		ID:       "fat_payload",
		Kind:     "fat_payload",
		Severity: "warn",
		Title:    fmt.Sprintf("%s is dumping %s a call", top.label, formatBytesForDetail(top.avgBytes)),
		Evidence: evidence,
		Impact: Impact{
			USD:       &usd,
			TokensEst: &tokensEst,
			Estimated: true,
			Note:      "result_tokens isn't populated by this install — tokens estimated from result_size_bytes at ~4 bytes/token, then priced at this install's own blended $/token rate.",
		},
		Fix:     fix,
		FixLang: "claude-md",
		Link:    "/spend",
		Viz:     &Viz{Type: "bar", Unit: "bytes", Points: points},
	}, nil
}

// normalizeBashPrefix reduces a full Bash command to its first two
// whitespace-separated tokens (e.g. "npm run build --watch" -> "npm run"),
// a heuristic stand-in for a stable command "shape" — not a validated
// Claude Code permission-rule pattern. The second token is only kept when it
// looks like a stable subcommand name (isSubcommandLike) — otherwise (e.g.
// "cd /Users/me/My Project", where the second token is an
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

// formatMinutesRounded renders a minute count the way a headline reads it
// ("17 min", "1h 20m") — rounder than a precise "Xm Ys", since the point is
// a punchy title, not a stopwatch reading.
func formatMinutesRounded(minutes float64) string {
	total := int64(minutes + 0.5)
	if total < 60 {
		if total < 1 {
			total = 1
		}
		return fmt.Sprintf("%d min", total)
	}
	hours := total / 60
	mins := total % 60
	return fmt.Sprintf("%dh %dm", hours, mins)
}

// formatCostForDetail renders a USD float as "$X.XX" for plain-text
// insight sentences.
func formatCostForDetail(usd float64) string {
	return fmt.Sprintf("$%.2f", usd)
}

// formatBytesForDetail renders a byte count as a human-readable size for
// plain-text insight sentences.
func formatBytesForDetail(n float64) string {
	if n < 1024 {
		return fmt.Sprintf("%.0f B", n)
	}
	kb := n / 1024
	if kb < 1024 {
		return fmt.Sprintf("%.1f KB", kb)
	}
	return fmt.Sprintf("%.2f MB", kb/1024)
}
