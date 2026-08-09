package ingest

import (
	"database/sql"
	"testing"
)

func TestMCPServerConnection_Inserted(t *testing.T) {
	db := openTestDB(t)

	e := mkEvent("claude_code.mcp_server_connection", "sess-1", "", map[string]any{
		"server_name":    "github",
		"status":         "connected",
		"transport_type": "stdio",
		"server_scope":   "user",
		"is_plugin":      false,
		"duration_ms":    int64(120),
	})
	if err := writeLogsBatch(db, []RawEvent{e}); err != nil {
		t.Fatalf("write mcp_server_connection: %v", err)
	}

	var serverName, status, transportType, serverScope string
	var isPlugin sql.NullInt64
	var durationMs sql.NullInt64
	err := db.QueryRow(`
		SELECT server_name, status, transport_type, server_scope, is_plugin, duration_ms
		FROM mcp_server_connections WHERE session_id = ?
	`, "sess-1").Scan(&serverName, &status, &transportType, &serverScope, &isPlugin, &durationMs)
	if err != nil {
		t.Fatalf("query mcp_server_connections: %v", err)
	}
	if serverName != "github" {
		t.Errorf("server_name = %q, want github", serverName)
	}
	if status != "connected" {
		t.Errorf("status = %q, want connected", status)
	}
	if transportType != "stdio" {
		t.Errorf("transport_type = %q, want stdio", transportType)
	}
	if serverScope != "user" {
		t.Errorf("server_scope = %q, want user", serverScope)
	}
	if !isPlugin.Valid || isPlugin.Int64 != 0 {
		t.Errorf("is_plugin = %v, want 0", isPlugin)
	}
	if !durationMs.Valid || durationMs.Int64 != 120 {
		t.Errorf("duration_ms = %v, want 120", durationMs)
	}
}

// TestMCPServerConnection_MultipleRowsPerServer confirms reconnect events
// each land as their own row rather than being merged/upserted — a server
// can connect, disconnect, and reconnect within one session.
func TestMCPServerConnection_MultipleRowsPerServer(t *testing.T) {
	db := openTestDB(t)

	connected := mkEvent("claude_code.mcp_server_connection", "sess-1", "", map[string]any{
		"server_name": "github",
		"status":      "connected",
	})
	disconnected := mkEvent("claude_code.mcp_server_connection", "sess-1", "", map[string]any{
		"server_name": "github",
		"status":      "disconnected",
	})
	if err := writeLogsBatch(db, []RawEvent{connected, disconnected}); err != nil {
		t.Fatalf("write mcp_server_connections: %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM mcp_server_connections WHERE server_name = ?`, "github").Scan(&count); err != nil {
		t.Fatalf("query mcp_server_connections: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows (connect + disconnect), got %d", count)
	}
}
