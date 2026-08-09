-- 0006_derived_views: the milestone-5 derived views, kept as plain SQL VIEWs
-- per the spec so the HTTP API layer stays thin (SELECT * FROM v_foo, no
-- business logic in Go).
--
-- v_skill_effectiveness (spec's "skill invocations split by
-- invocation_trigger") is deliberately NOT created here. invocation_trigger
-- doesn't exist anywhere in the current upstream schema — the only
-- skill-related fields are skill_name/skill.name, with no signal for
-- proactive vs. explicit invocation. Re-confirmed live against
-- code.claude.com/docs/en/monitoring-usage during milestone 5 planning; see
-- HANDOFF.md's "Milestone 3 design decisions, resolved" for the original
-- decision to drop this view.

-- Per prompt_id: prompt length, LLM turn count, token/cost totals, cache hit
-- ratio, tool call stats, and two derived booleans (triggered_compaction,
-- hit_max_tokens) read off any api_requests row sharing that prompt_id.
-- prompt_length passes straight through from interactions — the UI's
-- efficiency scatter (prompt length vs. turn count) needs it and the view
-- is otherwise the only per-prompt aggregate the API exposes.
--
-- cache_hit_ratio = cache_read_tokens / (cache_read_tokens + cache_creation_tokens):
-- the fraction of this prompt's cacheable context that was actually served
-- from cache rather than rebuilt. NULL (not zero) when neither happened, so
-- consumers can tell "no cacheable context" from "cache missed entirely".
--
-- triggered_compaction is a heuristic: true if any api_requests row for this
-- prompt_id has query_source='compact'. Compaction is only observable via
-- that query_source value; there's no dedicated "compaction happened" event.
CREATE VIEW v_prompt_efficiency AS
SELECT
  i.prompt_id,
  i.session_id,
  i.started_at,
  i.duration_ms,
  i.prompt_length,
  COALESCE(ar.turn_count, 0) AS llm_turn_count,
  COALESCE(ar.total_tokens, 0) AS total_tokens,
  COALESCE(ar.total_cost_usd, 0.0) AS total_cost_usd,
  ar.cache_hit_ratio,
  COALESCE(tc.tool_call_count, 0) AS tool_call_count,
  COALESCE(tc.rejected_count, 0) AS rejected_count,
  COALESCE(tc.tool_error_count, 0) AS tool_error_count,
  COALESCE(tc.blocked_on_user_ms, 0) AS blocked_on_user_ms,
  COALESCE(ar.triggered_compaction, 0) AS triggered_compaction,
  COALESCE(ar.hit_max_tokens, 0) AS hit_max_tokens
FROM interactions i
LEFT JOIN (
  SELECT
    prompt_id,
    COUNT(*) AS turn_count,
    SUM(COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0)
      + COALESCE(cache_read_tokens, 0) + COALESCE(cache_creation_tokens, 0)) AS total_tokens,
    SUM(COALESCE(cost_usd_micros, 0)) / 1000000.0 AS total_cost_usd,
    CASE WHEN SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_creation_tokens, 0)) > 0
      THEN CAST(SUM(COALESCE(cache_read_tokens, 0)) AS REAL)
        / SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_creation_tokens, 0))
      ELSE NULL END AS cache_hit_ratio,
    MAX(CASE WHEN query_source = 'compact' THEN 1 ELSE 0 END) AS triggered_compaction,
    MAX(CASE WHEN stop_reason = 'max_tokens' THEN 1 ELSE 0 END) AS hit_max_tokens
  FROM api_requests
  GROUP BY prompt_id
) ar ON ar.prompt_id = i.prompt_id
LEFT JOIN (
  SELECT
    prompt_id,
    COUNT(*) AS tool_call_count,
    SUM(CASE WHEN decision = 'reject' THEN 1 ELSE 0 END) AS rejected_count,
    SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) AS tool_error_count,
    SUM(COALESCE(blocked_on_user_ms, 0)) AS blocked_on_user_ms
  FROM tool_calls
  GROUP BY prompt_id
) tc ON tc.prompt_id = i.prompt_id;

-- Cost of API requests belonging to a prompt that had at least one rejected
-- tool call, grouped by session and by day. Attribution is at the whole-
-- prompt level (a prompt's entire api_requests cost counts as "wasted" if
-- any of its tool calls was rejected) since there's no per-tool-call link to
-- a specific api_requests row — tool spans/events carry no request_id.
-- prompt_cost is pre-aggregated per prompt_id before joining to
-- rejected_prompts so a prompt with multiple rejected tool calls doesn't
-- fan out and multiply its cost.
CREATE VIEW v_wasted_spend AS
WITH rejected_prompts AS (
  SELECT DISTINCT prompt_id FROM tool_calls WHERE decision = 'reject' AND prompt_id IS NOT NULL
),
prompt_cost AS (
  SELECT prompt_id, SUM(COALESCE(cost_usd_micros, 0)) AS cost_usd_micros
  FROM api_requests
  WHERE prompt_id IS NOT NULL
  GROUP BY prompt_id
),
rejected_counts AS (
  SELECT prompt_id, COUNT(*) AS rejected_count
  FROM tool_calls
  WHERE decision = 'reject' AND prompt_id IS NOT NULL
  GROUP BY prompt_id
)
SELECT
  i.session_id,
  date(i.started_at / 1000, 'unixepoch') AS day,
  SUM(COALESCE(pc.cost_usd_micros, 0)) / 1000000.0 AS wasted_cost_usd,
  SUM(COALESCE(rc.rejected_count, 0)) AS rejected_tool_count
FROM rejected_prompts rp
JOIN interactions i ON i.prompt_id = rp.prompt_id
LEFT JOIN prompt_cost pc ON pc.prompt_id = rp.prompt_id
LEFT JOIN rejected_counts rc ON rc.prompt_id = rp.prompt_id
GROUP BY i.session_id, day;

-- Permission friction per tool + "parameter shape". The spec's literal
-- wording ("per tool + parameter shape") has no data source finer than
-- tool identity in the current schema — there's no stable shape descriptor
-- for e.g. a Bash command's arguments. Interpreted here as (tool_name,
-- mcp_server_name, mcp_tool_name), which is the closest thing to a stable
-- "shape" ccotel actually has: it distinguishes MCP tools by server+tool
-- identity while still grouping built-in tools like Edit/Bash by name alone.
--
-- Also carries success_count/error_count/avg_duration_ms: the spec's Tools &
-- MCP page needs per-tool usage/success-rate/latency, and this is the only
-- view already grouped by tool identity, so it does double duty rather than
-- adding a near-identical view over the same table.
CREATE VIEW v_permission_friction AS
SELECT
  tool_name,
  mcp_server_name,
  mcp_tool_name,
  COUNT(*) AS total_calls,
  SUM(CASE WHEN decision_source = 'user_temporary' THEN 1 ELSE 0 END) AS user_temporary_count,
  SUM(CASE WHEN decision_source = 'user_permanent' THEN 1 ELSE 0 END) AS user_permanent_count,
  SUM(COALESCE(blocked_on_user_ms, 0)) AS total_blocked_on_user_ms,
  SUM(CASE WHEN success = 1 THEN 1 ELSE 0 END) AS success_count,
  SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) AS error_count,
  AVG(duration_ms) AS avg_duration_ms
FROM tool_calls
WHERE tool_name IS NOT NULL
GROUP BY tool_name, mcp_server_name, mcp_tool_name;

-- Every MCP server that connected vs. every server that actually had a tool
-- called, so the difference (connected, zero tool_call_count) surfaces dead
-- config costing tool-definition tokens on every request. Combines the two
-- sides with LEFT JOIN + UNION rather than FULL OUTER JOIN for portability
-- across SQLite builds that predate 3.39's FULL OUTER JOIN support.
CREATE VIEW v_mcp_inventory AS
WITH connections AS (
  SELECT server_name, COUNT(*) AS connection_count, MAX(ts) AS last_connected_at
  FROM mcp_server_connections
  WHERE status = 'connected' AND server_name IS NOT NULL
  GROUP BY server_name
),
tool_usage AS (
  SELECT
    mcp_server_name AS server_name,
    COUNT(*) AS tool_call_count,
    COUNT(DISTINCT mcp_tool_name) AS distinct_tools_called
  FROM tool_calls
  WHERE mcp_server_name IS NOT NULL
  GROUP BY mcp_server_name
)
SELECT
  c.server_name AS server_name,
  COALESCE(c.connection_count, 0) AS connection_count,
  c.last_connected_at AS last_connected_at,
  COALESCE(u.tool_call_count, 0) AS tool_call_count,
  COALESCE(u.distinct_tools_called, 0) AS distinct_tools_called
FROM connections c
LEFT JOIN tool_usage u ON u.server_name = c.server_name
UNION
SELECT
  u.server_name AS server_name,
  0 AS connection_count,
  NULL AS last_connected_at,
  u.tool_call_count AS tool_call_count,
  u.distinct_tools_called AS distinct_tools_called
FROM tool_usage u
LEFT JOIN connections c ON c.server_name = u.server_name
WHERE c.server_name IS NULL;

-- Per session: duration, cost, tokens, tool calls, commits, PRs, lines
-- added/removed, active time, and compaction count. Cost/tokens/commits/PRs/
-- lines/active-time all come from metric_points, summed per session — safe
-- because Claude Code's metrics are delta temporality (gotcha #1: points are
-- increments, summing deltas gives the running total). lines_of_code and
-- active_time are split by their "type" attribute (added/removed,
-- user/cli respectively) via json_extract on the stored attrs JSON.
CREATE VIEW v_session_summary AS
SELECT
  s.session_id,
  s.started_at,
  s.ended_at,
  (s.ended_at - s.started_at) AS duration_ms,
  COALESCE(cost.total_cost_usd, 0.0) AS total_cost_usd,
  COALESCE(tok.total_tokens, 0) AS total_tokens,
  COALESCE(loc_added.lines, 0) AS lines_added,
  COALESCE(loc_removed.lines, 0) AS lines_removed,
  COALESCE(commits.n, 0) AS commit_count,
  COALESCE(prs.n, 0) AS pull_request_count,
  COALESCE(active_user.seconds, 0) AS active_time_user_s,
  COALESCE(active_cli.seconds, 0) AS active_time_cli_s,
  COALESCE(tc.tool_call_count, 0) AS tool_call_count,
  COALESCE(compaction.n, 0) AS compaction_count
FROM sessions s
LEFT JOIN (
  SELECT session_id, SUM(value) AS total_cost_usd FROM metric_points
  WHERE name = 'claude_code.cost.usage' GROUP BY session_id
) cost ON cost.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, SUM(value) AS total_tokens FROM metric_points
  WHERE name = 'claude_code.token.usage' GROUP BY session_id
) tok ON tok.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, SUM(value) AS lines FROM metric_points
  WHERE name = 'claude_code.lines_of_code.count' AND json_extract(attrs, '$.type') = 'added'
  GROUP BY session_id
) loc_added ON loc_added.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, SUM(value) AS lines FROM metric_points
  WHERE name = 'claude_code.lines_of_code.count' AND json_extract(attrs, '$.type') = 'removed'
  GROUP BY session_id
) loc_removed ON loc_removed.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, SUM(value) AS n FROM metric_points
  WHERE name = 'claude_code.commit.count' GROUP BY session_id
) commits ON commits.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, SUM(value) AS n FROM metric_points
  WHERE name = 'claude_code.pull_request.count' GROUP BY session_id
) prs ON prs.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, SUM(value) AS seconds FROM metric_points
  WHERE name = 'claude_code.active_time.total' AND json_extract(attrs, '$.type') = 'user'
  GROUP BY session_id
) active_user ON active_user.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, SUM(value) AS seconds FROM metric_points
  WHERE name = 'claude_code.active_time.total' AND json_extract(attrs, '$.type') = 'cli'
  GROUP BY session_id
) active_cli ON active_cli.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, COUNT(*) AS tool_call_count FROM tool_calls GROUP BY session_id
) tc ON tc.session_id = s.session_id
LEFT JOIN (
  SELECT session_id, COUNT(*) AS n FROM api_requests WHERE query_source = 'compact' GROUP BY session_id
) compaction ON compaction.session_id = s.session_id;

-- p50/p95 of ttft_ms and duration_ms, plus error rate, grouped by
-- (model, effort, speed). SQLite has no PERCENTILE_CONT/DISC, so percentiles
-- are computed via PERCENT_RANK() (a core window function since SQLite
-- 3.25, no extension needed) and taking the smallest value whose rank meets
-- the target percentile. effort/speed are frequently NULL (unsupported by a
-- given model), so every join on them uses IS rather than = to stay
-- NULL-safe.
CREATE VIEW v_model_performance AS
WITH base AS (
  SELECT model, effort, speed, ttft_ms, duration_ms, success
  FROM api_requests
  WHERE model IS NOT NULL
),
ttft_ranked AS (
  SELECT model, effort, speed, ttft_ms,
    PERCENT_RANK() OVER (PARTITION BY model, effort, speed ORDER BY ttft_ms) AS pr
  FROM base WHERE ttft_ms IS NOT NULL
),
dur_ranked AS (
  SELECT model, effort, speed, duration_ms,
    PERCENT_RANK() OVER (PARTITION BY model, effort, speed ORDER BY duration_ms) AS pr
  FROM base WHERE duration_ms IS NOT NULL
),
ttft_p50 AS (
  SELECT model, effort, speed, MIN(ttft_ms) AS p50_ttft_ms
  FROM ttft_ranked WHERE pr >= 0.5 GROUP BY model, effort, speed
),
ttft_p95 AS (
  SELECT model, effort, speed, MIN(ttft_ms) AS p95_ttft_ms
  FROM ttft_ranked WHERE pr >= 0.95 GROUP BY model, effort, speed
),
dur_p50 AS (
  SELECT model, effort, speed, MIN(duration_ms) AS p50_duration_ms
  FROM dur_ranked WHERE pr >= 0.5 GROUP BY model, effort, speed
),
dur_p95 AS (
  SELECT model, effort, speed, MIN(duration_ms) AS p95_duration_ms
  FROM dur_ranked WHERE pr >= 0.95 GROUP BY model, effort, speed
),
counts AS (
  SELECT model, effort, speed,
    COUNT(*) AS total_requests,
    SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) AS error_count
  FROM base
  GROUP BY model, effort, speed
)
SELECT
  c.model, c.effort, c.speed,
  c.total_requests,
  c.error_count,
  CAST(c.error_count AS REAL) / NULLIF(c.total_requests, 0) AS error_rate,
  tp50.p50_ttft_ms, tp95.p95_ttft_ms,
  dp50.p50_duration_ms, dp95.p95_duration_ms
FROM counts c
LEFT JOIN ttft_p50 tp50 ON tp50.model = c.model AND tp50.effort IS c.effort AND tp50.speed IS c.speed
LEFT JOIN ttft_p95 tp95 ON tp95.model = c.model AND tp95.effort IS c.effort AND tp95.speed IS c.speed
LEFT JOIN dur_p50 dp50 ON dp50.model = c.model AND dp50.effort IS c.effort AND dp50.speed IS c.speed
LEFT JOIN dur_p95 dp95 ON dp95.model = c.model AND dp95.effort IS c.effort AND dp95.speed IS c.speed;
