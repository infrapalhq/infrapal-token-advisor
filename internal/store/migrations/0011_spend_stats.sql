-- 0011_spend_stats: two views for signals that are real but have no
-- runnable command behind them, so they don't qualify as a Fix-page
-- Finding (see internal/api/findings.go's computeFindings comment) — the
-- Spend page reads these directly instead.

-- v_session_cache_health: per-session cache hit ratio. Spend's aggregate
-- "Cache hit" tile can read healthy (high hit ratio) while one individual
-- session's context churned the whole time; this lets that outlier surface
-- instead of being averaged away.
CREATE VIEW v_session_cache_health AS
SELECT
  session_id,
  SUM(COALESCE(cache_read_tokens, 0)) AS cache_read_tokens,
  SUM(COALESCE(cache_creation_tokens, 0)) AS cache_creation_tokens,
  CASE WHEN SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_creation_tokens, 0)) > 0
    THEN CAST(SUM(COALESCE(cache_read_tokens, 0)) AS REAL)
      / SUM(COALESCE(cache_read_tokens, 0) + COALESCE(cache_creation_tokens, 0))
    ELSE NULL END AS cache_hit_ratio
FROM api_requests
WHERE session_id IS NOT NULL
GROUP BY session_id;

-- v_ancillary_model_spend: cost/tokens spent on short, backstage call sites
-- (title/summary/suggestion generation, identified by query_source) that
-- ran on a model other than Haiku. The query_source list here mirrors
-- ancillaryQuerySources's intent in internal/api/findings.go's history —
-- there's no lever to act on this (model routing for these call sites
-- isn't user-configurable), so it's a Spend stat, not a Fix finding.
CREATE VIEW v_ancillary_model_spend AS
SELECT
  query_source,
  model,
  COUNT(*) AS request_count,
  SUM(COALESCE(cost_usd_micros, 0)) / 1000000.0 AS cost_usd,
  SUM(COALESCE(input_tokens, 0) + COALESCE(output_tokens, 0)
    + COALESCE(cache_read_tokens, 0) + COALESCE(cache_creation_tokens, 0)) AS tokens
FROM api_requests
WHERE query_source IN ('away_summary', 'agent_summary', 'prompt_suggestion', 'side_question')
  AND model IS NOT NULL AND model NOT LIKE '%haiku%'
GROUP BY query_source, model;
