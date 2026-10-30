package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ErrNewerDatabase means the database was written by a newer build.
var ErrNewerDatabase = errors.New("database is newer than this build")

//go:embed migrations/*.sql
var migrationFiles embed.FS

type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads every embedded .sql file, ordered by the numeric
// prefix in its filename (e.g. 0001_init.sql -> version 1).
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var migs []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			return nil, fmt.Errorf("migration filename %q missing NNNN_ prefix", e.Name())
		}
		version, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration filename %q has non-numeric prefix: %w", e.Name(), err)
		}
		data, err := migrationFiles.ReadFile(path.Join("migrations", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", e.Name(), err)
		}
		migs = append(migs, migration{version: version, name: e.Name(), sql: string(data)})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	for i := 1; i < len(migs); i++ {
		if migs[i].version == migs[i-1].version {
			return nil, fmt.Errorf("duplicate migration version %d (%s, %s)", migs[i].version, migs[i-1].name, migs[i].name)
		}
	}
	return migs, nil
}

const createSchemaMigrations = `
	CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at INTEGER NOT NULL DEFAULT (unixepoch())
	)`

// Migrate brings the database at dbPath up to the latest embedded schema
// version, tracking progress in schema_migrations. Safe to call on every
// startup: migrations already applied are skipped.
//
// A database that already holds data is backed up first, and it isn't touched
// if that fails. One written by a newer build is refused, since this build
// doesn't know what its schema means.
func Migrate(db *sql.DB, dbPath string) error {
	if _, err := db.Exec(createSchemaMigrations); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	current, err := CurrentVersion(db)
	if err != nil {
		return err
	}

	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	latest := 0
	if len(migs) > 0 {
		latest = migs[len(migs)-1].version
	}
	if current > latest {
		return fmt.Errorf("%w: database is at schema v%d, this build only knows v%d — update token-advisor", ErrNewerDatabase, current, latest)
	}
	if current == latest {
		return nil
	}

	var backup string
	if current > 0 {
		if backup, err = backupDatabase(db, dbPath, current, time.Now()); err != nil {
			return fmt.Errorf("back up database before migrating: %w", err)
		}
	}

	for _, m := range migs {
		if m.version <= current {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			if backup != "" {
				return fmt.Errorf("apply migration %s (a backup from before it is at %s): %w", m.name, backup, err)
			}
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
	}

	// Only once it's worked, so a failure keeps every way back. The upgrade
	// has succeeded either way, so this can't fail it.
	if err := pruneBackups(dbPath, backupsKept); err != nil {
		slog.Warn("couldn't remove old database backups", "err", err)
	}
	return nil
}

// applyMigration runs one migration in a transaction. BEGIN IMMEDIATE takes
// the write lock up front, and the version is checked again under it: another
// process opening the database at the same time may have just applied this
// migration, and repeating it would fail.
func applyMigration(db *sql.DB, m migration) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return err
	}
	defer conn.ExecContext(ctx, `ROLLBACK`) // a no-op once committed

	var applied int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, m.version,
	).Scan(&applied); err != nil {
		return err
	}
	if applied > 0 {
		return nil
	}

	if _, err := conn.ExecContext(ctx, m.sql); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, unixepoch())`,
		m.version, m.name,
	); err != nil {
		return err
	}
	_, err = conn.ExecContext(ctx, `COMMIT`)
	return err
}

// CurrentVersion returns the highest applied migration version, or 0 if
// none have been applied yet.
func CurrentVersion(db *sql.DB) (int, error) {
	var version sql.NullInt64
	err := db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("query schema_migrations: %w", err)
	}
	return int(version.Int64), nil
}
