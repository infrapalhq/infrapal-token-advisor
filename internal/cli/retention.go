package cli

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

const (
	// retentionEvery is how often cleanup runs. It's checked against the time
	// of the last run, so a laptop that's often asleep still gets its cleanup.
	retentionEvery      = 24 * time.Hour
	retentionCheckEvery = time.Hour

	// vacuumFreeRatio is the share of the database that must be free pages
	// before cleanup rewrites the file. Below it, SQLite just reuses them.
	vacuumFreeRatio = 0.25

	// backupMaxAge is how long a pre-migration backup is kept.
	backupMaxAge = 30 * 24 * time.Hour

	retentionLastRunKey     = "retention_last_run"
	retentionLastDeletedKey = "retention_last_deleted"
)

// runRetentionLoop runs cleanup whenever it's due until ctx ends. months <= 0
// keeps everything.
//
// db must be the collector's own handle. store.Open pins it to one connection,
// so ingest waits behind a cleanup, with the writers' buffers holding telemetry
// meanwhile. On a second handle, a VACUUM could outlast ingest's busy timeout
// and lose a batch.
func runRetentionLoop(ctx context.Context, db *sql.DB, p paths.Paths, months int) {
	if months <= 0 {
		return
	}
	ticker := time.NewTicker(retentionCheckEvery)
	defer ticker.Stop()
	for {
		if now := time.Now(); retentionDue(db, now) {
			if err := runRetention(db, p, months, now); err != nil {
				slog.Error("retention cleanup failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// retentionDue reports whether the last cleanup was long enough ago.
func retentionDue(db *sql.DB, now time.Time) bool {
	last, ok := retentionState(db, retentionLastRunKey)
	return !ok || now.Sub(time.UnixMilli(last)) >= retentionEvery
}

// runRetention deletes everything older than months before now.
func runRetention(db *sql.DB, p paths.Paths, months int, now time.Time) error {
	cutoff := now.AddDate(0, -months, 0)

	rows, err := purgeData(db, true, cutoff.UnixMilli(), "", false)
	if err != nil {
		return err
	}
	files, err := removeOldBodies(p.BodiesDir, cutoff)
	if err != nil {
		return fmt.Errorf("remove old bodies: %w", err)
	}
	// A backup holds data as old as the database did, so it can't stay long.
	if _, err := store.RemoveBackups(p.DBPath, now.Add(-backupMaxAge)); err != nil {
		return fmt.Errorf("remove old database backups: %w", err)
	}
	// Disk space is a bonus: the data is already gone, so don't fail on this.
	vacuumed, err := vacuumIfWorthwhile(db)
	if err != nil {
		slog.Warn("retention couldn't reclaim disk space", "err", err)
	}
	if err := saveRetentionState(db, now, rows+files); err != nil {
		return err
	}
	slog.Info("retention cleanup", "months", months, "rows", rows, "body_files", files, "vacuumed", vacuumed)
	return nil
}

// removeOldBodies deletes raw body files last changed before cutoff. Only
// regular files go, so a symlink can't send a delete outside the directory.
func removeOldBodies(dir string, cutoff time.Time) (int64, error) {
	var removed int64
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		removed++
		return nil
	})
	return removed, err
}

// vacuumIfWorthwhile shrinks the database file once enough of it is free. It
// rewrites the whole file, so it isn't run after every small cleanup.
func vacuumIfWorthwhile(db *sql.DB) (bool, error) {
	var pages, free int64
	if err := db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		return false, err
	}
	if err := db.QueryRow(`PRAGMA freelist_count`).Scan(&free); err != nil {
		return false, err
	}
	if pages == 0 || float64(free)/float64(pages) < vacuumFreeRatio {
		return false, nil
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		return false, err
	}
	// In WAL mode the rewrite lands in the WAL, so the main file doesn't
	// shrink until it's checkpointed.
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return true, err
	}
	return true, nil
}

func saveRetentionState(db *sql.DB, now time.Time, deleted int64) error {
	for key, value := range map[string]int64{
		retentionLastRunKey:     now.UnixMilli(),
		retentionLastDeletedKey: deleted,
	} {
		if _, err := db.Exec(`
			INSERT INTO ui_state (key, value) VALUES (?, CAST(? AS TEXT))
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return fmt.Errorf("save retention state: %w", err)
		}
	}
	return nil
}

func retentionState(db *sql.DB, key string) (int64, bool) {
	var value string
	if err := db.QueryRow(`SELECT value FROM ui_state WHERE key = ?`, key).Scan(&value); err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(value, 10, 64)
	return n, err == nil
}

// retentionStatus describes the setting and the last cleanup in one line.
func retentionStatus(db *sql.DB, months int) string {
	if months <= 0 {
		return "off — keeping everything"
	}
	last, ok := retentionState(db, retentionLastRunKey)
	if !ok {
		return fmt.Sprintf("%d months, no cleanup yet", months)
	}
	deleted, _ := retentionState(db, retentionLastDeletedKey)
	return fmt.Sprintf("%d months, last cleanup %s removed %d items",
		months, time.UnixMilli(last).Local().Format(time.RFC3339), deleted)
}
