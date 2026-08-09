package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/ShamikMukhopadhyay/ccotel/internal/durfmt"
)

// viewTable whitelists the derived-view names exposed over the API against
// their actual SQL view. The path segment is never interpolated into a
// query directly — only a value looked up from this map is.
var viewTable = map[string]string{
	"prompt_efficiency":   "v_prompt_efficiency",
	"wasted_spend":        "v_wasted_spend",
	"permission_friction": "v_permission_friction",
	"mcp_inventory":       "v_mcp_inventory",
	"session_summary":     "v_session_summary",
	"model_performance":   "v_model_performance",
}

// viewsWithSessionID lists which of the above views carry a session_id
// column, and therefore accept the ?session_id= filter.
var viewsWithSessionID = map[string]bool{
	"prompt_efficiency": true,
	"wasted_spend":      true,
	"session_summary":   true,
}

// sinceColumn describes the column a view's ?since= filter applies to.
type sinceColumn struct {
	name       string
	dateString bool // true for a TEXT date column like wasted_spend.day, vs an epoch-ms column
}

// viewsWithSince lists which views carry a per-row timestamp, and therefore
// accept the ?since= filter (e.g. "7d", "30d"). Views that are already
// aggregated away from time (permission_friction, mcp_inventory,
// model_performance — grouped by tool/server/model, not by row) aren't
// listed here; they have no column a time filter could apply to.
var viewsWithSince = map[string]sinceColumn{
	"prompt_efficiency": {name: "started_at"},
	"session_summary":   {name: "started_at"},
	"wasted_spend":      {name: "day", dateString: true},
}

// handleView handles GET /api/views/{name}, a generic pass-through to one
// of the six derived views, per the spec's "thin API" instruction — no
// business logic here, the views already shape the data.
func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	table, ok := viewTable[name]
	if !ok {
		writeError(w, http.StatusNotFound, "unknown view: "+name)
		return
	}

	limit := intParam(r, "limit", 100)
	offset := intParam(r, "offset", 0)

	var conditions []string
	var args []any

	if sessionID := r.URL.Query().Get("session_id"); sessionID != "" && viewsWithSessionID[name] {
		conditions = append(conditions, "session_id = ?")
		args = append(args, sessionID)
	}

	if sc, ok := viewsWithSince[name]; ok {
		if since := r.URL.Query().Get("since"); since != "" {
			d, err := durfmt.ParseSince(since)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			cutoff := time.Now().Add(-d)
			conditions = append(conditions, sc.name+" >= ?")
			if sc.dateString {
				args = append(args, cutoff.Format("2006-01-02"))
			} else {
				args = append(args, cutoff.UnixMilli())
			}
		}
	}

	query := "SELECT * FROM " + table
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	results, err := rowsToMaps(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}
