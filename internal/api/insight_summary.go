package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/infrapalhq/infrapal-token-advisor/internal/project"
)

// computeInsights loads the window's data and runs every detector.
func computeInsights(ctx context.Context, db *sql.DB, cutoffMs int64) (*insightData, []occurrence, error) {
	d, err := loadInsightData(ctx, db, cutoffMs)
	if err != nil {
		return nil, nil, err
	}
	var occ []occurrence
	occ = append(occ, detectReRead(d)...)
	occ = append(occ, detectCompaction(d)...)
	occ = append(occ, detectContextBloat(d)...)
	occ = append(occ, detectRepeatCommand(d)...)
	occ = append(occ, detectRepeatApproval(d)...)
	occ = append(occ, detectFailingMCP(d)...)
	occ = append(occ, detectManyToolCalls(d)...)
	occ = append(occ, detectToolFailures(d)...)
	occ = append(occ, detectOutputSpikes(d)...)
	occ = append(occ, detectRejectedWork(d)...)
	occ = append(occ, detectOversizedModel(d)...)
	occ = append(occ, detectProjectNotes(d)...)
	dead, err := deadMCPOccurrences(ctx, db)
	if err != nil {
		return nil, nil, err
	}
	occ = append(occ, dead...)
	sortOccurrences(occ)
	return d, occ, nil
}

// deadMCPOccurrences reports connected MCP servers that never had a tool
// called. Install-wide (the inventory view is all-time), so it has no session.
func deadMCPOccurrences(ctx context.Context, db *sql.DB) ([]occurrence, error) {
	servers, err := deadMCPServers(ctx, db)
	if err != nil {
		return nil, err
	}
	var out []occurrence
	for _, srv := range servers {
		name := srv.name
		out = append(out, occurrence{
			Kind:     "dead_mcp",
			Area:     "context",
			Subject:  name,
			Count:    1,
			Severity: "half",
			Conf:     "Measured",
			Title:    name + " connected but never used",
			Detail:   "Its tool definitions ride along on every request.",
			Fix:      fmt.Sprintf("claude mcp remove %q", name),
			FixLang:  "bash",
		})
	}
	return out, nil
}

// deadMCPServer is a connected MCP server with no tool calls.
type deadMCPServer struct {
	name        string
	connections int64
}

// deadMCPServers lists connected servers that never had a tool called, ordered
// by name. Connection events and tool calls can spell one server differently
// ("claude.ai Notion" and "claude_ai_Notion"), so names are matched normalized.
func deadMCPServers(ctx context.Context, db *sql.DB) ([]deadMCPServer, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT server_name, connection_count, tool_call_count FROM v_mcp_inventory
		ORDER BY server_name`)
	if err != nil {
		return nil, fmt.Errorf("dead mcp: %w", err)
	}
	defer rows.Close()
	var connected []deadMCPServer
	calls := map[string]int64{}
	for rows.Next() {
		var srv deadMCPServer
		var toolCalls int64
		if err := rows.Scan(&srv.name, &srv.connections, &toolCalls); err != nil {
			return nil, err
		}
		calls[normalizeMCPServer(srv.name)] += toolCalls
		if srv.connections > 0 {
			connected = append(connected, srv)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []deadMCPServer
	for _, srv := range connected {
		if calls[normalizeMCPServer(srv.name)] == 0 {
			out = append(out, srv)
		}
	}
	return out, nil
}

// normalizeMCPServer reduces a server name to lowercase letters and digits
// separated by underscores, the form tool names use.
func normalizeMCPServer(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return '_'
	}, name)
}

// sessionCard is one row of the Sessions page (SPEC §4.1).
type sessionCard struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Title     string    `json:"title"`
	StartedAt int64     `json:"started_at"`
	DurationS int64     `json:"duration_s"` // open time, first event to last
	ActiveS   int64     `json:"active_s"`   // time you or Claude Code were actually working
	Tokens    int64     `json:"tokens"`
	CostUSD   float64   `json:"cost_usd"`
	WaitMs    int64     `json:"wait_ms"`
	Model     string    `json:"model"`
	Models    []string  `json:"models"` // main-thread models, most spend first
	Areas     []areaPip `json:"areas"`
	Priority  string    `json:"priority"` // now | look | fine
	IsNew     bool      `json:"is_new"`
	Symptoms  []string  `json:"symptoms"`
	Issues    int       `json:"issues"`
}

// isEmpty reports a session where nothing happened: nothing billed, nothing flagged.
func (c sessionCard) isEmpty() bool {
	return c.Tokens == 0 && c.Issues == 0
}

// countSessions counts the cards that aren't empty.
func countSessions(cards []sessionCard) int {
	n := 0
	for _, c := range cards {
		if !c.isEmpty() {
			n++
		}
	}
	return n
}

// issueSymptoms maps an issue kind to the Symptom filter it shows up under.
var issueSymptoms = map[string]string{
	"re_read":         "repeats",
	"repeat_cmd":      "repeats",
	"tool_failures":   "failed",
	"failing_mcp":     "failed",
	"context_bloat":   "bloated",
	"many_tool_calls": "many_calls",
	"output_spike":    "spikes",
	"rejected_work":   "rejected",
}

// areaPip is one problem area on a session: its worst severity.
type areaPip struct {
	Area     string  `json:"area"`
	Severity string  `json:"severity"` // full | half
	USD      float64 `json:"usd,omitempty"`
}

// loadTitles returns each session's latest generated title (Claude Code's own
// generate_session_title output), keyed by session id.
func loadTitles(ctx context.Context, db *sql.DB, cutoffMs int64) (map[string]string, error) {
	scope, args := scopeClause(cutoffMs)
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, COALESCE(response_text,'') FROM assistant_responses
		WHERE query_source = 'generate_session_title' AND `+scope+`
		ORDER BY ts`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var sid, text string
		if err := rows.Scan(&sid, &text); err != nil {
			return nil, err
		}
		var parsed struct {
			Title string `json:"title"`
		}
		if json.Unmarshal([]byte(text), &parsed) == nil && parsed.Title != "" {
			out[sid] = parsed.Title
		}
	}
	return out, rows.Err()
}

// loadActiveTimes returns each session's active seconds, user and CLI combined,
// from the view so the rule lives in one place.
func loadActiveTimes(ctx context.Context, db *sql.DB, cutoffMs int64) (map[string]int64, error) {
	scope, args := scopeClause(cutoffMs)
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, active_time_user_s + active_time_cli_s FROM v_session_summary
		WHERE `+scope, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var sid string
		var active float64
		if err := rows.Scan(&sid, &active); err != nil {
			return nil, err
		}
		out[sid] = int64(active)
	}
	return out, rows.Err()
}

// loadLastVisit returns ui_state.last_visit_at, or 0 if never recorded.
func loadLastVisit(ctx context.Context, db *sql.DB) int64 {
	var v string
	if err := db.QueryRowContext(ctx, `SELECT value FROM ui_state WHERE key='last_visit_at'`).Scan(&v); err != nil {
		return 0
	}
	var ms int64
	fmt.Sscan(v, &ms)
	return ms
}

// buildCards produces one card per session, newest first.
func buildCards(ctx context.Context, db *sql.DB, d *insightData, occ []occurrence, cutoffMs int64) ([]sessionCard, error) {
	titles, err := loadTitles(ctx, db, cutoffMs)
	if err != nil {
		return nil, err
	}
	active, err := loadActiveTimes(ctx, db, cutoffMs)
	if err != nil {
		return nil, err
	}
	lastVisit := loadLastVisit(ctx, db)

	// per-session aggregates
	type agg struct {
		tokens, cost, waitMs int64
		costF                float64
		modelCost            map[string]float64
		tools, toolFails     int
		compacted            bool
		longWait             bool
		symptoms             map[string]bool
		areaSev              map[string]string
		areaUSD              map[string]float64
		issues               int
	}
	aggs := map[string]*agg{}
	get := func(sid string) *agg {
		a, ok := aggs[sid]
		if !ok {
			a = &agg{modelCost: map[string]float64{}, symptoms: map[string]bool{}, areaSev: map[string]string{}, areaUSD: map[string]float64{}}
			aggs[sid] = a
		}
		return a
	}
	for _, r := range d.reqs {
		a := get(r.SessionID)
		a.tokens += r.Tokens()
		a.costF += r.CostUSD
		if r.QuerySource == "repl_main_thread" || r.QuerySource == "" {
			a.modelCost[r.Model] += r.CostUSD
		}
	}
	for _, t := range d.tools {
		a := get(t.SessionID)
		a.tools++
		if t.Success == 0 {
			a.toolFails++
		}
		if w, ok := d.waits[t.ID]; ok {
			a.waitMs += w.WaitMs
			if w.WaitMs > 30_000 {
				a.longWait = true
			}
		}
	}
	for _, c := range d.compactions {
		get(c.SessionID).compacted = true
	}
	for _, o := range occ {
		if o.SessionID == "" {
			continue
		}
		a := get(o.SessionID)
		a.issues++
		if symptom, ok := issueSymptoms[o.Kind]; ok {
			a.symptoms[symptom] = true
		}
		if cur, ok := a.areaSev[o.Area]; !ok || (cur == "half" && o.Severity == "full") {
			a.areaSev[o.Area] = o.Severity
		}
		if o.USD != nil {
			a.areaUSD[o.Area] += *o.USD
		}
	}

	out := make([]sessionCard, 0, len(d.sessions))
	for sid, s := range d.sessions {
		a := get(sid)
		c := sessionCard{
			Areas:     []areaPip{},
			Symptoms:  []string{},
			ID:        sid,
			Project:   project.Name(s.ProjectDir),
			Title:     titles[sid],
			StartedAt: s.StartedAt,
			Tokens:    a.tokens,
			CostUSD:   a.costF,
			WaitMs:    a.waitMs,
			ActiveS:   active[sid],
			Model:     topModel(a.modelCost),
			Models:    modelsBySpend(a.modelCost),
			IsNew:     lastVisit > 0 && s.StartedAt > lastVisit,
			Issues:    a.issues,
		}
		if c.Title == "" {
			c.Title = firstPromptTitle(d, sid)
		}
		if s.EndedAt > s.StartedAt {
			c.DurationS = (s.EndedAt - s.StartedAt) / 1000
		}
		for _, area := range []string{"model", "context", "exec", "perm"} {
			if sev, ok := a.areaSev[area]; ok {
				c.Areas = append(c.Areas, areaPip{Area: area, Severity: sev, USD: a.areaUSD[area]})
			}
		}
		c.Priority = priorityOf(a.areaSev)
		if a.compacted {
			c.Symptoms = append(c.Symptoms, "compacted")
		}
		if a.symptoms["failed"] || (a.tools > 0 && float64(a.toolFails)/float64(a.tools) > 0.10) {
			c.Symptoms = append(c.Symptoms, "failed")
		}
		if a.longWait {
			c.Symptoms = append(c.Symptoms, "waits")
		}
		for _, symptom := range []string{"repeats", "bloated", "many_calls", "spikes", "rejected"} {
			if a.symptoms[symptom] {
				c.Symptoms = append(c.Symptoms, symptom)
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	return out, nil
}

// priorityOf maps a session's worst area severity to now | look | fine.
func priorityOf(areaSev map[string]string) string {
	worst := "fine"
	for _, sev := range areaSev {
		if sev == "full" {
			return "now"
		}
		if sev == "half" {
			worst = "look"
		}
	}
	return worst
}

// topModel returns the model with the most main-thread spend.
func topModel(costs map[string]float64) string {
	best, bestC := "", -1.0
	for m, c := range costs {
		if c > bestC || (c == bestC && m < best) {
			best, bestC = m, c
		}
	}
	return best
}

// modelsBySpend lists models with the most spend first.
func modelsBySpend(costs map[string]float64) []string {
	out := make([]string, 0, len(costs))
	for m := range costs {
		if m != "" {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if costs[out[i]] != costs[out[j]] {
			return costs[out[i]] > costs[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// firstPromptTitle is the fallback title when no generated one exists.
func firstPromptTitle(d *insightData, sid string) string {
	for _, p := range d.prompts {
		if p.SessionID == sid && strings.TrimSpace(p.Text) != "" {
			return truncate(strings.TrimSpace(p.Text), 60)
		}
	}
	return "Session · " + time.UnixMilli(d.sessions[sid].StartedAt).Format("Jan 2")
}
