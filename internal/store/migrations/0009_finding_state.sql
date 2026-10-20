-- 0009_finding_state: per-finding user state for the Fix page (Dismiss /
-- Snooze / Mark fixed). Absence of a row means "open" — this table only
-- ever records a deviation from the default, computed state.
CREATE TABLE finding_state (
  finding_id    TEXT PRIMARY KEY,
  state         TEXT NOT NULL,   -- 'dismissed' | 'snoozed' | 'fixed'
  snoozed_until INTEGER,         -- epoch ms; NULL unless state='snoozed'
  updated_at    INTEGER NOT NULL,
  CHECK (state IN ('dismissed','snoozed','fixed'))
);
