package ingest

import (
	"context"
	"database/sql"
	"fmt"
)

// Writer buffers decoded log records (RawEvents) and flushes them to
// events_raw in batches, projecting each into its typed table in the same
// transaction as the raw insert (two-phase write, per spec: an unknown or
// not-yet-arrived attribute must degrade to a NULL column, never to data
// loss or a failed write).
type Writer struct {
	bw *batchWriter[RawEvent]
}

// NewWriter constructs a Writer over db. Call Run in its own goroutine to
// start flushing.
func NewWriter(db *sql.DB) *Writer {
	return &Writer{bw: newBatchWriter(db, "logs", writeLogsBatch, describeRawEvent)}
}

// Enqueue submits e for writing. Non-blocking.
func (w *Writer) Enqueue(e RawEvent) { w.bw.Enqueue(e) }

// Run drains the buffer into events_raw, batching up to a fixed size or
// flush interval, whichever comes first. It blocks until ctx is cancelled,
// then drains and flushes whatever remains before returning.
func (w *Writer) Run(ctx context.Context) error { return w.bw.Run(ctx) }

func describeRawEvent(e RawEvent) string {
	return fmt.Sprintf("%q (session=%s prompt=%s)", e.EventName, e.SessionID, e.PromptID)
}

func writeLogsBatch(db *sql.DB, batch []RawEvent) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO events_raw (ts, session_id, prompt_id, event_name, seq, body)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, e := range batch {
		if _, err := stmt.Exec(
			e.Ts,
			nullableString(e.SessionID),
			nullableString(e.PromptID),
			nullableString(e.EventName),
			e.Seq,
			e.Body,
		); err != nil {
			return err
		}
		if err := upsertSession(tx, e.SessionID, e.Ts, e.Attrs, e.ResourceAttrs); err != nil {
			return fmt.Errorf("upsert session for %q: %w", e.EventName, err)
		}
		if err := projectLogEvent(tx, e); err != nil {
			return fmt.Errorf("project %q: %w", e.EventName, err)
		}
	}
	return tx.Commit()
}
