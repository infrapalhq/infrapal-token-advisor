// Package claudecode is the Claude Code adapter. Claude Code reads its
// telemetry settings from the env block of ~/.claude/settings.json: exported
// shell vars silently lose to whatever's already there, so that file is what
// Connect patches.
package claudecode

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// ID is how Claude Code is named in config and stored data.
const ID agent.ID = "claude-code"

// Agent is the Claude Code adapter.
type Agent struct {
	settingsPath string
}

// New returns the adapter for the user with this home directory.
func New(home string) *Agent {
	return &Agent{settingsPath: filepath.Join(home, ".claude", "settings.json")}
}

// ID implements agent.Agent.
func (a *Agent) ID() agent.ID { return ID }

// Name implements agent.Agent.
func (a *Agent) Name() string { return "Claude Code" }

// Capabilities implements agent.Agent.
func (a *Agent) Capabilities() agent.Capability {
	return agent.CapFilePaths | agent.CapCommands | agent.CapApprovalWait | agent.CapCompaction |
		agent.CapContextSize | agent.CapPromptText | agent.CapMCPConnections
}

// CostSource implements agent.Agent.
func (a *Agent) CostSource() agent.CostSource { return agent.CostPerRequest }

// toolCategories maps Claude Code's built-in tools.
var toolCategories = map[string]agent.ToolCategory{
	"Read":         agent.ToolRead,
	"Grep":         agent.ToolSearch,
	"Glob":         agent.ToolSearch,
	"LS":           agent.ToolSearch,
	"Edit":         agent.ToolEdit,
	"Write":        agent.ToolEdit,
	"MultiEdit":    agent.ToolEdit,
	"NotebookEdit": agent.ToolEdit,
	"Bash":         agent.ToolShell,
}

// ToolCategory implements agent.Agent.
func (a *Agent) ToolCategory(name string) agent.ToolCategory {
	if strings.HasPrefix(name, "mcp__") {
		return agent.ToolMCP
	}
	if c, ok := toolCategories[name]; ok {
		return c
	}
	return agent.ToolOther
}

// TelemetryEnv is the env block that makes Claude Code report to the collector.
// Values are all strings, as settings.json expects.
func TelemetryEnv(otlpPort int) map[string]string {
	return map[string]string{
		"CLAUDE_CODE_ENABLE_TELEMETRY":        "1",
		"OTEL_METRICS_EXPORTER":               "otlp",
		"OTEL_LOGS_EXPORTER":                  "otlp",
		"OTEL_TRACES_EXPORTER":                "otlp",
		"CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
		"OTEL_EXPORTER_OTLP_PROTOCOL":         "http/protobuf",
		"OTEL_EXPORTER_OTLP_ENDPOINT":         fmt.Sprintf("http://localhost:%d", otlpPort),
		"OTEL_METRIC_EXPORT_INTERVAL":         "10000",
		"OTEL_LOGS_EXPORT_INTERVAL":           "2000",
		"OTEL_TRACES_EXPORT_INTERVAL":         "2000",
		"OTEL_METRICS_INCLUDE_VERSION":        "true",
		"OTEL_METRICS_INCLUDE_ENTRYPOINT":     "true",
		"OTEL_LOG_USER_PROMPTS":               "1",
		"OTEL_LOG_ASSISTANT_RESPONSES":        "1",
		"OTEL_LOG_TOOL_DETAILS":               "1",
		"OTEL_LOG_TOOL_CONTENT":               "1",
	}
}

// Connect implements agent.Agent by merging the telemetry env into settings.json.
func (a *Agent) Connect(w io.Writer, opts agent.ConnectOptions) (agent.Connection, error) {
	conn := opts.Previous
	doc, _, err := Load(a.settingsPath)
	if err != nil {
		return conn, err
	}

	wanted := TelemetryEnv(opts.OTLPPort)
	if opts.RawBodiesDir != "" {
		wanted["OTEL_LOG_RAW_API_BODIES"] = "file:" + opts.RawBodiesDir
		fmt.Fprintln(w, "warning: --raw-bodies captures the entire conversation history and system prompt on every API request, written unredacted to disk under", opts.RawBodiesDir)
	}

	changes, err := MergeEnv(doc, wanted)
	if err != nil {
		return conn, fmt.Errorf("merge settings.json env block: %w", err)
	}

	// Leave the file alone when there's nothing to change: rewriting it would
	// reorder the user's keys, and a backup would only be another copy of it.
	if len(changes) == 0 {
		fmt.Fprintln(w, "settings.json already had everything token-advisor wants — nothing changed")
	} else {
		backupPath, err := Backup(a.settingsPath, time.Now())
		if err != nil {
			return conn, fmt.Errorf("back up %s: %w", a.settingsPath, err)
		}
		if backupPath != "" {
			fmt.Fprintf(w, "backed up %s to %s\n", a.settingsPath, backupPath)
			// Keep the first backup across repeat inits: a later one already
			// holds our changes, so uninstall couldn't restore from it.
			if conn.BackupPath == "" {
				conn.BackupPath = backupPath
			}
		}
		if err := Save(a.settingsPath, doc); err != nil {
			return conn, fmt.Errorf("write %s: %w", a.settingsPath, err)
		}

		fmt.Fprintln(w, "changed in", a.settingsPath+":")
		for _, c := range changes {
			if !c.Existed {
				fmt.Fprintf(w, "  %s: added (%q)\n", c.Key, c.NewValue)
			} else {
				fmt.Fprintf(w, "  %s: %v -> %q\n", c.Key, c.OldValue, c.NewValue)
			}
		}
	}
	fmt.Fprintln(w, "reminder: already-running Claude Code sessions won't pick up the new env — restart them.")
	return conn, nil
}

// Disconnect implements agent.Agent by restoring settings.json from its backup.
func (a *Agent) Disconnect(w io.Writer, conn agent.Connection) error {
	if conn.BackupPath == "" {
		fmt.Fprintln(w, "no settings.json backup recorded, so init never changed an existing file — settings.json left as-is")
		return nil
	}
	if _, err := os.Stat(conn.BackupPath); err != nil {
		fmt.Fprintf(w, "settings.json backup %s is missing — settings.json left as-is\n", conn.BackupPath)
		return nil //nolint:nilerr // nothing to restore isn't a failure
	}
	if err := Restore(a.settingsPath, conn.BackupPath); err != nil {
		return fmt.Errorf("restore %s: %w", a.settingsPath, err)
	}
	fmt.Fprintf(w, "restored %s from %s\n", a.settingsPath, conn.BackupPath)
	return nil
}

// managedSettingsPath returns the platform's enterprise managed-settings.json
// location, or "" on a platform with no known path (Windows managed settings
// can also arrive via registry rather than a file — not checkable here).
// Confirmed against https://code.claude.com/docs/en/settings.
func managedSettingsPath(goos string) string {
	switch goos {
	case "darwin":
		return "/Library/Application Support/ClaudeCode/managed-settings.json"
	case "linux":
		return "/etc/claude-code/managed-settings.json"
	default:
		return ""
	}
}

// Check implements agent.Agent.
func (a *Agent) Check(otlpPort int) ([]agent.Check, error) {
	settings, err := a.checkSettings(otlpPort)
	if err != nil {
		return nil, err
	}
	return []agent.Check{settings, checkShellEnv(os.Environ()), checkManagedSettings(runtime.GOOS)}, nil
}

// checkSettings looks for our env block, with the endpoint matching our port.
func (a *Agent) checkSettings(otlpPort int) (agent.Check, error) {
	doc, existed, err := Load(a.settingsPath)
	if err != nil {
		return agent.Check{}, err
	}
	problem := settingsProblem(doc, existed, a.settingsPath, otlpPort)
	return agent.Check{
		Label: fmt.Sprintf("%s has token-advisor's env block", a.settingsPath),
		Pass:  problem == "",
		Fix:   fmt.Sprintf("%s — run `token-advisor init` to fix", problem),
	}, nil
}

// settingsProblem says what's wrong with the env block, or "" if nothing is.
func settingsProblem(doc Doc, existed bool, path string, otlpPort int) string {
	if !existed {
		return fmt.Sprintf("%s does not exist", path)
	}
	env, err := doc.EnvBlock()
	if err != nil {
		return err.Error()
	}
	wantEndpoint := fmt.Sprintf("http://localhost:%d", otlpPort)
	if endpoint, _ := env["OTEL_EXPORTER_OTLP_ENDPOINT"].(string); endpoint != wantEndpoint {
		return fmt.Sprintf("OTEL_EXPORTER_OTLP_ENDPOINT is %q, want %q", endpoint, wantEndpoint)
	}
	var missing []string
	for _, key := range []string{
		"CLAUDE_CODE_ENABLE_TELEMETRY", "OTEL_LOGS_EXPORTER", "OTEL_TRACES_EXPORTER",
		"OTEL_METRICS_EXPORTER", "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA",
	} {
		if _, present := env[key]; !present {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return fmt.Sprintf("missing keys: %s", strings.Join(missing, ", "))
	}
	return ""
}

// checkShellEnv looks for conflicting vars in doctor's own shell env.
func checkShellEnv(environ []string) agent.Check {
	var conflicting []string
	for _, kv := range environ {
		key, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(key, "OTEL_") || strings.HasPrefix(key, "CLAUDE_CODE_") {
			conflicting = append(conflicting, key)
		}
	}
	check := agent.Check{Label: "no OTEL_*/CLAUDE_CODE_* vars exported in this shell", Pass: len(conflicting) == 0}
	if !check.Pass {
		check.Fix = fmt.Sprintf("this shell exports: %s — these can override settings.json for any `claude` launched from here; unset them or reconcile with `token-advisor init`'s values. Note: this only reflects the shell doctor was invoked from, not every shell you might launch `claude` from.", strings.Join(conflicting, ", "))
	}
	return check
}

// checkManagedSettings looks for a managed settings file, which strips
// developer-set OTLP endpoints.
func checkManagedSettings(goos string) agent.Check {
	mp := managedSettingsPath(goos)
	if mp == "" {
		return agent.Check{Label: fmt.Sprintf("managed settings check — no known path for GOOS=%s", goos), Skipped: true}
	}
	check := agent.Check{Label: fmt.Sprintf("no managed settings file at %s", mp)}
	_, err := os.Stat(mp)
	check.Pass = err != nil
	if !check.Pass {
		check.Fix = "a managed-settings.json is present and can strip developer-set OTLP endpoint/protocol at startup — check it with `claude --debug` and coordinate with whoever manages it if telemetry isn't reaching token-advisor"
	}
	return check
}
