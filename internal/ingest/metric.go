package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// MetricPoint is one decoded OTLP data point, ready to land in
// metric_points. Per the schema's gotcha #1, Claude Code's metrics are
// always delta temporality: points are increments, not running totals.
// Per gotcha #2, prompt.id is deliberately never on metrics — metric<->event
// joins only work at session.id granularity.
type MetricPoint struct {
	Ts        int64 // unix millis
	Name      string
	Value     float64
	SessionID string
	Model     string

	Attrs         map[string]any // merged resource+metadata+datapoint attributes
	ResourceAttrs map[string]any
}

// MetricsWriter buffers decoded metric data points and flushes them to
// metric_points in batches.
type MetricsWriter struct {
	bw *batchWriter[MetricPoint]
}

// NewMetricsWriter constructs a MetricsWriter over db. Call Run in its own
// goroutine to start flushing.
func NewMetricsWriter(db *sql.DB) *MetricsWriter {
	return &MetricsWriter{bw: newBatchWriter(db, "metrics", writeMetricsBatch, describeMetricPoint)}
}

func (w *MetricsWriter) Enqueue(p MetricPoint) { w.bw.Enqueue(p) }

func (w *MetricsWriter) Run(ctx context.Context) error { return w.bw.Run(ctx) }

func describeMetricPoint(p MetricPoint) string {
	return fmt.Sprintf("%q (session=%s)", p.Name, p.SessionID)
}

func writeMetricsBatch(db *sql.DB, batch []MetricPoint) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO metric_points (ts, name, value, session_id, model, attrs)
		VALUES (?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range batch {
		attrsJSON, err := json.Marshal(p.Attrs)
		if err != nil {
			attrsJSON = []byte("{}")
		}
		if _, err := stmt.Exec(p.Ts, p.Name, p.Value, nullableString(p.SessionID), nullableString(p.Model), string(attrsJSON)); err != nil {
			return err
		}
		if err := upsertSession(tx, p.SessionID, p.Ts, p.Attrs, p.ResourceAttrs); err != nil {
			return fmt.Errorf("upsert session for metric %q: %w", p.Name, err)
		}
	}
	return tx.Commit()
}
