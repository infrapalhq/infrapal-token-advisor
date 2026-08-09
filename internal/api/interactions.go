package api

import "net/http"

// handleListInteractions handles GET /api/interactions?limit=&offset=&since=:
// a flat, paginated feed of real interactions across all sessions, newest
// first, enriched with per-prompt cost/tokens/turns/tool-call-count from
// v_prompt_efficiency — the data a card browser needs without a second
// round trip per card. since (e.g. "7d", "30d") restricts to interactions
// started at or after that cutoff; omitted means all time.
func (s *Server) handleListInteractions(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 50)
	offset := intParam(r, "offset", 0)

	cutoffMs, hasSince, err := sinceCutoffMs(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	query := `
		SELECT i.prompt_id, i.session_id, i.seq, i.started_at, i.duration_ms,
			i.prompt_text, i.prompt_length, i.command_name,
			COALESCE(pe.total_cost_usd, 0) AS total_cost_usd,
			COALESCE(pe.total_tokens, 0) AS total_tokens,
			COALESCE(pe.llm_turn_count, 0) AS llm_turn_count,
			COALESCE(pe.tool_call_count, 0) AS tool_call_count
		FROM interactions i
		LEFT JOIN v_prompt_efficiency pe ON pe.prompt_id = i.prompt_id
	`
	args := []any{}
	if hasSince {
		query += " WHERE i.started_at >= ?"
		args = append(args, cutoffMs)
	}
	query += " ORDER BY i.started_at DESC LIMIT ? OFFSET ?"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	interactions, err := rowsToMaps(rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, interactions)
}

// handleGetInteraction handles GET /api/interactions/{prompt_id}: the
// waterfall payload for one prompt. Returns the interaction, and flat
// arrays of its spans/api_requests/tool_calls/assistant_responses — each span already carries
// agent_id/parent_agent_id/parent_span_id (plus tool_use_id, extracted from
// its attrs JSON, so the client can join a "claude_code.tool" span back to
// its richer tool_calls row), so the client (milestone 6) reconstructs the
// subagent tree itself; this API stays thin and does not nest the response.
func (s *Server) handleGetInteraction(w http.ResponseWriter, r *http.Request) {
	promptID := r.PathValue("prompt_id")

	interactionRows, err := s.db.QueryContext(r.Context(), `
		SELECT prompt_id, session_id, seq, started_at, duration_ms, prompt_text,
			prompt_length, command_name, command_source, message_uuid, trace_id, root_span_id
		FROM interactions
		WHERE prompt_id = ?
	`, promptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	interaction, err := rowsToMaps(interactionRows)
	interactionRows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(interaction) == 0 {
		writeError(w, http.StatusNotFound, "interaction not found")
		return
	}

	spanRows, err := s.db.QueryContext(r.Context(), `
		SELECT span_id, trace_id, parent_span_id, span_type, name, start_ns, end_ns,
			status, session_id, agent_id, parent_agent_id,
			COALESCE(json_extract(attrs, '$.tool_use_id'), json_extract(attrs, '$."gen_ai.tool.call.id"')) AS tool_use_id
		FROM spans
		WHERE prompt_id = ?
		ORDER BY start_ns
	`, promptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	spans, err := rowsToMaps(spanRows)
	spanRows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	apiRequestRows, err := s.db.QueryContext(r.Context(), `
		SELECT request_id, ts, model, query_source, speed, effort, input_tokens,
			output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd_micros,
			duration_ms, ttft_ms, stop_reason, attempt, success, status_code, error
		FROM api_requests
		WHERE prompt_id = ?
		ORDER BY ts
	`, promptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	apiRequests, err := rowsToMaps(apiRequestRows)
	apiRequestRows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	toolCallRows, err := s.db.QueryContext(r.Context(), `
		SELECT tool_use_id, ts, tool_name, tool_source, decision, decision_source,
			blocked_on_user_ms, duration_ms, success, error_type, error,
			mcp_server_name, mcp_tool_name, skill_name, subagent_type, full_command
		FROM tool_calls
		WHERE prompt_id = ?
		ORDER BY ts
	`, promptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	toolCalls, err := rowsToMaps(toolCallRows)
	toolCallRows.Close()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	assistantResponseRows, err := s.db.QueryContext(r.Context(), `
		SELECT message_uuid, ts, model, request_id, query_source, response_text, response_length
		FROM assistant_responses
		WHERE prompt_id = ?
		ORDER BY ts
	`, promptID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer assistantResponseRows.Close()
	assistantResponses, err := rowsToMaps(assistantResponseRows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"interaction":         interaction[0],
		"spans":               spans,
		"api_requests":        apiRequests,
		"tool_calls":          toolCalls,
		"assistant_responses": assistantResponses,
	})
}
