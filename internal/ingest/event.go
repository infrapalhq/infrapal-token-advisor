// Package ingest buffers decoded telemetry records and writes them to
// SQLite in batches, so a slow disk or a burst of exports never makes the
// OTLP handler block the Claude Code process that's exporting to it.
package ingest

// RawEvent is one decoded OTLP log record, ready to land in events_raw and
// be projected into the typed tables. Body carries the full decoded record
// (resource + record attributes) as JSON — the catch-all so no field is
// ever lost even if it's not one of the columns below or in Attrs.
type RawEvent struct {
	Ts        int64 // unix millis
	SessionID string
	PromptID  string
	EventName string
	Seq       int64
	TraceID   string
	SpanID    string

	// Attrs is the merged resource+record attribute map (record wins on
	// key collision), for projection to read structured fields without
	// re-parsing Body's JSON. ResourceAttrs is the resource-only subset,
	// stored verbatim into sessions.resource_attrs.
	Attrs         map[string]any
	ResourceAttrs map[string]any

	Body string // JSON
}
