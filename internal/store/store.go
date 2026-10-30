// Package store owns the SQLite database: opening it with the right
// pragmas and permissions, and keeping its schema current via migrations.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Open opens (creating if necessary) the SQLite database at dbPath, applies
// pragmas, brings the schema up to date, and locks the file down to
// owner-only permissions.
func Open(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("create db directory: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	// SQLite allows only one writer at a time; infrapal-token-advisor's three ingest
	// writers (logs, metrics, spans) flush concurrently on independent
	// tickers. Without this, database/sql opens multiple connections and
	// two concurrent write transactions race for the same file lock,
	// occasionally exceeding busy_timeout and surfacing as SQLITE_BUSY.
	// Pinning to one connection serializes all access through Go's own
	// connection pool instead, which never fails.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}

	if err := Migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	if err := os.Chmod(dbPath, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("chmod db file: %w", err)
	}

	return db, nil
}
