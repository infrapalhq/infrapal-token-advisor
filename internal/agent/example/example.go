// Package example is a complete adapter for an imaginary agent, for copying
// when you add a real one. It isn't in internal/agent/all, so it never ships.
//
// The imaginary agent does both things an agent can do. It exports OTLP logs,
// which MapLog turns into events, and it keeps a local JSONL file, which Import
// reads. A real agent usually needs only one of them: delete the other, and
// the code will tell you nothing else depended on it.
package example

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// ID is how this agent is named in config and stored data.
const ID agent.ID = "example-agent"

// serviceName is the service.name attribute on this agent's OTLP records.
const serviceName = "example-cli"

// maxImportLines bounds one Import, however large the file is.
const maxImportLines = 500

// Agent is the adapter. Embedding NoAdvice gives every Advisor method a
// sensible default, so an adapter only overrides the advice it has.
type Agent struct {
	agent.NoAdvice
	configPath string
	logPath    string
}

// New returns the adapter for the user with this home directory.
func New(home string) *Agent {
	dir := filepath.Join(home, ".example-agent")
	return &Agent{
		configPath: filepath.Join(dir, "config.json"),
		logPath:    filepath.Join(dir, "sessions.jsonl"),
	}
}

// ID implements agent.Agent.
func (a *Agent) ID() agent.ID { return ID }

// Name implements agent.Agent.
func (a *Agent) Name() string { return "Example Agent" }

// Capabilities implements agent.Agent. List only what the agent really
// reports: a check needing anything else is skipped, not run on missing data.
func (a *Agent) Capabilities() agent.Capability {
	return agent.CapFilePaths | agent.CapCommands | agent.CapCompaction
}

// CostSource implements agent.Agent. This agent reports tokens but no dollars.
func (a *Agent) CostSource() agent.CostSource { return agent.CostNone }

// ToolCategory implements agent.Agent.
func (a *Agent) ToolCategory(name string) agent.ToolCategory {
	switch {
	case name == "open_file":
		return agent.ToolRead
	case name == "find_text":
		return agent.ToolSearch
	case name == "write_file":
		return agent.ToolEdit
	case name == "run_command":
		return agent.ToolShell
	case strings.HasPrefix(name, "mcp."):
		return agent.ToolMCP
	default:
		return agent.ToolOther
	}
}

// PremiumModel implements agent.Agent.
func (a *Agent) PremiumModel(model string) bool {
	return strings.HasSuffix(model, "-max")
}

// RepeatCommandFix overrides NoAdvice with advice in this agent's own terms.
func (a *Agent) RepeatCommandFix(command, _ string) agent.Fix {
	return agent.Fix{
		Text:  fmt.Sprintf("Add to .example-rules:\n\nnever-repeat: %s", command),
		Lang:  "text",
		Where: "add to .example-rules",
	}
}

// Connect implements agent.Agent. It must be safe to run again, and must keep
// the settings the user already had.
func (a *Agent) Connect(w io.Writer, opts agent.ConnectOptions) (agent.Connection, error) {
	conn := opts.Previous
	want, err := json.Marshal(map[string]string{"telemetry_endpoint": fmt.Sprintf("http://localhost:%d", opts.OTLPPort)})
	if err != nil {
		return conn, err
	}
	current, err := os.ReadFile(a.configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return conn, err
	}
	if bytes.Equal(current, want) {
		fmt.Fprintln(w, "Example Agent is already connected")
		return conn, nil
	}
	if err := os.MkdirAll(filepath.Dir(a.configPath), 0o700); err != nil {
		return conn, err
	}
	if current != nil && conn.BackupPath == "" {
		conn.BackupPath = a.configPath + ".backup"
		if err := os.WriteFile(conn.BackupPath, current, 0o600); err != nil {
			return conn, err
		}
	}
	if err := os.WriteFile(a.configPath, want, 0o600); err != nil {
		return conn, err
	}
	fmt.Fprintf(w, "connected Example Agent in %s\n", a.configPath)
	return conn, nil
}

// Disconnect implements agent.Agent by undoing exactly what Connect did.
func (a *Agent) Disconnect(w io.Writer, conn agent.Connection) error {
	if conn.BackupPath != "" {
		backup, err := os.ReadFile(conn.BackupPath)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "restored %s\n", a.configPath)
		return os.WriteFile(a.configPath, backup, 0o600)
	}
	if err := os.Remove(a.configPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Fprintf(w, "removed %s\n", a.configPath)
	return nil
}

// Check implements agent.Agent.
func (a *Agent) Check(otlpPort int) ([]agent.Check, error) {
	want := fmt.Sprintf("http://localhost:%d", otlpPort)
	data, err := os.ReadFile(a.configPath)
	return []agent.Check{{
		Label: "Example Agent reports to the collector",
		Pass:  err == nil && strings.Contains(string(data), want),
		Fix:   "run `token-advisor init --agent example-agent`",
	}}, nil
}

// OwnsService implements agent.LogSource.
func (a *Agent) OwnsService(service string) bool { return service == serviceName }

// MapLog implements agent.LogSource. Attrs come from outside the program, so
// never assume a key exists or has the right type, and return nothing for a
// record you don't recognise: new agent versions add new ones.
func (a *Agent) MapLog(rec agent.LogRecord) []agent.Event {
	session := str(rec.Attrs, "conversation.id")
	if session == "" {
		return nil
	}
	meta := agent.Meta{SessionID: session, PromptID: str(rec.Attrs, "turn.id"), Ts: rec.Ts}
	switch rec.Name {
	case "example.turn_started":
		return []agent.Event{
			agent.Session{Meta: meta, ProjectDir: str(rec.Attrs, "cwd"), Version: str(rec.Attrs, "app.version")},
			agent.Prompt{Meta: meta, Length: num(rec.Attrs, "prompt.length")},
		}
	case "example.model_call":
		input, cached := num(rec.Attrs, "tokens.input"), num(rec.Attrs, "tokens.cached")
		return []agent.Event{agent.Request{
			Meta:    meta,
			Model:   str(rec.Attrs, "model"),
			Success: true,
			// This agent counts cached tokens inside its input figure, and the
			// common model keeps them apart.
			Tokens: agent.Tokens{
				Input:     agent.InputWithoutCached(input, cached),
				Output:    num(rec.Attrs, "tokens.output"),
				CacheRead: cached,
			},
		}}
	case "example.tool":
		name := str(rec.Attrs, "tool")
		bytes := num(rec.Attrs, "output.bytes")
		return []agent.Event{agent.Tool{
			Meta:        meta,
			CallID:      str(rec.Attrs, "call.id"),
			Name:        name,
			Category:    a.ToolCategory(name),
			File:        str(rec.Attrs, "path"),
			Command:     str(rec.Attrs, "command"),
			ResultBytes: &bytes,
		}}
	default:
		return nil
	}
}

// str and num read an attribute, treating a missing or mistyped one as absent.
func str(attrs map[string]any, key string) string {
	s, _ := attrs[key].(string)
	return s
}

func num(attrs map[string]any, key string) int64 {
	switch v := attrs[key].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	default:
		return 0
	}
}

// compaction is one line of the agent's local file.
type compaction struct {
	Session string `json:"session"`
	Ts      int64  `json:"ts"`
	Before  int64  `json:"before"`
	After   int64  `json:"after"`
}

// Import implements agent.Importer. The cursor is a byte offset, so a large
// file is read a piece at a time, and a half-written last line waits for next
// time instead of being read twice.
func (a *Agent) Import(ctx context.Context, cursor string) (agent.ImportBatch, error) {
	data, err := os.ReadFile(a.logPath)
	if errors.Is(err, os.ErrNotExist) {
		return agent.ImportBatch{}, nil
	}
	if err != nil {
		return agent.ImportBatch{}, err
	}
	offset := 0
	if cursor != "" {
		if offset, err = strconv.Atoi(cursor); err != nil || offset > len(data) {
			return agent.ImportBatch{}, fmt.Errorf("bad cursor %q", cursor)
		}
	}

	var batch agent.ImportBatch
	for range maxImportLines {
		if err := ctx.Err(); err != nil {
			return agent.ImportBatch{}, err
		}
		end := bytes.IndexByte(data[offset:], '\n')
		if end < 0 {
			break
		}
		line := data[offset : offset+end]
		offset += end + 1
		var c compaction
		if json.Unmarshal(line, &c) != nil {
			continue // a damaged line shouldn't stop everything after it
		}
		batch.Events = append(batch.Events, agent.Compaction{
			Meta:         agent.Meta{SessionID: c.Session, Ts: c.Ts},
			TokensBefore: c.Before,
			TokensAfter:  c.After,
		})
	}
	batch.Cursor = strconv.Itoa(offset)
	batch.More = bytes.IndexByte(data[offset:], '\n') >= 0
	return batch, nil
}
