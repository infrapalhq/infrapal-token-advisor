package ingest

import (
	"database/sql"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// The views and detectors still read Claude Code's own values for these, so
// common events are written in that form.
const (
	queryMain       = "repl_main_thread"
	queryBackground = "background"
	queryTurnCost   = "turn_cost"

	decisionAccept = "accept"
	decisionReject = "reject"

	decisionSourceUser   = "user_temporary"
	decisionSourceConfig = "config"
	decisionSourceReject = "user_reject"
)

// logSource is an agent that maps its own OTLP logs.
type logSource struct {
	id agent.ID
	agent.LogSource
}

func logSources(agents []agent.Agent) []logSource {
	var sources []logSource
	for _, a := range agents {
		if s, ok := a.(agent.LogSource); ok {
			sources = append(sources, logSource{a.ID(), s})
		}
	}
	return sources
}

// routeLog finds the adapter that owns e, or nil if none does, which means e
// is Claude Code's. Its projection stays separate from common events because
// it fills far more columns than they carry, and merges spans into them.
func routeLog(sources []logSource, e RawEvent) *logSource {
	service := attrString(e.ResourceAttrs, "service.name")
	for i := range sources {
		if sources[i].OwnsService(service) {
			return &sources[i]
		}
	}
	return nil
}

func writeMappedLog(tx *sql.Tx, src logSource, e RawEvent) error {
	rec := agent.LogRecord{Name: e.EventName, Ts: e.Ts, Attrs: e.Attrs}
	for _, ev := range src.MapLog(rec) {
		if err := writeAgentEvent(tx, src.id, ev); err != nil {
			return err
		}
	}
	return nil
}

// writeAgentEvent stores one common event. An event with no session is
// dropped, because nothing can be shown for it.
func writeAgentEvent(tx *sql.Tx, id agent.ID, ev agent.Event) error {
	m := ev.Base()
	if m.SessionID == "" {
		return nil
	}
	if err := upsertAgentSession(tx, id, m); err != nil {
		return err
	}
	switch ev := ev.(type) {
	case agent.Session:
		return writeSessionDetails(tx, ev)
	case agent.Prompt:
		return writePrompt(tx, ev)
	case agent.Response:
		return writeResponse(tx, ev)
	case agent.Request:
		return writeRequest(tx, ev)
	case agent.TurnCost:
		return writeTurnCost(tx, ev)
	case agent.Tool:
		return writeTool(tx, ev)
	case agent.Compaction:
		return writeCompaction(tx, ev)
	case agent.MCPConnection:
		return writeMCPConnection(tx, ev)
	default:
		return nil
	}
}

// upsertAgentSession also corrects sessions.agent: a metric or span can create
// the row first, with the default agent.
func upsertAgentSession(tx *sql.Tx, id agent.ID, m agent.Meta) error {
	if err := upsertSession(tx, m.SessionID, m.Ts, nil, nil); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE sessions SET agent = ? WHERE session_id = ?`, string(id), m.SessionID)
	return err
}

func writeSessionDetails(tx *sql.Tx, ev agent.Session) error {
	_, err := tx.Exec(`
		UPDATE sessions SET
			project_dir = COALESCE(?, project_dir),
			app_version = COALESCE(?, app_version)
		WHERE session_id = ?`,
		nullableString(ev.ProjectDir), nullableString(ev.Version), ev.SessionID)
	return err
}

func writePrompt(tx *sql.Tx, ev agent.Prompt) error {
	if ev.PromptID == "" {
		return nil
	}
	_, err := tx.Exec(`
		INSERT INTO interactions (prompt_id, session_id, seq, started_at, prompt_text, prompt_length)
		VALUES (?, ?, (SELECT COUNT(*) + 1 FROM interactions WHERE session_id = ?), ?, ?, ?)
		ON CONFLICT(prompt_id) DO UPDATE SET
			prompt_text   = COALESCE(excluded.prompt_text, interactions.prompt_text),
			prompt_length = COALESCE(excluded.prompt_length, interactions.prompt_length)`,
		ev.PromptID, ev.SessionID, ev.SessionID, ev.Ts,
		nullableString(ev.Text), nullableInt64(ev.Length, ev.Length > 0))
	return err
}

func writeResponse(tx *sql.Tx, ev agent.Response) error {
	if ev.MessageID == "" {
		return nil
	}
	_, err := tx.Exec(`
		INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, ts, query_source, response_text)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(message_uuid) DO UPDATE SET
			response_text = COALESCE(excluded.response_text, assistant_responses.response_text)`,
		ev.MessageID, nullableString(ev.PromptID), ev.SessionID, ev.Ts, queryMain, nullableString(ev.Text))
	return err
}

func writeRequest(tx *sql.Tx, ev agent.Request) error {
	source := queryMain
	if ev.Background {
		source = queryBackground
	}
	success := int64(0)
	if ev.Success {
		success = 1
	}
	var cost sql.NullInt64
	if ev.CostMicros != nil {
		cost = sql.NullInt64{Int64: *ev.CostMicros, Valid: true}
	}
	_, err := tx.Exec(`
		INSERT INTO api_requests (
			request_id, prompt_id, session_id, ts, model, query_source, input_tokens,
			output_tokens, cache_read_tokens, cache_creation_tokens, cost_usd_micros,
			duration_ms, success, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		nullableString(ev.RequestID), nullableString(ev.PromptID), ev.SessionID, ev.Ts,
		nullableString(ev.Model), source, ev.Tokens.Input, ev.Tokens.Output,
		ev.Tokens.CacheRead, ev.Tokens.CacheWrite, cost, ev.DurationMs, success,
		nullableString(ev.Error))
	return err
}

// writeTurnCost stores a prompt's total as a request with no tokens, so that
// everything summing api_requests counts it without any change.
func writeTurnCost(tx *sql.Tx, ev agent.TurnCost) error {
	_, err := tx.Exec(`
		INSERT INTO api_requests (prompt_id, session_id, ts, query_source, cost_usd_micros, success)
		VALUES (?, ?, ?, ?, ?, 1)`,
		nullableString(ev.PromptID), ev.SessionID, ev.Ts, queryTurnCost, ev.Micros)
	return err
}

func writeTool(tx *sql.Tx, ev agent.Tool) error {
	f := toolCallFields{
		PromptID:        nullableString(ev.PromptID),
		SessionID:       nullableString(ev.SessionID),
		Ts:              sql.NullInt64{Int64: ev.Ts, Valid: ev.Ts != 0},
		ToolName:        nullableString(ev.Name),
		MCPServerName:   nullableString(ev.MCPServer),
		FilePath:        nullableString(ev.File),
		FullCommand:     nullableString(ev.Command),
		BlockedOnUserMs: nullableInt64Ptr(ev.WaitMs),
		DurationMs:      nullableInt64Ptr(ev.DurationMs),
		ResultSizeBytes: nullableInt64Ptr(ev.ResultBytes),
	}
	switch ev.Approval {
	case agent.ApprovalAuto:
		f.Decision, f.DecisionSource = nullableString(decisionAccept), nullableString(decisionSourceConfig)
	case agent.ApprovalUser:
		f.Decision, f.DecisionSource = nullableString(decisionAccept), nullableString(decisionSourceUser)
	case agent.ApprovalRejected:
		f.Decision, f.DecisionSource = nullableString(decisionReject), nullableString(decisionSourceReject)
	}
	if ev.Success != nil {
		f.Success = sql.NullInt64{Valid: true}
		if *ev.Success {
			f.Success.Int64 = 1
		}
	}
	return upsertToolCall(tx, ev.CallID, f)
}

func writeCompaction(tx *sql.Tx, ev agent.Compaction) error {
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO compactions (session_id, prompt_id, ts, pre_tokens, post_tokens)
		VALUES (?, ?, ?, ?, ?)`,
		ev.SessionID, nullableString(ev.PromptID), ev.Ts, ev.TokensBefore, ev.TokensAfter)
	return err
}

func writeMCPConnection(tx *sql.Tx, ev agent.MCPConnection) error {
	_, err := tx.Exec(`
		INSERT INTO mcp_server_connections (ts, session_id, server_name, status)
		VALUES (?, ?, ?, ?)`,
		ev.Ts, ev.SessionID, nullableString(ev.Server), nullableString(ev.Status))
	return err
}

func nullableInt64Ptr(v *int64) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *v, Valid: true}
}
