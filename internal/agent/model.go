// Package agent defines what infrapal-token-advisor knows about a coding agent
// independently of which agent it is. Adapters translate an agent's telemetry
// into the events here, and everything downstream reads only this model.
//
// The model is experimental until a second real agent uses it. It was designed
// from research on other agents, and only Claude Code has run through it.
package agent

// ToolCategory says what a tool does, so a check can ask "was this a file
// read?" without knowing that Claude Code calls it Read.
type ToolCategory string

const (
	ToolRead   ToolCategory = "read"   // reads a file
	ToolSearch ToolCategory = "search" // finds files or text, changes nothing
	ToolEdit   ToolCategory = "edit"   // changes a file directly
	ToolShell  ToolCategory = "shell"  // runs a command
	ToolMCP    ToolCategory = "mcp"    // calls an MCP server
	ToolOther  ToolCategory = "other"  // anything else, including new tools
)

// CostSource says where an agent's dollar figures come from, because they
// aren't equally trustworthy and some agents report none.
type CostSource string

const (
	CostNone       CostSource = "none"        // no figure: show none, never zero
	CostPerRequest CostSource = "per_request" // the agent reports each request
	CostPerTurn    CostSource = "per_turn"    // the agent reports each prompt's total
	CostComputed   CostSource = "computed"    // worked out from token counts and prices
)

// Capability is data an agent can supply. A check names what it needs, and is
// skipped for an agent that can't provide it, rather than reporting "no
// problems" from data it never had.
type Capability uint

const (
	// CapFilePaths means tool events carry the file a tool touched.
	CapFilePaths Capability = 1 << iota
	// CapCommands means tool events carry the shell command that ran.
	CapCommands
	// CapApprovalWait means tool events carry how long the user took to approve.
	CapApprovalWait
	// CapCompaction means Compaction events are reported.
	CapCompaction
	// CapContextSize means each request's tokens show how full the context was.
	CapContextSize
	// CapPromptText means prompt and response text can be captured.
	CapPromptText
	// CapMCPConnections means MCPConnection events are reported.
	CapMCPConnections
)

// Has reports whether c includes everything in need.
func (c Capability) Has(need Capability) bool {
	return c&need == need
}

// Tokens is one request's token use. Agents disagree on whether their input
// count includes cached tokens, so here it never does: the four fields don't
// overlap, and adapters convert. See InputWithoutCached.
type Tokens struct {
	Input      int64 // fresh input, excluding anything cached
	Output     int64 // everything generated, including reasoning
	CacheRead  int64
	CacheWrite int64
}

// Total is every token the request used.
func (t Tokens) Total() int64 {
	return t.Input + t.Output + t.CacheRead + t.CacheWrite
}

// Context is what the model read for this request.
func (t Tokens) Context() int64 {
	return t.Input + t.CacheRead + t.CacheWrite
}

// InputWithoutCached converts an input count that includes cached tokens. It
// never goes below zero, so a bad count can't reduce a session's total.
func InputWithoutCached(input, cached int64) int64 {
	return max(input-cached, 0)
}

// Approval says how a tool call was allowed to run.
type Approval string

const (
	ApprovalUnknown  Approval = ""
	ApprovalAuto     Approval = "auto"     // allowed by config, without asking
	ApprovalUser     Approval = "user"     // the user approved it
	ApprovalRejected Approval = "rejected" // the user or a rule refused it
)

// Meta is what every event carries. PromptID is empty for an event outside a
// prompt. Ts is in Unix milliseconds.
type Meta struct {
	SessionID string
	PromptID  string
	Ts        int64
}

// Base returns the event's Meta. Embedding Meta is what makes a type an Event.
func (m Meta) Base() Meta {
	return m
}

// Event is one fact from an agent, in common form. An adapter returns nothing
// for a record with no event here: the raw record is still kept, so a new
// agent feature can't break ingest.
type Event interface {
	Base() Meta
}

// Session describes a session, whenever the agent reveals its details.
type Session struct {
	Meta
	ProjectDir string // empty if unknown
	Version    string // the agent's version, empty if unknown
}

// Prompt is one thing the user asked. Text is empty unless capture is on.
type Prompt struct {
	Meta
	Text   string
	Length int64
}

// Response is text the model answered with.
type Response struct {
	Meta
	MessageID string
	Text      string
}

// Request is one call to a model. CostMicros is in millionths of a dollar and
// nil when this request has no figure of its own, which isn't the same as free.
type Request struct {
	Meta
	RequestID  string
	Model      string
	Tokens     Tokens
	CostMicros *int64
	DurationMs int64
	Success    bool
	Error      string // empty unless the request failed
	// Background marks housekeeping such as generating a session title, which
	// shouldn't count as the model chosen for the user's work.
	Background bool
}

// TurnCost is a prompt's total cost, for an agent with no per-request figure.
type TurnCost struct {
	Meta
	Micros int64
}

// Tool is what's known so far about one tool call. Agents report a call in
// parts, such as a decision and later a result, so CallID ties them together
// and an empty or nil field means "not in this event", not "none".
type Tool struct {
	Meta
	CallID      string
	Name        string // the agent's own name, for display
	Category    ToolCategory
	MCPServer   string
	File        string
	Command     string
	Approval    Approval
	WaitMs      *int64 // time spent waiting for the user
	DurationMs  *int64
	Success     *bool
	ResultBytes *int64
}

// Compaction is the agent shrinking its context to keep going.
type Compaction struct {
	Meta
	TokensBefore int64
	TokensAfter  int64
}

// MCPConnection is a session's connection to an MCP server.
type MCPConnection struct {
	Meta
	Server string
	Status string
}
