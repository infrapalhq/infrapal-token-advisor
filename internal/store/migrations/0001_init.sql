-- 0001_init: base schema for token-advisor's OTLP-derived tables.
--
-- Two-phase write model: events_raw is the catch-all landing zone written
-- before projection into the typed tables below. Typed columns are allowed
-- to be NULL — an unknown or not-yet-arrived attribute must never block a
-- write or lose data, it just leaves the column empty until backfilled.

CREATE TABLE sessions (
  session_id TEXT PRIMARY KEY,
  started_at INTEGER,
  ended_at INTEGER,
  start_type TEXT,
  app_version TEXT,
  app_entrypoint TEXT,
  user_id TEXT,
  user_email TEXT,
  org_id TEXT,
  terminal_type TEXT,
  resource_attrs TEXT           -- JSON: everything else off the resource, incl. custom OTEL_RESOURCE_ATTRIBUTES
);

CREATE INDEX idx_sessions_started_at ON sessions(started_at);

CREATE TABLE interactions (      -- one row per prompt.id
  prompt_id TEXT PRIMARY KEY,
  session_id TEXT,
  seq INTEGER,
  started_at INTEGER,
  duration_ms INTEGER,
  prompt_text TEXT,
  prompt_length INTEGER,
  command_name TEXT,
  command_source TEXT,
  message_uuid TEXT,
  trace_id TEXT,
  root_span_id TEXT
);

CREATE INDEX idx_interactions_session_id ON interactions(session_id);
CREATE INDEX idx_interactions_started_at ON interactions(started_at);

CREATE TABLE assistant_responses (
  message_uuid TEXT PRIMARY KEY,
  prompt_id TEXT,
  session_id TEXT,
  ts INTEGER,
  model TEXT,
  request_id TEXT,
  query_source TEXT,
  response_text TEXT,
  response_length INTEGER
);

CREATE INDEX idx_assistant_responses_prompt_id ON assistant_responses(prompt_id);
CREATE INDEX idx_assistant_responses_session_id ON assistant_responses(session_id);
CREATE INDEX idx_assistant_responses_ts ON assistant_responses(ts);

CREATE TABLE api_requests (
  id INTEGER PRIMARY KEY,
  request_id TEXT,
  client_request_id TEXT,
  prompt_id TEXT,
  session_id TEXT,
  ts INTEGER,
  model TEXT,
  query_source TEXT,
  speed TEXT,
  effort TEXT,
  input_tokens INTEGER,
  output_tokens INTEGER,
  cache_read_tokens INTEGER,
  cache_creation_tokens INTEGER,
  cost_usd_micros INTEGER,
  duration_ms INTEGER,
  ttft_ms INTEGER,
  stop_reason TEXT,
  attempt INTEGER,
  success INTEGER,
  status_code INTEGER,
  error TEXT,
  agent_name TEXT,
  skill_name TEXT,
  plugin_name TEXT,
  marketplace_name TEXT,
  mcp_server_name TEXT,
  mcp_tool_name TEXT,
  request_body_ref TEXT,
  response_body_ref TEXT
);

CREATE INDEX idx_api_requests_request_id ON api_requests(request_id);
CREATE INDEX idx_api_requests_client_request_id ON api_requests(client_request_id);
CREATE INDEX idx_api_requests_prompt_id ON api_requests(prompt_id);
CREATE INDEX idx_api_requests_session_id ON api_requests(session_id);
CREATE INDEX idx_api_requests_ts ON api_requests(ts);

CREATE TABLE tool_calls (
  tool_use_id TEXT PRIMARY KEY,
  prompt_id TEXT,
  session_id TEXT,
  ts INTEGER,
  tool_name TEXT,
  tool_source TEXT,
  decision TEXT,
  decision_source TEXT,
  blocked_on_user_ms INTEGER,
  execution_ms INTEGER,
  duration_ms INTEGER,
  success INTEGER,
  error_type TEXT,
  error TEXT,
  input_size_bytes INTEGER,
  result_size_bytes INTEGER,
  result_tokens INTEGER,
  mcp_server_name TEXT,
  mcp_tool_name TEXT,
  mcp_server_scope TEXT,
  marketplace_name TEXT,
  skill_name TEXT,
  subagent_type TEXT,
  file_path TEXT,
  full_command TEXT,
  git_commit_id TEXT,           -- unconfirmed in current upstream schema; left for forward-compat, stays NULL if unused
  tool_input TEXT               -- JSON
);

CREATE INDEX idx_tool_calls_prompt_id ON tool_calls(prompt_id);
CREATE INDEX idx_tool_calls_session_id ON tool_calls(session_id);
CREATE INDEX idx_tool_calls_ts ON tool_calls(ts);

CREATE TABLE spans (
  span_id TEXT PRIMARY KEY,
  trace_id TEXT,
  parent_span_id TEXT,
  span_type TEXT,
  name TEXT,
  start_ns INTEGER,
  end_ns INTEGER,
  status TEXT,
  session_id TEXT,
  prompt_id TEXT,
  agent_id TEXT,
  parent_agent_id TEXT,
  attrs TEXT                    -- JSON
);

CREATE INDEX idx_spans_trace_id ON spans(trace_id);
CREATE INDEX idx_spans_parent_span_id ON spans(parent_span_id);
CREATE INDEX idx_spans_session_id ON spans(session_id);
CREATE INDEX idx_spans_prompt_id ON spans(prompt_id);
CREATE INDEX idx_spans_start_ns ON spans(start_ns);

CREATE TABLE metric_points (
  id INTEGER PRIMARY KEY,
  ts INTEGER,
  name TEXT,
  value REAL,
  session_id TEXT,
  model TEXT,
  attrs TEXT                    -- JSON
);

CREATE INDEX idx_metric_points_ts ON metric_points(ts);
CREATE INDEX idx_metric_points_session_id ON metric_points(session_id);
CREATE INDEX idx_metric_points_name ON metric_points(name);

CREATE TABLE events_raw (        -- catch-all, written before projection
  id INTEGER PRIMARY KEY,
  ts INTEGER,
  session_id TEXT,
  prompt_id TEXT,
  event_name TEXT,
  seq INTEGER,
  body TEXT                     -- JSON: the fully decoded log record, verbatim
);

CREATE INDEX idx_events_raw_ts ON events_raw(ts);
CREATE INDEX idx_events_raw_session_id ON events_raw(session_id);
CREATE INDEX idx_events_raw_prompt_id ON events_raw(prompt_id);
CREATE INDEX idx_events_raw_event_name ON events_raw(event_name);

-- Full-text search over the two content-bearing columns the Search page
-- needs to hit. External-content FTS5 tables kept in sync via triggers so
-- application code never has to double-write.

CREATE VIRTUAL TABLE interactions_fts USING fts5(
  prompt_text,
  content='interactions',
  content_rowid='rowid'
);

CREATE TRIGGER interactions_fts_ai AFTER INSERT ON interactions BEGIN
  INSERT INTO interactions_fts(rowid, prompt_text) VALUES (new.rowid, new.prompt_text);
END;

CREATE TRIGGER interactions_fts_ad AFTER DELETE ON interactions BEGIN
  INSERT INTO interactions_fts(interactions_fts, rowid, prompt_text) VALUES ('delete', old.rowid, old.prompt_text);
END;

CREATE TRIGGER interactions_fts_au AFTER UPDATE ON interactions BEGIN
  INSERT INTO interactions_fts(interactions_fts, rowid, prompt_text) VALUES ('delete', old.rowid, old.prompt_text);
  INSERT INTO interactions_fts(rowid, prompt_text) VALUES (new.rowid, new.prompt_text);
END;

CREATE VIRTUAL TABLE assistant_responses_fts USING fts5(
  response_text,
  content='assistant_responses',
  content_rowid='rowid'
);

CREATE TRIGGER assistant_responses_fts_ai AFTER INSERT ON assistant_responses BEGIN
  INSERT INTO assistant_responses_fts(rowid, response_text) VALUES (new.rowid, new.response_text);
END;

CREATE TRIGGER assistant_responses_fts_ad AFTER DELETE ON assistant_responses BEGIN
  INSERT INTO assistant_responses_fts(assistant_responses_fts, rowid, response_text) VALUES ('delete', old.rowid, old.response_text);
END;

CREATE TRIGGER assistant_responses_fts_au AFTER UPDATE ON assistant_responses BEGIN
  INSERT INTO assistant_responses_fts(assistant_responses_fts, rowid, response_text) VALUES ('delete', old.rowid, old.response_text);
  INSERT INTO assistant_responses_fts(rowid, response_text) VALUES (new.rowid, new.response_text);
END;
