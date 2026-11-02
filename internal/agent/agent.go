package agent

import (
	"context"
	"io"
)

// ID names an agent in config, flags and stored data, such as "claude-code".
type ID string

// Agent is everything the rest of the program needs from one coding agent. To
// support a new agent, add a package that implements this and list it in
// internal/agent/all.
type Agent interface {
	ID() ID
	// Name is the agent's name for people, such as "Claude Code".
	Name() string
	// Capabilities says what data this agent can supply.
	Capabilities() Capability
	// CostSource says where this agent's dollar figures come from.
	CostSource() CostSource
	// ToolCategory says what a tool does, given the agent's own name for it.
	// A tool the adapter doesn't recognise is ToolOther.
	ToolCategory(name string) ToolCategory
	// PremiumModel reports whether a model is this agent's most expensive
	// tier, where small tasks are worth moving to something cheaper.
	PremiumModel(model string) bool
	// Connect sets the agent up to report to the collector, describing what
	// it did on w. Running it again must be safe.
	Connect(w io.Writer, opts ConnectOptions) (Connection, error)
	// Disconnect undoes Connect, describing what it did on w.
	Disconnect(w io.Writer, conn Connection) error
	// Check reports whether the agent is set up correctly, for `doctor`.
	Check(otlpPort int) ([]Check, error)
}

// ConnectOptions is what Connect needs to know.
type ConnectOptions struct {
	OTLPPort int
	// RawBodiesDir turns on capturing raw API bodies there, if the agent can.
	RawBodiesDir string
	// Previous is the Connection from an earlier Connect, if any.
	Previous Connection
}

// Connection is what Disconnect needs later, saved between runs.
type Connection struct {
	// BackupPath is a copy of the agent's settings from before the first
	// change, or empty if there is nothing to restore.
	BackupPath string
}

// Check is one line of `doctor` output.
type Check struct {
	Label   string
	Pass    bool
	Fix     string // what to do if it failed
	Skipped bool   // the check doesn't apply here, and Pass means nothing
}

// LogSource is for an agent that exports OTLP logs. Implement it, or Importer,
// or both, according to how the agent reports.
type LogSource interface {
	// OwnsService reports whether a record with this service.name resource
	// attribute came from this agent. A record no adapter owns is treated as
	// Claude Code's.
	OwnsService(service string) bool
	// MapLog turns one record into events. Return nothing for a record you
	// don't recognise: it has already been kept raw. Each event must set its
	// Meta, because agents name their session and prompt ids differently.
	MapLog(rec LogRecord) []Event
}

// LogRecord is one OTLP log record.
type LogRecord struct {
	Name  string         // the event.name attribute
	Ts    int64          // Unix milliseconds
	Attrs map[string]any // resource and record attributes, with the record's winning
}

// Importer is for an agent that keeps its data locally. It's called every so
// often and should return promptly with a bounded batch, however large the
// source is.
type Importer interface {
	// Import returns the events after cursor, which is empty the first time
	// and otherwise a value this method returned. It's opaque to the caller,
	// so a file offset or a timestamp both work. Leaving Cursor empty in the
	// result keeps the current position.
	Import(ctx context.Context, cursor string) (ImportBatch, error)
}

// ImportBatch is one step of an import. Its events and cursor are saved
// together, so a crash can't lose or repeat a batch.
type ImportBatch struct {
	Events []Event
	Cursor string
	More   bool // there's more to read now, so call again straight away
}
