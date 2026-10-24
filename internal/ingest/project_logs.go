package ingest

import (
	"database/sql"
	"strings"
)

// projectLogEvent dispatches a decoded log record into its typed table,
// inside the same transaction as its events_raw insert (two-phase write).
// Event names not handled here (auth, permission_mode_changed,
// internal_error, plugin_installed/loaded, the gated api_request_body/
// api_response_body pair, ...) have no typed table of their own in v1 —
// they still land in events_raw, just unprojected.
func projectLogEvent(tx *sql.Tx, e RawEvent) error {
	switch normalizeEventName(e.EventName) {
	case "user_prompt":
		return upsertInteractionFromPrompt(tx, e)
	case "assistant_response":
		return upsertAssistantResponse(tx, e)
	case "tool_result":
		return upsertToolCallFromResult(tx, e)
	case "tool_decision":
		return upsertToolCallFromDecision(tx, e)
	case "api_request":
		return insertAPIRequest(tx, e, false)
	case "api_error":
		return insertAPIRequest(tx, e, true)
	case "mcp_server_connection":
		return insertMCPServerConnection(tx, e)
	case "compaction":
		return insertCompaction(tx, e)
	default:
		return nil
	}
}

// normalizeEventName strips an optional "claude_code." prefix so dispatch
// matches regardless of which form the connected client sends. The docs
// (and prompt.md) describe event.name as always "claude_code.<event>", but
// a live 2.1.212 session was observed sending the bare form
// ("user_prompt", not "claude_code.user_prompt") — events_raw.event_name
// stores whatever arrived verbatim, this normalization only affects
// dispatch.
func normalizeEventName(name string) string {
	return strings.TrimPrefix(name, "claude_code.")
}

// upsertInteractionFromPrompt creates or enriches the interactions row for
// e.PromptID. This is one side of the interactions <-> claude_code.interaction
// span correlation problem: the event has prompt.id but, per docs, also
// carries the interaction span's trace_id/span_id from its own log record
// trace context once one is active — that's the only field shared with the
// span side, which carries no prompt.id at all. See
// claimPendingInteractionSpan and projectInteractionSpan (span.go) for the
// other side of this handshake, which can arrive first or second.
func upsertInteractionFromPrompt(tx *sql.Tx, e RawEvent) error {
	if e.PromptID == "" {
		return nil
	}

	promptLen, promptLenOK := attrInt64(e.Attrs, "prompt_length")

	_, err := tx.Exec(`
		INSERT INTO interactions (
			prompt_id, session_id, seq, started_at, prompt_text, prompt_length,
			command_name, command_source, message_uuid, trace_id, root_span_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(prompt_id) DO UPDATE SET
			session_id     = COALESCE(excluded.session_id, interactions.session_id),
			seq            = COALESCE(interactions.seq, excluded.seq),
			started_at     = COALESCE(interactions.started_at, excluded.started_at),
			prompt_text    = COALESCE(excluded.prompt_text, interactions.prompt_text),
			prompt_length  = COALESCE(excluded.prompt_length, interactions.prompt_length),
			command_name   = COALESCE(excluded.command_name, interactions.command_name),
			command_source = COALESCE(excluded.command_source, interactions.command_source),
			message_uuid   = COALESCE(excluded.message_uuid, interactions.message_uuid),
			trace_id       = COALESCE(excluded.trace_id, interactions.trace_id),
			root_span_id   = COALESCE(excluded.root_span_id, interactions.root_span_id)
	`,
		e.PromptID, nullableString(e.SessionID), e.Seq, e.Ts,
		nullStringAttr(e.Attrs, "prompt"), nullableInt64(promptLen, promptLenOK),
		nullStringAttr(e.Attrs, "command_name"), nullStringAttr(e.Attrs, "command_source"),
		nullStringAttr(e.Attrs, "message.uuid"), nullableString(e.TraceID), nullableString(e.SpanID),
	)
	if err != nil {
		return err
	}

	if e.TraceID == "" || e.SpanID == "" {
		return nil
	}
	return claimPendingInteractionSpan(tx, e.PromptID, e.TraceID, e.SpanID)
}

func nullableInt64(v int64, ok bool) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: ok}
}

// claimPendingInteractionSpan looks for a claude_code.interaction span that
// already arrived and staged itself (keyed by trace_id+span_id, see
// projectInteractionSpan) before this event did. If found, its duration is
// applied to the interactions row and the staging row is consumed.
func claimPendingInteractionSpan(tx *sql.Tx, promptID, traceID, spanID string) error {
	var durationMs sql.NullInt64
	err := tx.QueryRow(`
		SELECT duration_ms FROM pending_interaction_spans WHERE trace_id = ? AND span_id = ?
	`, traceID, spanID).Scan(&durationMs)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := tx.Exec(`
		UPDATE interactions SET duration_ms = COALESCE(interactions.duration_ms, ?) WHERE prompt_id = ?
	`, durationMs, promptID); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM pending_interaction_spans WHERE trace_id = ? AND span_id = ?`, traceID, spanID)
	return err
}

// upsertAssistantResponse upserts on message.uuid, the record's only
// natural key. message.uuid is absent before Claude Code v2.1.214 and on
// command dispatches — there's no other stable field to key on, so the
// record is dropped rather than inventing a synthetic key that could
// collide or diverge once a real message.uuid shows up for the same
// response.
func upsertAssistantResponse(tx *sql.Tx, e RawEvent) error {
	messageUUID := attrString(e.Attrs, "message.uuid")
	if messageUUID == "" {
		return nil
	}

	responseLen, responseLenOK := attrInt64(e.Attrs, "response_length")

	_, err := tx.Exec(`
		INSERT INTO assistant_responses (
			message_uuid, prompt_id, session_id, ts, model, request_id,
			query_source, response_text, response_length
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_uuid) DO UPDATE SET
			prompt_id       = COALESCE(excluded.prompt_id, assistant_responses.prompt_id),
			session_id      = COALESCE(excluded.session_id, assistant_responses.session_id),
			ts              = COALESCE(assistant_responses.ts, excluded.ts),
			model           = COALESCE(excluded.model, assistant_responses.model),
			request_id      = COALESCE(excluded.request_id, assistant_responses.request_id),
			query_source    = COALESCE(excluded.query_source, assistant_responses.query_source),
			response_text   = COALESCE(excluded.response_text, assistant_responses.response_text),
			response_length = COALESCE(excluded.response_length, assistant_responses.response_length)
	`,
		messageUUID, nullableString(e.PromptID), nullableString(e.SessionID), e.Ts,
		nullStringAttr(e.Attrs, "model"), nullStringAttr(e.Attrs, "request_id"),
		nullStringAttr(e.Attrs, "query_source"), nullStringAttr(e.Attrs, "response"),
		nullableInt64(responseLen, responseLenOK),
	)
	return err
}

// upsertToolCallFromResult projects claude_code.tool_result. mcp/skill/
// subagent/full_command/git_commit_id all arrive nested inside the gated
// tool_parameters JSON, not as top-level attributes — see
// parseToolParameters.
func upsertToolCallFromResult(tx *sql.Tx, e RawEvent) error {
	toolUseID := attrString(e.Attrs, "tool_use_id")
	if toolUseID == "" {
		return nil
	}

	params := parseToolParameters(e.Attrs)

	f := toolCallFields{
		PromptID:        nullableString(e.PromptID),
		SessionID:       nullableString(e.SessionID),
		Ts:              sql.NullInt64{Int64: e.Ts, Valid: true},
		ToolName:        nullStringAttr(e.Attrs, "tool_name"),
		Decision:        sql.NullString{String: "accept", Valid: true}, // tool_result only ever fires post-accept
		DecisionSource:  nullStringAttr(e.Attrs, "decision_source"),
		DurationMs:      nullInt64Attr(e.Attrs, "duration_ms"),
		Success:         nullBoolAttr(e.Attrs, "success"),
		ErrorType:       nullStringAttr(e.Attrs, "error_type"),
		Error:           nullStringAttr(e.Attrs, "error"),
		InputSizeBytes:  nullInt64Attr(e.Attrs, "tool_input_size_bytes"),
		ResultSizeBytes: nullInt64Attr(e.Attrs, "tool_result_size_bytes"),
		MCPServerScope:  nullStringAttr(e.Attrs, "mcp_server_scope"),
		MCPServerName:   nullMapString(params, "mcp_server_name"),
		MCPToolName:     nullMapString(params, "mcp_tool_name"),
		SkillName:       nullMapString(params, "skill_name"),
		SubagentType:    nullMapString(params, "subagent_type"),
		FullCommand:     nullMapString(params, "full_command"),
		GitCommitID:     nullMapString(params, "git_commit_id"),
		ToolInput:       nullJSONAttr(e.Attrs, "tool_input"),
	}
	return upsertToolCall(tx, toolUseID, f)
}

// upsertToolCallFromDecision projects claude_code.tool_decision. Its accept/
// reject decision lives in a "decision" attribute and its source lives in
// "source" — different names than the matching "decision"/"decision_source"
// columns tool_result writes, one of several event-vs-event naming
// inconsistencies in this schema.
func upsertToolCallFromDecision(tx *sql.Tx, e RawEvent) error {
	toolUseID := attrString(e.Attrs, "tool_use_id")
	if toolUseID == "" {
		return nil
	}

	params := parseToolParameters(e.Attrs)

	f := toolCallFields{
		PromptID:       nullableString(e.PromptID),
		SessionID:      nullableString(e.SessionID),
		Ts:             sql.NullInt64{Int64: e.Ts, Valid: true},
		ToolName:       nullStringAttr(e.Attrs, "tool_name"),
		ToolSource:     nullStringAttr(e.Attrs, "tool_source"),
		Decision:       nullStringAttr(e.Attrs, "decision"),
		DecisionSource: nullStringAttr(e.Attrs, "source"),
		MCPServerName:  nullMapString(params, "mcp_server_name"),
		MCPToolName:    nullMapString(params, "mcp_tool_name"),
	}
	return upsertToolCall(tx, toolUseID, f)
}

// insertAPIRequest inserts claude_code.api_request / claude_code.api_error.
// These are mutually exclusive outcomes of a single API call, not two views
// of the same logical row (there's no shared key to upsert on — request_id
// is only assigned once a response comes back, so a request that errors
// before that point has none), so each is its own INSERT.
func insertAPIRequest(tx *sql.Tx, e RawEvent, isError bool) error {
	costMicros, costOK := attrInt64(e.Attrs, "cost_usd_micros")
	if !costOK {
		if usd, ok := attrFloat64(e.Attrs, "cost_usd"); ok {
			costMicros, costOK = int64(usd*1e6), true
		}
	}

	success := sql.NullInt64{Int64: 1, Valid: true}
	var errText sql.NullString
	var statusCode sql.NullInt64
	if isError {
		success = sql.NullInt64{Int64: 0, Valid: true}
		errText = nullStringAttr(e.Attrs, "error")
		statusCode = nullInt64Attr(e.Attrs, "status_code")
	}

	_, err := tx.Exec(`
		INSERT INTO api_requests (
			request_id, client_request_id, prompt_id, session_id, ts, model,
			query_source, speed, effort, input_tokens, output_tokens,
			cache_read_tokens, cache_creation_tokens, cost_usd_micros, duration_ms,
			ttft_ms, stop_reason, attempt, success, status_code, error,
			agent_name, skill_name, plugin_name, marketplace_name, mcp_server_name, mcp_tool_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		nullStringAttr(e.Attrs, "request_id"), nullStringAttr(e.Attrs, "client_request_id"),
		nullableString(e.PromptID), nullableString(e.SessionID), e.Ts,
		nullStringAttr(e.Attrs, "model"), nullStringAttr(e.Attrs, "query_source"),
		nullStringAttr(e.Attrs, "speed"), nullStringAttr(e.Attrs, "effort"),
		nullInt64Attr(e.Attrs, "input_tokens"), nullInt64Attr(e.Attrs, "output_tokens"),
		nullInt64Attr(e.Attrs, "cache_read_tokens"), nullInt64Attr(e.Attrs, "cache_creation_tokens"),
		sql.NullInt64{Int64: costMicros, Valid: costOK}, nullInt64Attr(e.Attrs, "duration_ms"),
		nullInt64Attr(e.Attrs, "ttft_ms"), nullStringAttr(e.Attrs, "stop_reason"),
		nullInt64Attr(e.Attrs, "attempt"), success, statusCode, errText,
		// agent.name/skill.name/plugin.name/mcp_server.name/mcp_tool.name:
		// api_request/api_error use the dotted attribute form, unlike the
		// underscored form on log events like tool_result.
		nullStringAttr(e.Attrs, "agent.name"), nullStringAttr(e.Attrs, "skill.name"),
		nullStringAttr(e.Attrs, "plugin.name"), nullStringAttr(e.Attrs, "marketplace.name"),
		nullStringAttr(e.Attrs, "mcp_server.name"), nullStringAttr(e.Attrs, "mcp_tool.name"),
	)
	return err
}

// insertCompaction projects a compaction event into the typed compactions
// table (migration 0013). A compaction is identified by (session, ts); a
// redelivered event is ignored rather than duplicated.
func insertCompaction(tx *sql.Tx, e RawEvent) error {
	if e.SessionID == "" {
		return nil
	}
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO compactions (session_id, prompt_id, ts, pre_tokens, post_tokens, trigger, duration_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.SessionID, nullableString(e.PromptID), e.Ts,
		nullInt64Attr(e.Attrs, "pre_tokens"), nullInt64Attr(e.Attrs, "post_tokens"),
		nullStringAttr(e.Attrs, "trigger"), nullInt64Attr(e.Attrs, "duration_ms"),
	)
	return err
}
