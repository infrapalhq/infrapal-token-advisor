package api

import (
	"context"
	"database/sql"
	"net/http"
	"time"
)

// weekStart returns the Monday 00:00 (local) of the week containing t.
func weekStart(t time.Time) time.Time {
	y, m, d := t.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	offset := (int(day.Weekday()) + 6) % 7 // Monday = 0
	return day.AddDate(0, 0, -offset)
}

// progressWeek is one week of the Fixes page's chart.
type progressWeek struct {
	Week        string  `json:"week"` // Monday, YYYY-MM-DD
	Avoidable   float64 `json:"avoidable_usd"`
	Sessions    int     `json:"sessions"`
	HealthyPct  float64 `json:"healthy_pct"` // share of sessions not flagged "now"
	MarkedFixes int     `json:"marked_fixes"`
}

// handleProgress handles GET /api/progress?since=install. Weekly avoidable
// spend and healthy-session share since install, with the weeks in which a
// fix was marked, so the Fixes chart can annotate them.
func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
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
	marks, err := markedWeeks(r.Context(), s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	weeks := map[string]*progressWeek{}
	get := func(t time.Time) *progressWeek {
		key := weekStart(t).Format("2006-01-02")
		if weeks[key] == nil {
			weeks[key] = &progressWeek{Week: key}
		}
		return weeks[key]
	}
	for _, o := range occ {
		if o.SessionID == "" {
			continue
		}
		if o.USD != nil {
			get(time.UnixMilli(o.Ts)).Avoidable += *o.USD
		} else if o.Tokens != nil && d.blendedRate > 0 {
			get(time.UnixMilli(o.Ts)).Avoidable += float64(*o.Tokens) * d.blendedRate
		}
	}
	healthyNow := map[string]int{} // week -> sessions flagged "now"
	for _, c := range cards {
		wk := get(time.UnixMilli(c.StartedAt))
		wk.Sessions++
		if c.Priority == "now" {
			healthyNow[wk.Week]++
		}
	}
	for _, wk := range weeks {
		if wk.Sessions > 0 {
			wk.HealthyPct = 1 - float64(healthyNow[wk.Week])/float64(wk.Sessions)
		}
		wk.MarkedFixes = marks[wk.Week]
	}

	out := make([]*progressWeek, 0, len(weeks))
	for _, wk := range weeks {
		out = append(out, wk)
	}
	sortProgressWeeks(out)
	writeJSON(w, http.StatusOK, map[string]any{"range": token, "weeks": out})
}

// markedWeeks counts fix marks per Monday, for the chart's annotations.
func markedWeeks(ctx context.Context, db *sql.DB) (map[string]int, error) {
	rows, err := db.QueryContext(ctx, `SELECT marked_at FROM fix_marks WHERE state = 'fixed'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var at int64
		if err := rows.Scan(&at); err != nil {
			return nil, err
		}
		out[weekStart(time.UnixMilli(at)).Format("2006-01-02")]++
	}
	return out, rows.Err()
}

// sortProgressWeeks orders weeks oldest first.
func sortProgressWeeks(ws []*progressWeek) {
	for i := 1; i < len(ws); i++ {
		for j := i; j > 0 && ws[j-1].Week > ws[j].Week; j-- {
			ws[j-1], ws[j] = ws[j], ws[j-1]
		}
	}
}

// handleStatus handles GET /api/status: whether the collector is receiving
// data, when the last event arrived, and whether prompt text is being logged.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	var lastEvent sql.NullInt64
	if err := s.db.QueryRowContext(r.Context(), `SELECT MAX(ts) FROM events_raw`).Scan(&lastEvent); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	var promptsLogged int64
	if err := s.db.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM interactions
		WHERE prompt_text IS NOT NULL AND prompt_text != '' AND started_at >= ?`,
		time.Now().Add(-7*24*time.Hour).UnixMilli()).Scan(&promptsLogged); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	live := lastEvent.Valid && time.Since(time.UnixMilli(lastEvent.Int64)) < 10*time.Minute
	out := map[string]any{
		"collector_live":    live,
		"prompts_logged_7d": promptsLogged > 0,
	}
	if lastEvent.Valid {
		out["last_event_ms"] = lastEvent.Int64
	}
	writeJSON(w, http.StatusOK, out)
}
