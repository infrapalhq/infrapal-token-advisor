-- 0008_ai_recommendations: cache/history table for AI Mode (internal/ai),
-- the optional Anthropic-backed recommendation layer on top of the
-- deterministic insights already computed in Go. Purely additive — no
-- existing table or view changes.
--
-- One row = one recommendation, not one whole "Generate" batch, so the API
-- can SELECT it the same way the other derived-view passthroughs do.
-- batch_id groups every row produced by one Generate click.
--
-- Caching key is (since_range, input_hash): a click that reuses the same
-- date range against unchanged underlying data (same input_hash) is served
-- from the most recent matching batch instead of calling Anthropic again
-- (avoids spending the user's own API budget on a no-op). "Force
-- regenerate" bypasses the cache lookup and always inserts a new batch,
-- regardless of input_hash.
CREATE TABLE ai_recommendations (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  batch_id      TEXT NOT NULL,
  generated_at  INTEGER NOT NULL,  -- epoch ms
  since_range   TEXT NOT NULL,     -- the ?since= value used, e.g. "30d"; "" means all-time
  input_hash    TEXT NOT NULL,     -- sha256 hex of the preprocessed context bundle (internal/ai.Context.Hash)
  model         TEXT NOT NULL,     -- Anthropic model id used, e.g. "claude-haiku-4-5"
  category      TEXT NOT NULL,
  severity      TEXT NOT NULL,
  title         TEXT NOT NULL,
  detail        TEXT NOT NULL,
  evidence_json TEXT NOT NULL,     -- JSON array of {label, value} facts, already grounding-validated
  CHECK (category IN ('efficiency','sessions','waste','tools')),
  CHECK (severity IN ('info','warn'))
);

CREATE INDEX idx_ai_recommendations_lookup ON ai_recommendations (since_range, input_hash, generated_at);
CREATE INDEX idx_ai_recommendations_batch ON ai_recommendations (batch_id);
