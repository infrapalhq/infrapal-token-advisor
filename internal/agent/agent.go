package agent

import "io"

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
