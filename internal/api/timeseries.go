package api

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/ShamikMukhopadhyay/ccotel/internal/durfmt"
)

// metricSpec maps a short API alias to the metric_points.name it reads and,
// for the metrics that split by an attrs.type value, that type.
type metricSpec struct {
	name       string
	typeFilter string
}

// metricAliases whitelists the metric names accepted by ?metric=, per the
// spec's 7 documented Claude Code metrics (lines_of_code and active_time
// each split into two aliases by their "type" attribute).
var metricAliases = map[string]metricSpec{
	"cost":             {name: "claude_code.cost.usage"},
	"tokens":           {name: "claude_code.token.usage"},
	"commits":          {name: "claude_code.commit.count"},
	"prs":              {name: "claude_code.pull_request.count"},
	"lines_added":      {name: "claude_code.lines_of_code.count", typeFilter: "added"},
	"lines_removed":    {name: "claude_code.lines_of_code.count", typeFilter: "removed"},
	"active_time_user": {name: "claude_code.active_time.total", typeFilter: "user"},
	"active_time_cli":  {name: "claude_code.active_time.total", typeFilter: "cli"},
	// Token-usage split by the same attrs.type values Claude Code already
	// reports (input/output/cacheRead/cacheCreation), for a composition
	// view instead of the single "tokens" total above.
	"tokens_input":          {name: "claude_code.token.usage", typeFilter: "input"},
	"tokens_output":         {name: "claude_code.token.usage", typeFilter: "output"},
	"tokens_cache_read":     {name: "claude_code.token.usage", typeFilter: "cacheRead"},
	"tokens_cache_creation": {name: "claude_code.token.usage", typeFilter: "cacheCreation"},
}

// bucketFormats maps a ?bucket= value to a SQLite strftime format string.
var bucketFormats = map[string]string{
	"hour": "%Y-%m-%dT%H:00:00",
	"day":  "%Y-%m-%d",
	"week": "%Y-%W",
}

// handleTimeseries handles GET /api/timeseries?metric=&bucket=hour|day|week&since=7d.
// "sessions" is the one alias with no metric_points source — it counts
// session starts directly off the sessions table instead.
func (s *Server) handleTimeseries(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	bucket := r.URL.Query().Get("bucket")
	if bucket == "" {
		bucket = "day"
	}
	format, ok := bucketFormats[bucket]
	if !ok {
		writeError(w, http.StatusBadRequest, "bucket must be one of: hour, day, week")
		return
	}

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

	if metric == "sessions" {
		rows, err := s.db.QueryContext(r.Context(), `
			SELECT strftime(?, started_at / 1000, 'unixepoch') AS bucket, COUNT(*) AS value
			FROM sessions
			WHERE started_at >= ?
			GROUP BY bucket
			ORDER BY bucket
		`, format, cutoffMs)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		defer rows.Close()
		s.writeTimeseriesRows(w, rows)
		return
	}

	spec, ok := metricAliases[metric]
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown metric: "+metric)
		return
	}

	query := `
		SELECT strftime(?, ts / 1000, 'unixepoch') AS bucket, SUM(value) AS value
		FROM metric_points
		WHERE name = ? AND ts >= ?
	`
	args := []any{format, spec.name, cutoffMs}
	if spec.typeFilter != "" {
		query += " AND json_extract(attrs, '$.type') = ?"
		args = append(args, spec.typeFilter)
	}
	query += " GROUP BY bucket ORDER BY bucket"

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	s.writeTimeseriesRows(w, rows)
}

func (s *Server) writeTimeseriesRows(w http.ResponseWriter, rows *sql.Rows) {
	type point struct {
		Bucket string  `json:"bucket"`
		Value  float64 `json:"value"`
	}
	points := []point{}
	for rows.Next() {
		var p point
		if err := rows.Scan(&p.Bucket, &p.Value); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, points)
}
