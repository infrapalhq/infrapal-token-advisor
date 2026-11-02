package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// RunImports reads every agent that keeps its data locally, now and then every
// interval, until ctx is cancelled. A failed import is logged and retried from
// the same place next time.
func RunImports(ctx context.Context, db *sql.DB, agents []agent.Agent, interval time.Duration) {
	importers := map[agent.ID]agent.Importer{}
	for _, a := range agents {
		if imp, ok := a.(agent.Importer); ok {
			importers[a.ID()] = imp
		}
	}
	if len(importers) == 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		for id, imp := range importers {
			if err := runImport(ctx, db, id, imp); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("ingest: import failed", "agent", string(id), "err", err)
				recordIngestError(db, "import", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runImport takes batches until the agent has nothing more to give.
func runImport(ctx context.Context, db *sql.DB, id agent.ID, imp agent.Importer) error {
	key := "import:" + string(id)
	var cursor string
	err := db.QueryRowContext(ctx, `SELECT value FROM ui_state WHERE key = ?`, key).Scan(&cursor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("read import cursor: %w", err)
	}

	for {
		batch, err := imp.Import(ctx, cursor)
		if err != nil {
			return err
		}
		if batch.Cursor == "" {
			batch.Cursor = cursor // nothing new, so stay where we are
		}
		if err := saveImportBatch(ctx, db, id, key, batch); err != nil {
			return err
		}
		if !batch.More {
			return nil
		}
		if batch.Cursor == cursor {
			return fmt.Errorf("importer for %s asked to continue without moving its cursor", id)
		}
		cursor = batch.Cursor
	}
}

// saveImportBatch stores a batch's events and cursor in one transaction, so
// either both land or the batch is simply read again.
func saveImportBatch(ctx context.Context, db *sql.DB, id agent.ID, key string, batch agent.ImportBatch) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, ev := range batch.Events {
		if err := writeAgentEvent(tx, id, ev); err != nil {
			return fmt.Errorf("write imported event: %w", err)
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO ui_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, batch.Cursor); err != nil {
		return fmt.Errorf("save import cursor: %w", err)
	}
	return tx.Commit()
}
