package ingest

import (
	"database/sql"
	"encoding/json"
)

// upsertSession ensures a sessions row exists for sessionID and merges in
// whatever standard resource attributes this record carries. There is no
// dedicated session-start or session-end event in Claude Code's telemetry
// schema (session start is only observable via the claude_code.session.count
// metric's start_type attribute, and there is no end signal at all), so
// every writer — logs, metrics, spans — calls this on every record that
// carries a session.id: session identity and metadata can show up first
// from any of the three signal types, and ended_at is tracked as a running
// last-seen timestamp rather than populated from a dedicated event.
func upsertSession(tx *sql.Tx, sessionID string, ts int64, attrs, resourceAttrs map[string]any) error {
	if sessionID == "" {
		return nil
	}

	var resourceJSON sql.NullString
	if len(resourceAttrs) > 0 {
		if b, err := json.Marshal(resourceAttrs); err == nil {
			resourceJSON = sql.NullString{String: string(b), Valid: true}
		}
	}

	// The earliest session ever captured marks the install date (ui_state
	// install_at, the baseline for "since install"). Keeps the minimum seen,
	// regardless of the order batches arrive in.
	if _, err := tx.Exec(`
		INSERT INTO ui_state (key, value) VALUES ('install_at', CAST(? AS TEXT))
		ON CONFLICT(key) DO UPDATE SET
			value = CAST(MIN(CAST(ui_state.value AS INTEGER), CAST(excluded.value AS INTEGER)) AS TEXT)`,
		ts); err != nil {
		return err
	}

	_, err := tx.Exec(`
		INSERT INTO sessions (
			session_id, started_at, ended_at, start_type, app_version,
			app_entrypoint, user_id, user_email, org_id, terminal_type, resource_attrs
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			started_at     = MIN(sessions.started_at, excluded.started_at),
			ended_at       = MAX(sessions.ended_at, excluded.ended_at),
			start_type     = COALESCE(excluded.start_type, sessions.start_type),
			app_version    = COALESCE(excluded.app_version, sessions.app_version),
			app_entrypoint = COALESCE(excluded.app_entrypoint, sessions.app_entrypoint),
			user_id        = COALESCE(excluded.user_id, sessions.user_id),
			user_email     = COALESCE(excluded.user_email, sessions.user_email),
			org_id         = COALESCE(excluded.org_id, sessions.org_id),
			terminal_type  = COALESCE(excluded.terminal_type, sessions.terminal_type),
			resource_attrs = COALESCE(excluded.resource_attrs, sessions.resource_attrs)
	`,
		sessionID, ts, ts,
		nullStringAttr(attrs, "start_type"),
		nullStringAttr(attrs, "app.version"),
		nullStringAttr(attrs, "app.entrypoint"),
		nullStringAttr(attrs, "user.id"),
		nullStringAttr(attrs, "user.email"),
		nullStringAttr(attrs, "organization.id"),
		nullStringAttr(attrs, "terminal.type"),
		resourceJSON,
	)
	return err
}
