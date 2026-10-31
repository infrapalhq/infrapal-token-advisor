-- 0017_mcp_detail_views: per-request and per-tool-call rows for the MCP
-- server's run_sql tool, which can only read v_* views. Metrics and identifiers
-- only: no error text, file paths, commands, tool input, or body refs, since
-- whatever these return is sent to the model.
CREATE VIEW v_api_request_detail AS
SELECT
  request_id,
  prompt_id,
  session_id,
  ts,
  model,
  query_source,
  speed,
  effort,
  input_tokens,
  output_tokens,
  cache_read_tokens,
  cache_creation_tokens,
  COALESCE(cost_usd_micros, 0) / 1000000.0 AS cost_usd,
  duration_ms,
  ttft_ms,
  stop_reason,
  attempt,
  success,
  status_code,
  agent_name,
  skill_name,
  plugin_name,
  mcp_server_name,
  mcp_tool_name
FROM api_requests;

CREATE VIEW v_tool_call_detail AS
SELECT
  tool_use_id,
  prompt_id,
  session_id,
  ts,
  tool_name,
  tool_source,
  decision,
  decision_source,
  blocked_on_user_ms,
  execution_ms,
  duration_ms,
  success,
  error_type,
  input_size_bytes,
  result_size_bytes,
  result_tokens,
  mcp_server_name,
  mcp_tool_name,
  mcp_server_scope,
  skill_name,
  subagent_type
FROM tool_calls;
