package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

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
	occ = append(occ, detectRepeatCommand(d)...)
	occ = append(occ, detectRepeatApproval(d)...)
	occ = append(occ, detectFailingMCP(d)...)
	occ = append(occ, detectOversizedModel(d)...)
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
	rows, err := db.QueryContext(ctx, `
		SELECT server_name FROM v_mcp_inventory
		WHERE connection_count > 0 AND tool_call_count = 0
		ORDER BY server_name`)
	if err != nil {
		return nil, fmt.Errorf("dead mcp: %w", err)
	}
	defer rows.Close()
	var out []occurrence
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
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
	return out, rows.Err()
}

// sessionCard is one row of the Sessions page (SPEC §4.1).
type sessionCard struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	Title     string    `json:"title"`
	StartedAt int64     `json:"started_at"`
	DurationS int64     `json:"duration_s"`
	Tokens    int64     `json:"tokens"`
	CostUSD   float64   `json:"cost_usd"`
	WaitMs    int64     `json:"wait_ms"`
	Model     string    `json:"model"`
	Areas     []areaPip `json:"areas"`
	Priority  string    `json:"priority"` // now | look | fine
	IsNew     bool      `json:"is_new"`
	Symptoms  []string  `json:"symptoms"`
	Issues    int       `json:"issues"`
}

// areaPip is one problem area on a session: its worst severity.
type areaPip struct {
	Area     string `json:"area"`
	Severity string `json:"severity"` // full | half
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
	lastVisit := loadLastVisit(ctx, db)

	// per-session aggregates
	type agg struct {
		tokens, cost, waitMs int64
		costF                float64
		modelCost            map[string]float64
		tools, toolFails     int
		compacted            bool
		longWait, repeats    bool
		areaSev              map[string]string
		issues               int
	}
	aggs := map[string]*agg{}
	get := func(sid string) *agg {
		a, ok := aggs[sid]
		if !ok {
			a = &agg{modelCost: map[string]float64{}, areaSev: map[string]string{}}
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
		if o.Kind == "re_read" || o.Kind == "repeat_cmd" {
			a.repeats = true
		}
		if cur, ok := a.areaSev[o.Area]; !ok || (cur == "half" && o.Severity == "full") {
			a.areaSev[o.Area] = o.Severity
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
			Model:     topModel(a.modelCost),
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
				c.Areas = append(c.Areas, areaPip{Area: area, Severity: sev})
			}
		}
		c.Priority = priorityOf(a.areaSev)
		if a.compacted {
			c.Symptoms = append(c.Symptoms, "compacted")
		}
		if a.tools > 0 && float64(a.toolFails)/float64(a.tools) > 0.10 {
			c.Symptoms = append(c.Symptoms, "failed")
		}
		if a.longWait {
			c.Symptoms = append(c.Symptoms, "waits")
		}
		if a.repeats {
			c.Symptoms = append(c.Symptoms, "repeats")
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

// firstPromptTitle is the fallback title when no generated one exists.
func firstPromptTitle(d *insightData, sid string) string {
	for _, p := range d.prompts {
		if p.SessionID == sid && strings.TrimSpace(p.Text) != "" {
			return truncate(strings.TrimSpace(p.Text), 60)
		}
	}
	return "Session · " + time.UnixMilli(d.sessions[sid].StartedAt).Format("Jan 2")
}
