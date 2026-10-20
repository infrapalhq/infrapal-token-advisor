-- 0010_turn_context: one view for the Trace page's per-turn context-growth
-- chart. Additive — no existing table or view changes.
--
-- The Optimize page (pre-redesign) plotted v_prompt_efficiency.total_tokens
-- per turn, which is a SUM across every api_requests row for that prompt —
-- i.e. spend, not context size (a single prompt can trigger several LLM
-- calls in a tool loop, each paying for the same growing context again).
-- The honest proxy for "how much context is this turn carrying" is the
-- PEAK of (input + cache_read + cache_creation) tokens across that turn's
-- requests, not their sum — that's what this view computes.
CREATE VIEW v_turn_context AS
SELECT
  i.prompt_id,
  i.session_id,
  i.seq,
  i.started_at,
  MAX(COALESCE(a.input_tokens, 0) + COALESCE(a.cache_read_tokens, 0) + COALESCE(a.cache_creation_tokens, 0)) AS peak_context_tokens,
  MAX(CASE WHEN a.query_source = 'compact' THEN 1 ELSE 0 END) AS triggered_compaction,
  MAX(CASE WHEN a.stop_reason = 'max_tokens' THEN 1 ELSE 0 END) AS hit_max_tokens
FROM interactions i
LEFT JOIN api_requests a ON a.prompt_id = i.prompt_id
GROUP BY i.prompt_id, i.session_id, i.seq, i.started_at;
