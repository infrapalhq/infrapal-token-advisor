package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenAndMigrate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "ccotel.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	v, err := CurrentVersion(db)
	if err != nil {
		t.Fatalf("CurrentVersion: %v", err)
	}
	if v != 6 {
		t.Fatalf("expected version 6, got %d", v)
	}

	info, err := os.Stat(dbPath)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected db perm 0600, got %o", info.Mode().Perm())
	}

	if _, err := db.Exec(`INSERT INTO interactions (prompt_id, session_id, prompt_text) VALUES ('p1', 's1', 'how do I center a div')`); err != nil {
		t.Fatalf("insert interaction: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO assistant_responses (message_uuid, prompt_id, session_id, response_text) VALUES ('m1', 'p1', 's1', 'use flexbox')`); err != nil {
		t.Fatalf("insert assistant_response: %v", err)
	}

	var promptID string
	row := db.QueryRow(`SELECT i.prompt_id FROM interactions_fts JOIN interactions i ON i.rowid = interactions_fts.rowid WHERE interactions_fts MATCH 'center'`)
	if err := row.Scan(&promptID); err != nil {
		t.Fatalf("fts search on interactions: %v", err)
	}
	if promptID != "p1" {
		t.Fatalf("expected p1, got %s", promptID)
	}

	var respID string
	row2 := db.QueryRow(`SELECT a.message_uuid FROM assistant_responses_fts JOIN assistant_responses a ON a.rowid = assistant_responses_fts.rowid WHERE assistant_responses_fts MATCH 'flexbox'`)
	if err := row2.Scan(&respID); err != nil {
		t.Fatalf("fts search on assistant_responses: %v", err)
	}
	if respID != "m1" {
		t.Fatalf("expected m1, got %s", respID)
	}

	// Re-opening should be idempotent (no re-apply, no error).
	db.Close()
	db2, err := Open(dbPath)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	defer db2.Close()
	v2, err := CurrentVersion(db2)
	if err != nil || v2 != 6 {
		t.Fatalf("expected version 6 after reopen, got %d, err %v", v2, err)
	}
}
