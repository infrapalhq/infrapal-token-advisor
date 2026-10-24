package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/project"
)

// rangeCutoff reads ?since= as one of the dashboard range tokens.
func rangeCutoff(s *Server, r *http.Request) (int64, string, error) {
	token := r.URL.Query().Get("since")
	cutoff, err := durationSince(r.Context(), s.db, token)
	return cutoff, token, err
}

// handleSessionCards handles GET /api/sessions/cards?since=. One card per
// session, plus the priority counts the Sessions page's tabs show.
func (s *Server) handleSessionCards(w http.ResponseWriter, r *http.Request) {
	cutoff, token, err := rangeCutoff(s, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	d, occ, err := computeInsights(r.Context(), s.db, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cards, err := buildCards(r.Context(), s.db, d, occ, cutoff)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
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
	d, occ, err := computeInsights(r.Context(), s.db, cutoff)
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

	var spend float64
	var tokens, cacheRead, context, waitMs, idleMs int64
	byDay := map[string]*dayRow{}
	byProject := map[string]*projRow{}
	models := map[string]float64{}
	projOf := map[string]string{}
	for _, c := range cards {
		projOf[c.ID] = c.Project
	}
	for _, req := range d.reqs {
		spend += req.CostUSD
		tokens += req.Tokens()
		cacheRead += req.CacheRead
		context += req.Context
		day := time.UnixMilli(req.Ts).Format("2006-01-02")
		if byDay[day] == nil {
			byDay[day] = &dayRow{Day: day}
		}
		byDay[day].Cost += req.CostUSD
		models[req.Model] += req.CostUSD
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
	for _, o := range occ {
		if o.SessionID == "" {
			continue
		}
		day := time.UnixMilli(o.Ts).Format("2006-01-02")
		if byDay[day] == nil {
			byDay[day] = &dayRow{Day: day}
		}
		if o.USD != nil {
			byDay[day].Avoidable += *o.USD
		} else if o.Tokens != nil && d.blendedRate > 0 {
			byDay[day].Avoidable += float64(*o.Tokens) * d.blendedRate
		}
	}

	fixFirst := []*insightItem{}
	var recoveredUSD, recoveredMin float64
	for _, it := range open {
		if it.Fix == "" {
			continue
		}
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
	days := make([]*dayRow, 0, len(byDay))
	for _, v := range byDay {
		days = append(days, v)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Day < days[j].Day })
	projects := make([]*projRow, 0, len(byProject))
	for _, v := range byProject {
		v.Sessions = len(v.sessions)
		projects = append(projects, v)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].Cost > projects[j].Cost })

	modelRows := []map[string]any{}
	for m, c := range models {
		share := 0.0
		if spend > 0 {
			share = c / spend
		}
		modelRows = append(modelRows, map[string]any{"model": m, "cost_usd": c, "share": share})
	}
	sort.Slice(modelRows, func(i, j int) bool { return modelRows[i]["cost_usd"].(float64) > modelRows[j]["cost_usd"].(float64) })

	review := []sessionCard{}
	for _, c := range cards {
		if c.Priority == "now" && len(review) < 2 {
			review = append(review, c)
		}
	}
	cacheShare := 0.0
	if context > 0 {
		cacheShare = float64(cacheRead) / float64(context)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"range":          token,
		"spend_usd":      spend,
		"tokens":         tokens,
		"cache_share":    cacheShare,
		"waiting_ms":     waitMs,
		"idle_ms":        idleMs,
		"avoidable_usd":  avoidable,
		"recovered":      map[string]any{"usd": recoveredUSD, "minutes": recoveredMin},
		"fix_first":      fixFirst,
		"areas":          areas,
		"by_day":         days,
		"by_project":     projects,
		"models":         modelRows,
		"review":         review,
		"sessions_count": len(cards),
	})
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
	d, occ, err := computeInsights(r.Context(), s.db, cutoff)
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
	d, occ, err := computeInsights(r.Context(), s.db, 0)
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

// handleGetUIState handles GET /api/ui/state: last_visit_at and install_at.
func (s *Server) handleGetUIState(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), `SELECT key, value FROM ui_state`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
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
	d, occ, err := computeInsights(r.Context(), s.db, 0)
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
