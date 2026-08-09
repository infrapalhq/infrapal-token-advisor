package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// Span is one decoded OTLP span, ready to land in the spans table and be
// projected into whichever typed table its span type enriches
// (interactions, tool_calls). Name doubles as span_type: Claude Code names
// every span after its type (claude_code.interaction, claude_code.tool,
// ...) and docs confirm every span's span.type attribute matches its name.
type Span struct {
	TraceID      string
	SpanID       string
	ParentSpanID string
	Name         string
	StartNs      int64
	EndNs        int64
	Status       string
	SessionID    string

	Attrs         map[string]any
	ResourceAttrs map[string]any
	Body          string // JSON
}

// SpansWriter buffers decoded spans and flushes them to the spans table in
// batches, projecting each into interactions/tool_calls enrichment in the
// same transaction as the raw insert.
type SpansWriter struct {
	bw *batchWriter[Span]
}

// NewSpansWriter constructs a SpansWriter over db. Call Run in its own
// goroutine to start flushing.
func NewSpansWriter(db *sql.DB) *SpansWriter {
	return &SpansWriter{bw: newBatchWriter(db, "spans", writeSpansBatch, describeSpan)}
}

func (w *SpansWriter) Enqueue(s Span) { w.bw.Enqueue(s) }

func (w *SpansWriter) Run(ctx context.Context) error { return w.bw.Run(ctx) }

func describeSpan(s Span) string {
	return fmt.Sprintf("%q (trace=%s span=%s)", s.Name, s.TraceID, s.SpanID)
}

func writeSpansBatch(db *sql.DB, batch []Span) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO spans (
			span_id, trace_id, parent_span_id, span_type, name, start_ns, end_ns,
			status, session_id, prompt_id, agent_id, parent_agent_id, attrs
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(span_id) DO UPDATE SET
			trace_id        = COALESCE(excluded.trace_id, spans.trace_id),
			parent_span_id  = COALESCE(excluded.parent_span_id, spans.parent_span_id),
			span_type       = COALESCE(excluded.span_type, spans.span_type),
			name            = COALESCE(excluded.name, spans.name),
			start_ns        = COALESCE(spans.start_ns, excluded.start_ns),
			end_ns          = COALESCE(excluded.end_ns, spans.end_ns),
			status          = COALESCE(excluded.status, spans.status),
			session_id      = COALESCE(excluded.session_id, spans.session_id),
			prompt_id       = COALESCE(excluded.prompt_id, spans.prompt_id),
			agent_id        = COALESCE(excluded.agent_id, spans.agent_id),
			parent_agent_id = COALESCE(excluded.parent_agent_id, spans.parent_agent_id),
			attrs           = COALESCE(excluded.attrs, spans.attrs)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, s := range batch {
		attrsJSON, err := json.Marshal(s.Attrs)
		if err != nil {
			attrsJSON = []byte("{}")
		}

		// Spans never carry prompt.id (see the interactions<->span
		// correlation note on projectInteractionSpan); best-effort fill it
		// from interactions once that row exists via the shared trace_id.
		promptID := lookupPromptIDByTrace(tx, s.TraceID)

		if _, err := stmt.Exec(
			s.SpanID, nullableString(s.TraceID), nullableString(s.ParentSpanID),
			s.Name, s.Name, s.StartNs, s.EndNs, nullableString(s.Status),
			nullableString(s.SessionID), promptID,
			nullStringAttr(s.Attrs, "agent_id"), nullStringAttr(s.Attrs, "parent_agent_id"),
			string(attrsJSON),
		); err != nil {
			return err
		}
		if err := upsertSession(tx, s.SessionID, s.StartNs/1e6, s.Attrs, s.ResourceAttrs); err != nil {
			return fmt.Errorf("upsert session for span %q: %w", s.Name, err)
		}
		if err := projectSpan(tx, s); err != nil {
			return fmt.Errorf("project span %q: %w", s.Name, err)
		}
	}
	return tx.Commit()
}

func lookupPromptIDByTrace(tx *sql.Tx, traceID string) sql.NullString {
	if traceID == "" {
		return sql.NullString{}
	}
	var promptID string
	if err := tx.QueryRow(`SELECT prompt_id FROM interactions WHERE trace_id = ? LIMIT 1`, traceID).Scan(&promptID); err != nil {
		return sql.NullString{}
	}
	return sql.NullString{String: promptID, Valid: true}
}

func projectSpan(tx *sql.Tx, s Span) error {
	switch normalizeEventName(s.Name) {
	case "interaction":
		return projectInteractionSpan(tx, s)
	case "tool":
		return projectToolSpan(tx, s)
	case "tool.execution":
		return projectToolExecutionSpan(tx, s)
	case "tool.blocked_on_user":
		return projectToolBlockedOnUserSpan(tx, s)
	default:
		return nil
	}
}

// projectInteractionSpan is the span side of the interactions<->
// claude_code.interaction correlation handshake (see
// upsertInteractionFromPrompt / claimPendingInteractionSpan in
// project_logs.go for the event side). If the user_prompt event already
// landed, its interactions row is keyed by (trace_id, root_span_id) and
// gets the duration applied directly; if not, this span stashes itself in
// pending_interaction_spans for the event side to claim later.
func projectInteractionSpan(tx *sql.Tx, s Span) error {
	durationMs, ok := attrInt64(s.Attrs, "interaction.duration_ms")
	if !ok {
		durationMs = (s.EndNs - s.StartNs) / 1e6
	}

	res, err := tx.Exec(`
		UPDATE interactions SET duration_ms = COALESCE(interactions.duration_ms, ?)
		WHERE trace_id = ? AND root_span_id = ?
	`, durationMs, s.TraceID, s.SpanID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}

	_, err = tx.Exec(`
		INSERT INTO pending_interaction_spans (trace_id, span_id, duration_ms)
		VALUES (?, ?, ?)
		ON CONFLICT(trace_id, span_id) DO UPDATE SET duration_ms = excluded.duration_ms
	`, s.TraceID, s.SpanID, durationMs)
	return err
}

// projectToolSpan projects claude_code.tool, one of five sources merged
// into tool_calls by tool_use_id (see toolCallFields).
func projectToolSpan(tx *sql.Tx, s Span) error {
	toolUseID := toolUseIDFromSpan(s.Attrs)
	if toolUseID == "" {
		return nil
	}
	f := toolCallFields{
		SessionID:    nullableString(s.SessionID),
		ToolName:     nullStringAttr(s.Attrs, "tool_name"),
		DurationMs:   nullInt64Attr(s.Attrs, "duration_ms"),
		ResultTokens: nullInt64Attr(s.Attrs, "result_tokens"),
		SkillName:    nullStringAttr(s.Attrs, "skill_name"),
		SubagentType: nullStringAttr(s.Attrs, "subagent_type"),
		FilePath:     nullStringAttr(s.Attrs, "file_path"),
		FullCommand:  nullStringAttr(s.Attrs, "full_command"),
	}
	if err := upsertToolCall(tx, toolUseID, f); err != nil {
		return err
	}
	return claimPendingBlockedOnUserSpan(tx, s.SpanID, toolUseID)
}

// claimPendingBlockedOnUserSpan looks for a claude_code.tool.blocked_on_user
// span that already arrived and staged itself (keyed by parent_span_id, see
// projectToolBlockedOnUserSpan) before this claude_code.tool span did. If
// found, its fields are applied to tool_calls and the staging row is
// consumed.
func claimPendingBlockedOnUserSpan(tx *sql.Tx, spanID, toolUseID string) error {
	var durationMs sql.NullInt64
	var decision, decisionSource sql.NullString
	err := tx.QueryRow(`
		SELECT duration_ms, decision, decision_source
		FROM pending_blocked_on_user_spans WHERE parent_span_id = ?
	`, spanID).Scan(&durationMs, &decision, &decisionSource)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	f := toolCallFields{
		BlockedOnUserMs: durationMs,
		Decision:        decision,
		DecisionSource:  decisionSource,
	}
	if err := upsertToolCall(tx, toolUseID, f); err != nil {
		return err
	}
	_, err = tx.Exec(`DELETE FROM pending_blocked_on_user_spans WHERE parent_span_id = ?`, spanID)
	return err
}

// projectToolExecutionSpan projects claude_code.tool.execution.
func projectToolExecutionSpan(tx *sql.Tx, s Span) error {
	toolUseID := toolUseIDFromSpan(s.Attrs)
	if toolUseID == "" {
		return nil
	}
	f := toolCallFields{
		ExecutionMs: nullInt64Attr(s.Attrs, "duration_ms"),
		Success:     nullBoolAttr(s.Attrs, "success"),
		Error:       nullStringAttr(s.Attrs, "error"),
	}
	return upsertToolCall(tx, toolUseID, f)
}

// projectToolBlockedOnUserSpan projects claude_code.tool.blocked_on_user.
// This span type carries no tool_use_id attribute at all (per docs) — its
// tool_use_id must be read off its parent claude_code.tool span's stored
// attrs via parent_span_id. Live testing showed the parent doesn't reliably
// precede this child (it can arrive later in the same flush batch, or on a
// later batch entirely), so if the parent hasn't been written yet, this
// span stages itself in pending_blocked_on_user_spans for
// claimPendingBlockedOnUserSpan (in projectToolSpan) to pick up once the
// parent does arrive — the same handshake pattern as
// pending_interaction_spans.
func projectToolBlockedOnUserSpan(tx *sql.Tx, s Span) error {
	if s.ParentSpanID == "" {
		return nil
	}

	durationMs := nullInt64Attr(s.Attrs, "duration_ms")
	decision := nullStringAttr(s.Attrs, "decision")
	decisionSource := nullStringAttr(s.Attrs, "source")

	var parentAttrsJSON string
	err := tx.QueryRow(`SELECT attrs FROM spans WHERE span_id = ?`, s.ParentSpanID).Scan(&parentAttrsJSON)
	if err == sql.ErrNoRows {
		_, err := tx.Exec(`
			INSERT INTO pending_blocked_on_user_spans (parent_span_id, duration_ms, decision, decision_source)
			VALUES (?, ?, ?, ?)
			ON CONFLICT(parent_span_id) DO UPDATE SET
				duration_ms     = COALESCE(excluded.duration_ms, pending_blocked_on_user_spans.duration_ms),
				decision        = COALESCE(excluded.decision, pending_blocked_on_user_spans.decision),
				decision_source = COALESCE(excluded.decision_source, pending_blocked_on_user_spans.decision_source)
		`, s.ParentSpanID, durationMs, decision, decisionSource)
		return err
	}
	if err != nil {
		return err
	}
	var parentAttrs map[string]any
	if err := json.Unmarshal([]byte(parentAttrsJSON), &parentAttrs); err != nil {
		return nil
	}

	toolUseID := toolUseIDFromSpan(parentAttrs)
	if toolUseID == "" {
		return nil
	}
	f := toolCallFields{
		BlockedOnUserMs: durationMs,
		Decision:        decision,
		DecisionSource:  decisionSource,
	}
	return upsertToolCall(tx, toolUseID, f)
}

func toolUseIDFromSpan(attrs map[string]any) string {
	if id := attrString(attrs, "tool_use_id"); id != "" {
		return id
	}
	return attrString(attrs, "gen_ai.tool.call.id")
}
