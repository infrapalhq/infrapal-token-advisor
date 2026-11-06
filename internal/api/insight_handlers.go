package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/all"
	"github.com/infrapalhq/infrapal-token-advisor/internal/project"
)

// rangeCutoff reads ?since= as one of the dashboard range tokens.
func rangeCutoff(s *Server, r *http.Request) (int64, string, error) {
	token := r.URL.Query().Get("since")
	cutoff, err := durationSince(r.Context(), s.db, token)
	return cutoff, token, err
}

// agentParam reads ?agent=, which keeps one agent's sessions. Empty means all.
func agentParam(r *http.Request) agent.ID {
	return agent.ID(r.URL.Query().Get("agent"))
}

// handleAgents handles GET /api/agents: the default agent, then every other
// agent with sessions, with what each can't report.
func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT agent, COUNT(*) FROM sessions GROUP BY agent`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	counts := map[agent.ID]int{}
	for rows.Next() {
		var id agent.ID
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		counts[id] = n
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	first := all.Default("").ID()
	ids := []agent.ID{first}
	for id := range counts {
		if id != first {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids[1:], func(i, j int) bool { return ids[i+1] < ids[j+1] })
	out := make([]agentInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, describeAgent(id, counts[id]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// agentInfo is what the dashboard knows about one agent.
type agentInfo struct {
	ID        agent.ID `json:"id"`
	Name      string   `json:"name"`
	Sessions  int      `json:"sessions"`
	CostKnown bool     `json:"cost_known"`
	Skipped   []string `json:"skipped"` // checks this agent can't supply data for
}

func describeAgent(id agent.ID, sessions int) agentInfo {
	info := agentInfo{ID: id, Name: string(id), Sessions: sessions, Skipped: []string{}}
	a, ok := agentFor(id)
	if ok {
		info.Name = a.Name()
		info.CostKnown = a.CostSource() != agent.CostNone
	}
	for _, det := range detectors {
		if det.needs != 0 && (!ok || !a.Capabilities().Has(det.needs)) {
			info.Skipped = append(info.Skipped, det.label)
		}
	}
	return info
}

// handleSessionCards handles GET /api/sessions/cards?since=. One card per
// session, plus the priority counts the Sessions page's tabs show.
func (s *Server) handleSessionCards(w http.ResponseWriter, r *http.Request) {
	cutoff, token, err := rangeCutoff(s, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d, occ, err := computeInsights(r.Context(), s.db, cutoff, agentParam(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cards, err := buildCards(r.Context(), s.db, d, occ, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	marks, err := loadMarks(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := aggregateItems(occ, d.blendedRate)
	applyMarks(items, marks)
	installWide := 0
	for _, it := range items {
		if !it.Marked && it.Sessions == 0 {
			installWide++
		}
	}
	var attn, fresh int
	for _, c := range cards {
		if c.Priority != "fine" {
			attn++
		}
		if c.IsNew {
			fresh++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range":           token,
		"sessions":        cards,
		"count_all":       len(cards),
		"count_attention": attn,
		"count_new":       fresh,
		// count_install_wide is open findings tied to no session, such as an
		// unused MCP server. No session card lists them.
		"count_install_wide": installWide,
	})
}

// handleOverview handles GET /api/overview?since=. The numbers behind the
// Overview page: hero spend, KPIs, the top three fixes, problem areas,
// spend by day and by project, the model mix, and sessions to review.
func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	cutoff, token, err := rangeCutoff(s, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d, occ, err := computeInsights(r.Context(), s.db, cutoff, agentParam(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	marks, err := loadMarks(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := aggregateItems(occ, d.blendedRate)
	applyMarks(items, marks)
	cards, err := buildCards(r.Context(), s.db, d, occ, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	totals := totalSpend(d, occ)
	var waitMs, idleMs int64
	byProject := map[string]*projRow{}
	projOf := map[string]string{}
	for _, c := range cards {
		projOf[c.ID] = c.Project
	}
	for _, req := range d.reqs {
		proj := projOf[req.SessionID]
		if proj == "" {
			proj = project.Name("")
		}
		if byProject[proj] == nil {
			byProject[proj] = &projRow{Project: proj, sessions: map[string]struct{}{}}
		}
		byProject[proj].Cost += req.CostUSD
		byProject[proj].sessions[req.SessionID] = struct{}{}
	}
	for _, w := range d.waits {
		waitMs += w.WaitMs
		idleMs += w.IdleMs
	}

	var avoidable float64
	open := []*insightItem{}
	for _, it := range items {
		if it.Marked {
			continue
		}
		open = append(open, it)
		if it.USD != nil {
			avoidable += *it.USD
		}
	}
	fixFirst := []*insightItem{}
	var recoveredUSD, recoveredMin float64
	openFixes := 0
	for _, it := range open {
		if it.Fix == "" {
			continue
		}
		openFixes++
		if len(fixFirst) < 3 {
			fixFirst = append(fixFirst, it)
			if it.USD != nil {
				recoveredUSD += *it.USD
			}
			if it.Minutes != nil {
				recoveredMin += *it.Minutes
			}
		}
	}

	areas := areaSummaries(open, occ)
	projects := make([]*projRow, 0, len(byProject))
	for _, v := range byProject {
		v.Sessions = len(v.sessions)
		projects = append(projects, v)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Cost > projects[j].Cost })

	review := []sessionCard{}
	costUnknown := 0
	for _, c := range cards {
		if c.CostUnknown {
			costUnknown++
		}
		if c.Priority == "now" && len(review) < 2 {
			review = append(review, c)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range":          token,
		"spend_usd":      totals.Spend,
		"tokens":         totals.Tokens,
		"cache_share":    totals.CacheShare,
		"waiting_ms":     waitMs,
		"idle_ms":        idleMs,
		"avoidable_usd":  avoidable,
		"recovered":      map[string]any{"usd": recoveredUSD, "minutes": recoveredMin},
		"fix_first":      fixFirst,
		"open_fixes":     openFixes,
		"areas":          areas,
		"by_day":         totals.Days,
		"by_project":     projects,
		"models":         totals.Models,
		"review":         review,
		"sessions_count": len(cards),

		"cost_unknown_sessions": costUnknown,
	})
}

// spendTotals is what the dashboard's Overview and the MCP overview share.
type spendTotals struct {
	Spend      float64
	Tokens     int64
	CacheShare float64 // share of context tokens read from the cache
	Days       []*dayRow
	Models     []modelRow
}

// modelRow is one model's share of the spend.
type modelRow struct {
	Model string  `json:"model"`
	Cost  float64 `json:"cost_usd"`
	Share float64 `json:"share"`
}

// totalSpend adds up spend, tokens and cache reuse, spend and avoidable spend
// by day, and spend by model. Global findings, which belong to no session,
// have no day, so they're left out of the daily avoidable spend.
func totalSpend(d *insightData, occ []occurrence) spendTotals {
	var t spendTotals
	var cacheRead, context int64
	byDay := map[string]*dayRow{}
	dayOf := func(ts int64) *dayRow {
		day := time.UnixMilli(ts).Format("2006-01-02")
		if byDay[day] == nil {
			byDay[day] = &dayRow{Day: day}
		}
		return byDay[day]
	}
	models := map[string]float64{}
	for _, req := range d.reqs {
		t.Spend += req.CostUSD
		t.Tokens += req.Tokens()
		cacheRead += req.CacheRead
		context += req.Context
		dayOf(req.Ts).Cost += req.CostUSD
		models[req.Model] += req.CostUSD
	}
	for _, o := range occ {
		if o.SessionID == "" {
			continue
		}
		if o.USD != nil {
			dayOf(o.Ts).Avoidable += *o.USD
		} else if o.Tokens != nil && d.blendedRate > 0 {
			dayOf(o.Ts).Avoidable += float64(*o.Tokens) * d.blendedRate
		}
	}
	if context > 0 {
		t.CacheShare = float64(cacheRead) / float64(context)
	}

	t.Days = make([]*dayRow, 0, len(byDay))
	for _, v := range byDay {
		t.Days = append(t.Days, v)
	}
	sort.Slice(t.Days, func(i, j int) bool { return t.Days[i].Day < t.Days[j].Day })
	t.Models = make([]modelRow, 0, len(models))
	for m, c := range models {
		share := 0.0
		if t.Spend > 0 {
			share = c / t.Spend
		}
		t.Models = append(t.Models, modelRow{Model: m, Cost: c, Share: share})
	}
	sort.Slice(t.Models, func(i, j int) bool { return t.Models[i].Cost > t.Models[j].Cost })
	return t
}

// dayRow is one bar of the Overview's spend-by-day chart.
type dayRow struct {
	Day       string  `json:"day"`
	Cost      float64 `json:"cost_usd"`
	Avoidable float64 `json:"avoidable_usd"`
}

// projRow is one row of the spend-by-project list.
type projRow struct {
	Project  string  `json:"project"`
	Cost     float64 `json:"cost_usd"`
	Sessions int     `json:"sessions"`
	sessions map[string]struct{}
}

// areaSummary is one "problem area" card on the Overview.
type areaSummary struct {
	Area     string  `json:"area"`
	Items    int     `json:"items"`
	USD      float64 `json:"usd"`
	Severity string  `json:"severity"`
	Sessions int     `json:"sessions"`
	// InstallWide is how many of Items belong to no session (global findings).
	InstallWide int `json:"install_wide"`
}

// areaSummaries rolls open items up by area, with the worst severity and the
// number of distinct sessions affected in that area.
func areaSummaries(open []*insightItem, occ []occurrence) []areaSummary {
	order := []string{"context", "exec", "perm", "model"}
	out := make([]areaSummary, 0, len(order))
	for _, area := range order {
		a := areaSummary{Area: area, Severity: "fine"}
		sessions := map[string]struct{}{}
		for _, it := range open {
			if it.Area != area {
				continue
			}
			a.Items++
			if it.Sessions == 0 {
				// A global finding (an unused MCP server) is counted, but it
				// sets no severity or dollars: it isn't a problem in any session.
				a.InstallWide++
				continue
			}
			if it.USD != nil {
				a.USD += *it.USD
			}
			if it.Severity == "full" {
				a.Severity = "full"
			} else if it.Severity == "half" && a.Severity != "full" {
				a.Severity = "half"
			}
		}
		for _, o := range occ {
			if o.Area == area && o.SessionID != "" {
				sessions[o.SessionID] = struct{}{}
			}
		}
		a.Sessions = len(sessions)
		out = append(out, a)
	}
	return out
}

// handleFixes handles GET /api/fixes?since=. Items split into the three
// Fixes-page groups: to fix (open, with a copy-paste fix), habits (open, no
// config lever), and marked fixed.
func (s *Server) handleFixes(w http.ResponseWriter, r *http.Request) {
	cutoff, token, err := rangeCutoff(s, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d, occ, err := computeInsights(r.Context(), s.db, cutoff, agentParam(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	marks, err := loadMarks(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := aggregateItems(occ, d.blendedRate)
	applyMarks(items, marks)

	toFix, habits, marked := []*insightItem{}, []*insightItem{}, []*insightItem{}
	for _, it := range items {
		switch {
		case it.Marked:
			marked = append(marked, it)
		case it.Fix != "":
			toFix = append(toFix, it)
		default:
			habits = append(habits, it)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"range":  token,
		"to_fix": toFix,
		"habits": habits,
		"marked": marked,
		"counts": map[string]int{"to_fix": len(toFix), "habits": len(habits), "marked": len(marked)},
	})
}

// handlePostFixState handles POST /api/fixes/{id}/state, where {id} is the
// item key "kind|subject" (URL-escaped) and body is {"state":"fixed"|"reverted"}.
// Marking records a baseline snapshot and a 7-day measuring window.
func (s *Server) handlePostFixState(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("id")
	if key == "" {
		writeError(w, http.StatusBadRequest, "missing fix id")
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.State != "fixed" && body.State != "reverted" {
		writeError(w, http.StatusBadRequest, "state must be fixed or reverted")
		return
	}

	// Find the item across all time so its baseline covers its full history.
	d, occ, err := computeInsights(r.Context(), s.db, 0, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var target *insightItem
	for _, it := range aggregateItems(occ, d.blendedRate) {
		if it.Key == key {
			target = it
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "no such fix")
		return
	}
	if err := recordMark(r.Context(), s.db, target, body.State, time.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "state": body.State})
}

// handlePostUIVisit handles POST /api/ui/visit: records the time of this
// visit to the Sessions page. Sessions started after the previous visit are
// marked NEW on the next load.
func (s *Server) handlePostUIVisit(w http.ResponseWriter, r *http.Request) {
	prev := loadLastVisit(r.Context(), s.db)
	now := time.Now().UnixMilli()
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO ui_state (key, value) VALUES ('last_visit_at', CAST(? AS TEXT))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, now); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"last_visit_at": now, "previous_visit_at": prev})
}

// handleSessionIssues handles GET /api/sessions/{id}/issues: the session's
// occurrences grouped by area, for the Issues tabs and the Fix cards.
func (s *Server) handleSessionIssues(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, occ, err := computeInsights(r.Context(), s.db, 0, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, ok := d.sessions[id]; !ok {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	byArea := map[string][]occurrence{}
	for _, o := range occ {
		if o.SessionID == id {
			byArea[o.Area] = append(byArea[o.Area], o)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"session_id": id,
		"areas":      byArea,
	})
}
