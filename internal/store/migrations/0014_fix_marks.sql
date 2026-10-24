-- 0014_fix_marks: append-only history of "Mark as fixed" actions. Each row
-- snapshots the baseline metric at mark time so the Fixes page can measure
-- before -> after. finding_state (0009) keeps the latest state per finding and
-- is untouched; this table is where the measured history lives.
CREATE TABLE fix_marks (
  id            INTEGER PRIMARY KEY,
  finding_id    TEXT NOT NULL,       -- detector kind, e.g. "re_read"
  subject       TEXT NOT NULL DEFAULT '',  -- pattern / file / server the fix targets
  marked_at     INTEGER NOT NULL,    -- epoch ms
  baseline_json TEXT,                -- JSON snapshot of the metric at mark time
  measure_until INTEGER,             -- epoch ms: max(5 sessions, 7 days) window end
  sessions_after_mark INTEGER NOT NULL DEFAULT 0,
  state         TEXT NOT NULL DEFAULT 'fixed' CHECK (state IN ('fixed','reverted'))
);

CREATE INDEX idx_fix_marks_finding ON fix_marks(finding_id, subject, marked_at);
