-- 0016_user_waits: time spent waiting on the user's own approval decision.
-- Only hand decisions count (user_temporary / user_permanent / user_reject);
-- auto-approved calls carry a few ms of blocked time that isn't waiting on
-- anyone. Plan approval and clarifying questions are excluded because they
-- are designed to block. Each wait is capped at 10 min; the excess is idle
-- time (the user walked away), reported separately.
CREATE VIEW v_user_waits AS
SELECT
  tool_use_id,
  session_id,
  prompt_id,
  ts,
  tool_name,
  mcp_server_name,
  mcp_tool_name,
  full_command,
  file_path,
  decision_source,
  MIN(COALESCE(blocked_on_user_ms, 0), 600000) AS wait_ms,
  MAX(COALESCE(blocked_on_user_ms, 0) - 600000, 0) AS idle_ms
FROM tool_calls
WHERE decision_source IN ('user_temporary', 'user_permanent', 'user_reject')
  AND tool_name IS NOT NULL
  AND tool_name NOT IN ('AskUserQuestion', 'ExitPlanMode');
