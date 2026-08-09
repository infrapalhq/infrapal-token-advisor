package ingest

import "database/sql"

// insertMCPServerConnection projects claude_code.mcp_server_connection.
// Unlike tool_calls, there's no natural key to upsert on here — a server
// can connect, disconnect, and reconnect repeatedly within one session, and
// each of those is its own row, not fields of one merged record.
func insertMCPServerConnection(tx *sql.Tx, e RawEvent) error {
	_, err := tx.Exec(`
		INSERT INTO mcp_server_connections (
			ts, session_id, server_name, status, transport_type,
			server_scope, is_plugin, plugin_name, duration_ms, error_code
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		e.Ts, nullableString(e.SessionID), nullStringAttr(e.Attrs, "server_name"),
		nullStringAttr(e.Attrs, "status"), nullStringAttr(e.Attrs, "transport_type"),
		nullStringAttr(e.Attrs, "server_scope"), nullBoolAttr(e.Attrs, "is_plugin"),
		nullStringAttr(e.Attrs, "plugin.name"), nullInt64Attr(e.Attrs, "duration_ms"),
		nullStringAttr(e.Attrs, "error_code"),
	)
	return err
}
