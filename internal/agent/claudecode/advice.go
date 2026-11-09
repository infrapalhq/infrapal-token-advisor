package claudecode

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

const (
	whereClaudeMD = "paste into CLAUDE.md"
	whereSettings = "add to settings.json"
	whereTerminal = "run in your terminal"
)

// ReReadFix implements agent.Advisor.
func (a *Agent) ReReadFix() agent.Fix {
	return agent.Fix{
		Text:  "## Working with files\n- Grep before reading large files.\n- Don't re-read a file already in context.",
		Lang:  "claude-md",
		Where: whereClaudeMD,
	}
}

// RepeatCommandFix implements agent.Advisor.
func (a *Agent) RepeatCommandFix(command, editedExt string) agent.Fix {
	text := fmt.Sprintf("Add to CLAUDE.md:\n\n- Don't re-run `%s` unless something it depends on has changed.", command)
	if editedExt != "" {
		text = fmt.Sprintf("Add to CLAUDE.md:\n\n- Only re-run `%s` after editing .%s files.", command, editedExt)
	}
	return agent.Fix{Text: text, Lang: "claude-md", Where: whereClaudeMD}
}

// ShellPattern implements agent.Advisor.
func (a *Agent) ShellPattern(prefix string) string {
	return fmt.Sprintf("Bash(%s:*)", prefix)
}

// AllowFix implements agent.Advisor with a settings.json permissions.allow entry.
func (a *Agent) AllowFix(pattern string) agent.Fix {
	return agent.Fix{
		Text:  fmt.Sprintf("{\n  \"permissions\": {\n    \"allow\": [\n      %q\n    ]\n  }\n}", pattern),
		Lang:  "settings-json",
		Where: whereSettings,
	}
}

// RemoveMCPFix implements agent.Advisor.
func (a *Agent) RemoveMCPFix(server string) agent.Fix {
	return agent.Fix{Text: fmt.Sprintf("claude mcp remove %q", server), Lang: "bash", Where: whereTerminal}
}

// CompactionDetail implements agent.Advisor.
func (a *Agent) CompactionDetail() string {
	return "Claude had to summarise the conversation to keep going. A fresh session per task avoids this."
}

// SmallerModelNote implements agent.Advisor.
func (a *Agent) SmallerModelNote() string {
	return "Habit: switch to a smaller model with /model for turns like this. The saving assumes the session was already on that model, with its cache warm."
}

// MCPSetup implements agent.MCPHost.
func (a *Agent) MCPSetup(server, binary string) agent.MCPSetup {
	return agent.MCPSetup{
		Enabled: mcpRegistered(a.configPath, server),
		Command: "claude mcp add --scope user " + server + " -- " + shellQuote(binary) + " mcp",
		Verify:  "claude mcp list",
		Remove:  "claude mcp remove --scope user " + server,
	}
}

// mcpRegistered reports whether Claude Code's user config lists the server. A
// missing or unreadable file just means "not registered".
func mcpRegistered(configPath, server string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	var cfg struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false
	}
	_, ok := cfg.MCPServers[server]
	return ok
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote makes s a single POSIX shell word.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
