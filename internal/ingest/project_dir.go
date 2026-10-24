package ingest

import (
	"database/sql"
	"os"
	"path/filepath"

	"github.com/infrapalhq/infrapal-token-advisor/internal/project"
)

// claudeProjectsDir is where Claude Code keeps per-session transcripts. It is
// a variable so tests can point it at a temp directory.
var claudeProjectsDir = defaultClaudeProjectsDir()

func defaultClaudeProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "projects")
}

// resolveSessionProject persists sessions.project_dir the first time a
// directory can be inferred for the session. Once set it is never changed.
// If nothing is known yet it leaves the column NULL, so a later batch can
// try again with more file paths.
func resolveSessionProject(tx *sql.Tx, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	var current sql.NullString
	err := tx.QueryRow(`SELECT project_dir FROM sessions WHERE session_id = ?`, sessionID).Scan(&current)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if current.Valid && current.String != "" {
		return nil
	}

	rows, err := tx.Query(`
		SELECT DISTINCT p FROM (
			SELECT COALESCE(file_path, json_extract(tool_input, '$.file_path')) AS p
			FROM tool_calls WHERE session_id = ?
		) WHERE p IS NOT NULL
		LIMIT 200`, sessionID)
	if err != nil {
		return err
	}
	var filePaths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		filePaths = append(filePaths, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	dir := project.Resolve(claudeProjectsDir, sessionID, filePaths)
	if dir == "" {
		return nil
	}
	_, err = tx.Exec(`UPDATE sessions SET project_dir = ? WHERE session_id = ? AND (project_dir IS NULL OR project_dir = '')`, dir, sessionID)
	return err
}

// BackfillProjectDirs resolves sessions.project_dir for every session captured
// before migration 0012 (or any session the live hook missed). Sessions that
// still resolve to nothing stay NULL and are retried on the next start.
// Returns how many sessions gained a directory.
func BackfillProjectDirs(db *sql.DB) (int, error) {
	rows, err := db.Query(`SELECT session_id FROM sessions WHERE project_dir IS NULL OR project_dir = ''`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	resolved := 0
	for _, id := range ids {
		tx, err := db.Begin()
		if err != nil {
			return resolved, err
		}
		if err := resolveSessionProject(tx, id); err != nil {
			tx.Rollback()
			return resolved, err
		}
		var dir sql.NullString
		if err := tx.QueryRow(`SELECT project_dir FROM sessions WHERE session_id = ?`, id).Scan(&dir); err != nil {
			tx.Rollback()
			return resolved, err
		}
		if err := tx.Commit(); err != nil {
			return resolved, err
		}
		if dir.Valid && dir.String != "" {
			resolved++
		}
	}
	return resolved, nil
}
