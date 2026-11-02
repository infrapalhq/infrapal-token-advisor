package agent

// Fix is something a user can paste in to stop a problem recurring.
type Fix struct {
	Text  string
	Lang  string // how to highlight Text
	Where string // where it goes, for people, such as "paste into CLAUDE.md"
}

// Advisor writes fixes in an agent's own terms. It's optional: an agent without
// one still gets every finding, just with no fix to paste. Embed NoAdvice to
// write only the advice you have.
type Advisor interface {
	// ReReadFix stops the same file being read again with nothing changed.
	ReReadFix() Fix
	// RepeatCommandFix stops a command running again with nothing changed.
	// editedExt is the session's most edited file type, if there is one.
	RepeatCommandFix(command, editedExt string) Fix
	// ShellPattern writes a read-only command prefix as a permission pattern.
	ShellPattern(prefix string) string
	// AllowFix permanently allows a pattern from ShellPattern, or a tool name.
	AllowFix(pattern string) Fix
	// RemoveMCPFix removes an MCP server that's never used.
	RemoveMCPFix(server string) Fix
	// CompactionDetail explains a compaction.
	CompactionDetail() string
	// SmallerModelNote suggests a cheaper model for a small turn.
	SmallerModelNote() string
}

// NoAdvice is the Advisor for an agent with nothing specific to say.
type NoAdvice struct{}

func (NoAdvice) ReReadFix() Fix                    { return Fix{} }
func (NoAdvice) RepeatCommandFix(_, _ string) Fix  { return Fix{} }
func (NoAdvice) ShellPattern(prefix string) string { return prefix }
func (NoAdvice) AllowFix(string) Fix               { return Fix{} }
func (NoAdvice) RemoveMCPFix(string) Fix           { return Fix{} }

func (NoAdvice) CompactionDetail() string {
	return "The agent had to summarise the conversation to keep going. A fresh session per task avoids this."
}

func (NoAdvice) SmallerModelNote() string {
	return "Habit: use a smaller model for turns like this. Spend shown is what this turn cost, not a saving."
}

// MCPSetup is how a user connects the token-advisor MCP server to an agent.
type MCPSetup struct {
	Enabled bool
	Command string // connects it
	Verify  string // shows that it worked
	Remove  string // disconnects it
}

// MCPHost is for an agent that can use MCP servers.
type MCPHost interface {
	// MCPSetup describes connecting the server of this name, run by binary.
	MCPSetup(server, binary string) MCPSetup
}
