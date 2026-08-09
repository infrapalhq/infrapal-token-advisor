package api

import (
	"net/http"
	"strings"
)

// handleSearch handles GET /api/search?q=&limit=, querying both FTS5 tables
// from migration 0001 and merging results with a "kind" discriminator so
// the client can tell prompts from assistant responses.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	limit := intParam(r, "limit", 25)

	// Wrap the query as an FTS5 phrase rather than passing it through
	// FTS5's own query syntax unescaped: user input containing FTS5
	// operators (e.g. a literal "-" or ":") would otherwise raise a syntax
	// error instead of just searching for that text.
	ftsQuery := `"` + strings.ReplaceAll(q, `"`, `""`) + `"`

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT 'prompt' AS kind, i.prompt_id AS id, i.session_id, i.started_at AS ts,
			snippet(interactions_fts, 0, '<mark>', '</mark>', '…', 12) AS snippet
		FROM interactions_fts
		JOIN interactions i ON i.rowid = interactions_fts.rowid
		WHERE interactions_fts MATCH ?
		UNION ALL
		SELECT 'response' AS kind, a.message_uuid AS id, a.session_id, a.ts AS ts,
			snippet(assistant_responses_fts, 0, '<mark>', '</mark>', '…', 12) AS snippet
		FROM assistant_responses_fts
		JOIN assistant_responses a ON a.rowid = assistant_responses_fts.rowid
		WHERE assistant_responses_fts MATCH ?
		ORDER BY ts DESC
		LIMIT ?
	`, ftsQuery, ftsQuery, limit)
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
