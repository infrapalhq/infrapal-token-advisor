package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/infrapalhq/infrapal-token-advisor/internal/paths"
)

// MCPServerName is the name the MCP server is registered under in Claude Code.
const MCPServerName = "infrapal-token-advisor"

// handleMCPSetup handles GET /api/mcp/setup: the commands to paste into a
// terminal to enable the MCP server, and whether it is already registered.
func (s *Server) handleMCPSetup(w http.ResponseWriter, r *http.Request) {
	p, err := paths.Resolve()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	binary, err := os.Executable()
	if err != nil {
		binary = MCPServerName
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"server_name": MCPServerName,
		"enabled":     mcpRegistered(p.ClaudeConfigPath),
		"command":     "claude mcp add --scope user " + MCPServerName + " -- " + shellQuote(binary) + " mcp",
		"verify":      "claude mcp list",
		"remove":      "claude mcp remove --scope user " + MCPServerName,
	})
}

// mcpRegistered reports whether Claude Code's user config lists the server. A
// missing or unreadable file just means "not registered".
func mcpRegistered(configPath string) bool {
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
	_, ok := cfg.MCPServers[MCPServerName]
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

// safeFindingTitles lists every kind whose text may go to the model. An empty
// title means the item's own title is safe. Otherwise the replacement is used,
// because the item's title names a file, command, or project. Kinds not listed
// here keep only their numbers, so a new detector leaks nothing by default.
var safeFindingTitles = map[string]string{
	"compaction":      "",
	"context_bloat":   "",
	"dead_mcp":        "",
	"failing_mcp":     "",
	"many_tool_calls": "",
	"output_spike":    "",
	"oversized_model": "",
	"rejected_work":   "",
	"tool_failures":   "",
	"project_notes":   "Files read across several sessions could be covered by project notes",
	"re_read":         "A file was re-read with no change between reads",
	"repeat_approval": "A tool pattern was approved by hand repeatedly",
	"repeat_cmd":      "A shell command ran again with nothing changed",
}

// SafeFinding is an insightItem with everything that could hold a file path,
// command, or project name removed. ID stands in for the subject, so findings
// about different files stay distinct without naming them.
type SafeFinding struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Area        string   `json:"area"`
	Title       string   `json:"title"`
	Detail      string   `json:"detail,omitempty"`
	Note        string   `json:"note,omitempty"`
	Severity    string   `json:"severity"`
	Confidence  string   `json:"confidence"`
	Occurrences int      `json:"occurrences"`
	Sessions    int      `json:"sessions"`
	Tokens      *int64   `json:"tokens,omitempty"`
	USD         *float64 `json:"avoidable_usd,omitempty"`
	Minutes     *float64 `json:"minutes,omitempty"`
	HasFix      bool     `json:"has_fix"`
	MarkedFixed bool     `json:"marked_fixed"`
	LastTs      int64    `json:"last_ts"`
}

// SafeFindings returns the same items as the Fixes page, highest priority
// first, for a range token such as "30d" or "all".
func SafeFindings(ctx context.Context, db *sql.DB, since string) ([]SafeFinding, error) {
	cutoff, err := durationSince(ctx, db, since)
	if err != nil {
		return nil, err
	}
	d, occ, err := computeInsights(ctx, db, cutoff)
	if err != nil {
		return nil, err
	}
	marks, err := loadMarks(ctx, db)
	if err != nil {
		return nil, err
	}
	items := aggregateItems(occ, d.blendedRate)
	applyMarks(items, marks)

	out := make([]SafeFinding, 0, len(items))
	for _, it := range items {
		sum := sha256.Sum256([]byte(it.Key))
		f := SafeFinding{
			ID:          hex.EncodeToString(sum[:6]),
			Kind:        it.Kind,
			Area:        it.Area,
			Title:       it.Kind,
			Severity:    it.Severity,
			Confidence:  it.Conf,
			Occurrences: it.Occurrences,
			Sessions:    it.Sessions,
			Tokens:      it.Tokens,
			USD:         it.USD,
			Minutes:     it.Minutes,
			HasFix:      it.Fix != "",
			MarkedFixed: it.Marked,
			LastTs:      it.LastTs,
		}
		if title, known := safeFindingTitles[it.Kind]; known {
			f.Title, f.Detail, f.Note = title, it.Detail, it.Note
			if title == "" {
				f.Title = it.Title
			}
		}
		out = append(out, f)
	}
	return out, nil
}
