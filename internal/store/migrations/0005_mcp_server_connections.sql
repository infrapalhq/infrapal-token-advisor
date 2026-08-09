-- 0005_mcp_server_connections: typed landing table for claude_code.
-- mcp_server_connection events, which had no typed table of their own
-- before this (they only landed in events_raw). Needed by v_mcp_inventory
-- (migration 0006) to tell "server connected" apart from "server had a
-- tool called" — the two facts live in different event types entirely.
-- Insert-only: each connection/disconnection is its own event, not a
-- record to be merged with anything else, so there's no upsert key here.

CREATE TABLE mcp_server_connections (
  id INTEGER PRIMARY KEY,
  ts INTEGER,
  session_id TEXT,
  server_name TEXT,
  status TEXT,            -- connected|failed|disconnected
  transport_type TEXT,
  server_scope TEXT,
  is_plugin INTEGER,
  plugin_name TEXT,
  duration_ms INTEGER,
  error_code TEXT
);

CREATE INDEX idx_mcp_server_connections_ts ON mcp_server_connections(ts);
CREATE INDEX idx_mcp_server_connections_session_id ON mcp_server_connections(session_id);
CREATE INDEX idx_mcp_server_connections_server_name ON mcp_server_connections(server_name);
