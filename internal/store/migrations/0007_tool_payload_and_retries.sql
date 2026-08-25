-- 0007_tool_payload_and_retries: two views for token-reduction analysis,
-- additive to the milestone-5 views in 0006. Neither changes an existing
-- table or view — both are new SELECT surfaces over columns already
-- ingested (tool_calls.input_size_bytes/result_size_bytes/result_tokens,
-- api_requests.attempt), just never aggregated before.

-- Per tool identity (tool_name + mcp_server_name + mcp_tool_name, same
-- "shape" used by v_permission_friction): call volume plus the payload
-- size going in and coming back out. result_tokens is carried through for
-- installs where Claude Code populates it directly; where it doesn't,
-- avg_result_bytes / total_result_bytes are the real signal for which
-- tools are stuffing the most content back into context.
CREATE VIEW v_tool_payload AS
SELECT
  tool_name,
  mcp_server_name,
  mcp_tool_name,
  COUNT(*) AS call_count,
  SUM(COALESCE(input_size_bytes, 0)) AS total_input_bytes,
  SUM(COALESCE(result_size_bytes, 0)) AS total_result_bytes,
  AVG(result_size_bytes) AS avg_result_bytes,
  SUM(COALESCE(result_tokens, 0)) AS total_result_tokens
FROM tool_calls
WHERE tool_name IS NOT NULL
GROUP BY tool_name, mcp_server_name, mcp_tool_name;

-- Cost and tokens spent on API requests that needed more than one attempt
-- (attempt > 1) — a call that failed and was paid for again. A second,
-- distinct waste channel from v_wasted_spend's rejected-tool-call cost,
-- grouped the same way (session_id + day) so it can sit beside it.
CREATE VIEW v_retry_waste AS
SELECT
  session_id,
  date(ts / 1000, 'unixepoch') AS day,
  COUNT(*) AS retried_requests,
  SUM(COALESCE(cost_usd_micros, 0)) / 1000000.0 AS retried_cost_usd,
  SUM(COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0)
    + COALESCE(cache_read_tokens, 0) + COALESCE(cache_creation_tokens, 0)) AS retried_tokens
FROM api_requests
WHERE attempt > 1 AND session_id IS NOT NULL
GROUP BY session_id, day;
