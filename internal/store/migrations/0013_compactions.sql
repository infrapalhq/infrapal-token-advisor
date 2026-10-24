-- 0013_compactions: typed table for compaction events, which previously only
-- landed in events_raw. Carries the before/after token counts that make the
-- Context panel's "compacted" claim a measurement instead of a guess.
CREATE TABLE compactions (
  id           INTEGER PRIMARY KEY,
  session_id   TEXT NOT NULL,
  prompt_id    TEXT,
  ts           INTEGER NOT NULL,
  pre_tokens   INTEGER,
  post_tokens  INTEGER,
  trigger      TEXT,
  duration_ms  INTEGER,
  UNIQUE (session_id, ts)
);

CREATE INDEX idx_compactions_session_id ON compactions(session_id);

-- Backfill from raw events already captured before this migration.
INSERT OR IGNORE INTO compactions (session_id, prompt_id, ts, pre_tokens, post_tokens, trigger, duration_ms)
SELECT
  session_id,
  prompt_id,
  ts,
  CAST(json_extract(body, '$.attributes.pre_tokens') AS INTEGER),
  CAST(json_extract(body, '$.attributes.post_tokens') AS INTEGER),
  json_extract(body, '$.attributes.trigger'),
  CAST(json_extract(body, '$.attributes.duration_ms') AS INTEGER)
FROM events_raw
WHERE event_name IN ('compaction', 'claude_code.compaction')
  AND session_id IS NOT NULL;
