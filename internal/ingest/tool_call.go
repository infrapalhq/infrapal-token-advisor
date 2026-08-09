package ingest

import (
	"database/sql"
	"encoding/json"
)

// toolCallFields holds the subset of tool_calls columns a single source
// record can contribute. Every field is NULL-safe (sql.Null*) so
// upsertToolCall's COALESCE merge never lets an unset field from one
// source stomp a value already contributed by another.
//
// tool_calls is merged from up to five independent, out-of-order sources
// sharing tool_use_id: the tool_decision and tool_result log events, and
// the claude_code.tool / claude_code.tool.execution /
// claude_code.tool.blocked_on_user spans. This is the one shared shape
// they all write through.
type toolCallFields struct {
	PromptID        sql.NullString
	SessionID       sql.NullString
	Ts              sql.NullInt64
	ToolName        sql.NullString
	ToolSource      sql.NullString
	Decision        sql.NullString
	DecisionSource  sql.NullString
	BlockedOnUserMs sql.NullInt64
	ExecutionMs     sql.NullInt64
	DurationMs      sql.NullInt64
	Success         sql.NullInt64
	ErrorType       sql.NullString
	Error           sql.NullString
	InputSizeBytes  sql.NullInt64
	ResultSizeBytes sql.NullInt64
	ResultTokens    sql.NullInt64
	MCPServerName   sql.NullString
	MCPToolName     sql.NullString
	MCPServerScope  sql.NullString
	MarketplaceName sql.NullString
	SkillName       sql.NullString
	SubagentType    sql.NullString
	FilePath        sql.NullString
	FullCommand     sql.NullString
	GitCommitID     sql.NullString
	ToolInput       sql.NullString
}

func upsertToolCall(tx *sql.Tx, toolUseID string, f toolCallFields) error {
	if toolUseID == "" {
		return nil
	}
	_, err := tx.Exec(`
		INSERT INTO tool_calls (
			tool_use_id, prompt_id, session_id, ts, tool_name, tool_source,
			decision, decision_source, blocked_on_user_ms, execution_ms, duration_ms,
			success, error_type, error, input_size_bytes, result_size_bytes, result_tokens,
			mcp_server_name, mcp_tool_name, mcp_server_scope, marketplace_name, skill_name,
			subagent_type, file_path, full_command, git_commit_id, tool_input
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tool_use_id) DO UPDATE SET
			prompt_id          = COALESCE(excluded.prompt_id, tool_calls.prompt_id),
			session_id         = COALESCE(excluded.session_id, tool_calls.session_id),
			ts                 = COALESCE(tool_calls.ts, excluded.ts),
			tool_name          = COALESCE(excluded.tool_name, tool_calls.tool_name),
			tool_source        = COALESCE(excluded.tool_source, tool_calls.tool_source),
			decision           = COALESCE(excluded.decision, tool_calls.decision),
			decision_source    = COALESCE(excluded.decision_source, tool_calls.decision_source),
			blocked_on_user_ms = COALESCE(excluded.blocked_on_user_ms, tool_calls.blocked_on_user_ms),
			execution_ms       = COALESCE(excluded.execution_ms, tool_calls.execution_ms),
			duration_ms        = COALESCE(excluded.duration_ms, tool_calls.duration_ms),
			success            = COALESCE(excluded.success, tool_calls.success),
			error_type         = COALESCE(excluded.error_type, tool_calls.error_type),
			error              = COALESCE(excluded.error, tool_calls.error),
			input_size_bytes   = COALESCE(excluded.input_size_bytes, tool_calls.input_size_bytes),
			result_size_bytes  = COALESCE(excluded.result_size_bytes, tool_calls.result_size_bytes),
			result_tokens      = COALESCE(excluded.result_tokens, tool_calls.result_tokens),
			mcp_server_name    = COALESCE(excluded.mcp_server_name, tool_calls.mcp_server_name),
			mcp_tool_name      = COALESCE(excluded.mcp_tool_name, tool_calls.mcp_tool_name),
			mcp_server_scope   = COALESCE(excluded.mcp_server_scope, tool_calls.mcp_server_scope),
			marketplace_name   = COALESCE(excluded.marketplace_name, tool_calls.marketplace_name),
			skill_name         = COALESCE(excluded.skill_name, tool_calls.skill_name),
			subagent_type      = COALESCE(excluded.subagent_type, tool_calls.subagent_type),
			file_path          = COALESCE(excluded.file_path, tool_calls.file_path),
			full_command       = COALESCE(excluded.full_command, tool_calls.full_command),
			git_commit_id      = COALESCE(excluded.git_commit_id, tool_calls.git_commit_id),
			tool_input         = COALESCE(excluded.tool_input, tool_calls.tool_input)
	`,
		toolUseID, f.PromptID, f.SessionID, f.Ts, f.ToolName, f.ToolSource,
		f.Decision, f.DecisionSource, f.BlockedOnUserMs, f.ExecutionMs, f.DurationMs,
		f.Success, f.ErrorType, f.Error, f.InputSizeBytes, f.ResultSizeBytes, f.ResultTokens,
		f.MCPServerName, f.MCPToolName, f.MCPServerScope, f.MarketplaceName, f.SkillName,
		f.SubagentType, f.FilePath, f.FullCommand, f.GitCommitID, f.ToolInput,
	)
	return err
}

// parseToolParameters decodes the tool_parameters attribute (gated, JSON,
// tool-specific shape, e.g. mcp_server_name/mcp_tool_name for MCP calls or
// git_commit_id for Bash) into a plain map, or nil if the attribute is
// absent, gated off, or malformed.
func parseToolParameters(m map[string]any) map[string]any {
	raw, ok := attrJSON(m, "tool_parameters")
	if !ok {
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil
	}
	return obj
}
