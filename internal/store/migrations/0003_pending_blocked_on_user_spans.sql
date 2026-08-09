-- 0003_pending_blocked_on_user_spans: staging table solving the same
-- out-of-order problem as pending_interaction_spans, but for
-- claude_code.tool.blocked_on_user spans. That span type carries no
-- tool_use_id of its own -- it must be read off its parent
-- claude_code.tool span's stored attrs via parent_span_id. Live testing
-- showed the child can arrive (and be written) before its parent within
-- the same flush batch, contradicting the ordering this table's
-- predecessor logic assumed, so whichever side arrives first stashes
-- itself here keyed by parent_span_id; whichever arrives second reads and
-- clears the row.

CREATE TABLE pending_blocked_on_user_spans (
  parent_span_id TEXT PRIMARY KEY,
  duration_ms INTEGER,
  decision TEXT,
  decision_source TEXT
);
