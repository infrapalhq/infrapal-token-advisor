package ingest

import (
	"context"
	"database/sql"
	"log/slog"
	"time"
)

// recordIngestError persists a batch-write failure to ingest_errors so
// `token-advisor doctor` (a fresh process with no view of past log lines) can
// report ingest errors in the last hour. Best-effort: if the DB itself is
// the problem, this insert can fail too — that failure only reaches the
// log file, not a second table row, to avoid recursing.
func recordIngestError(db *sql.DB, signal string, cause error) {
	if _, err := db.Exec(
		`INSERT INTO ingest_errors (ts, signal, message) VALUES (?, ?, ?)`,
		time.Now().UnixMilli(), signal, cause.Error(),
	); err != nil {
		slog.Error("ingest: failed to record ingest error", "signal", signal, "err", err)
	}
}

const (
	defaultBufferSize    = 4096
	defaultBatchSize     = 200
	defaultFlushInterval = time.Second
)

// batchWriter buffers items of type T submitted via Enqueue and flushes
// them to the database in batches on a background goroutine started by
// Run. Enqueue never blocks: if the buffer is full (the DB has fallen far
// behind, which shouldn't happen for a single local user) the item is
// dropped and logged rather than stalling the caller — typically an HTTP
// handler decoding a live OTLP export.
//
// Logs, metrics, and spans each wrap a batchWriter with their own
// signal-specific write function rather than duplicating the buffering,
// ticking, and shutdown-drain logic three times.
type batchWriter[T any] struct {
	db            *sql.DB
	signal        string
	ch            chan T
	batchSize     int
	flushInterval time.Duration
	write         func(*sql.DB, []T) error
	describe      func(T) string
}

// newBatchWriter constructs a batchWriter over db. signal labels this
// writer's rows in ingest_errors ("logs", "metrics", "spans"). write is
// invoked with each flushed batch; describe renders a single dropped item
// for the buffer-full log line. Call Run in its own goroutine to start
// flushing.
func newBatchWriter[T any](db *sql.DB, signal string, write func(*sql.DB, []T) error, describe func(T) string) *batchWriter[T] {
	return &batchWriter[T]{
		db:            db,
		signal:        signal,
		ch:            make(chan T, defaultBufferSize),
		batchSize:     defaultBatchSize,
		flushInterval: defaultFlushInterval,
		write:         write,
		describe:      describe,
	}
}

// Enqueue submits item for writing. Non-blocking.
func (w *batchWriter[T]) Enqueue(item T) {
	select {
	case w.ch <- item:
	default:
		slog.Warn("ingest: buffer full, dropping item — DB is falling behind", "item", w.describe(item))
	}
}

// Run drains the buffer, batching up to batchSize items or flushInterval,
// whichever comes first. It blocks until ctx is cancelled, then drains and
// flushes whatever remains before returning.
func (w *batchWriter[T]) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]T, 0, w.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if err := w.write(w.db, batch); err != nil {
			slog.Error("ingest: write batch failed", "signal", w.signal, "items", len(batch), "err", err)
			recordIngestError(w.db, w.signal, err)
		}
		batch = batch[:0]
	}

	for {
		select {
		case item := <-w.ch:
			batch = append(batch, item)
			if len(batch) >= w.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			// Drain whatever is already queued before giving up.
			for {
				select {
				case item := <-w.ch:
					batch = append(batch, item)
				default:
					flush()
					return nil
				}
			}
		}
	}
}
