-- 0002_pending_interaction_spans: staging table solving the interactions
-- <-> claude_code.interaction span correlation problem. The two sides
-- share only (trace_id, span_id) as a join key (user_prompt has no
-- span-independent field pointing at the interaction span, and the span
-- carries no prompt.id at all) and arrive on independent flush cycles, so
-- either can land first. Whichever side arrives second reads and clears
-- this row; whichever arrives first stashes itself here.

CREATE TABLE pending_interaction_spans (
  trace_id TEXT NOT NULL,
  span_id TEXT NOT NULL,
  duration_ms INTEGER,
  PRIMARY KEY (trace_id, span_id)
);
