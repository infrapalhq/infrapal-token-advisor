package api

import "net/http"

// handleListSessions handles GET /api/sessions?limit=&offset=&since=, newest
// first. since (e.g. "7d", "30d") restricts to sessions started at or after
// that cutoff; omitted means all time.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50)
	offset := intParam(r, "offset", 0)

	cutoffMs, hasSince, err := sinceCutoffMs(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	query := `
		SELECT
			sess.session_id, sess.started_at, sess.ended_at, sess.start_type,
			sess.app_version, sess.terminal_type,
			vs.duration_ms, vs.total_cost_usd, vs.total_tokens,
			vs.lines_added, vs.lines_removed, vs.commit_count, vs.pull_request_count,
			vs.active_time_user_s, vs.active_time_cli_s, vs.tool_call_count, vs.compaction_count
		FROM sessions sess
		JOIN v_session_summary vs ON vs.session_id = sess.session_id
	`
	args := []any{}
	if hasSince {
		query += " WHERE sess.started_at >= ?"
		args = append(args, cutoffMs)
	}
	query += " ORDER BY sess.started_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	sessions, err := rowsToMaps(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

// handleGetSession handles GET /api/sessions/{id}: one v_session_summary row
// plus the session's interactions ordered by seq.
func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	rows, err := s.db.QueryContext(r.Context(), `
		SELECT
			sess.session_id, sess.started_at, sess.ended_at, sess.start_type,
			sess.app_version, sess.app_entrypoint, sess.user_id, sess.user_email,
			sess.org_id, sess.terminal_type,
			vs.duration_ms, vs.total_cost_usd, vs.total_tokens,
			vs.lines_added, vs.lines_removed, vs.commit_count, vs.pull_request_count,
			vs.active_time_user_s, vs.active_time_cli_s, vs.tool_call_count, vs.compaction_count
		FROM sessions sess
		JOIN v_session_summary vs ON vs.session_id = sess.session_id
		WHERE sess.session_id = ?
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	summary, err := rowsToMaps(rows)
	rows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(summary) == 0 {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	interactionRows, err := s.db.QueryContext(r.Context(), `
		SELECT prompt_id, seq, started_at, duration_ms, prompt_text, command_name
		FROM interactions
		WHERE session_id = ?
		ORDER BY seq
	`, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer interactionRows.Close()

	interactions, err := rowsToMaps(interactionRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"session":      summary[0],
		"interactions": interactions,
	})
}
