// Package agenttest is the contract every adapter must meet. Being listed in
// internal/agent/all gets an adapter the basic checks. To cover what's special
// about yours, call Run from its own tests with sample data:
//
//	func TestContract(t *testing.T) {
//		agenttest.Run(t, agenttest.Suite{
//			New:     func(home string) agent.Agent { return New(home) },
//			Samples: []agent.LogRecord{{Name: "tool_call", Ts: 1, Attrs: ...}},
//		})
//	}
package agenttest

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// Suite describes the adapter under test.
type Suite struct {
	// New builds the adapter for a home directory. Each check gets a fresh one.
	New func(home string) agent.Agent
	// Tools maps tool names to the category they should have.
	Tools map[string]agent.ToolCategory
	// Samples are real log records. Capture these from the agent itself: the
	// contract can only check what it's shown.
	Samples []agent.LogRecord
	// SeedImport writes local data for Importer to read, under home.
	SeedImport func(t *testing.T, home string)
	// NativeIngest is for Claude Code alone, whose records have their own path.
	NativeIngest bool
}

var idPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Run checks an adapter against the contract.
func Run(t *testing.T, s Suite) {
	t.Helper()
	build := func(t *testing.T) (agent.Agent, string) {
		t.Helper()
		home := t.TempDir()
		return s.New(home), home
	}

	t.Run("identity", func(t *testing.T) {
		a, _ := build(t)
		if !idPattern.MatchString(string(a.ID())) {
			t.Errorf("ID %q should be lowercase words with dashes: it's stored in the database", a.ID())
		}
		if a.Name() == "" {
			t.Error("Name is empty")
		}
		switch a.CostSource() {
		case agent.CostNone, agent.CostPerRequest, agent.CostPerTurn, agent.CostComputed:
		default:
			t.Errorf("unknown CostSource %q", a.CostSource())
		}
	})

	t.Run("brings data in", func(t *testing.T) {
		a, _ := build(t)
		_, logs := a.(agent.LogSource)
		_, imports := a.(agent.Importer)
		if !logs && !imports && !s.NativeIngest {
			t.Error("implement agent.LogSource, agent.Importer or both, or nothing can reach the dashboard")
		}
	})

	t.Run("tool categories", func(t *testing.T) {
		a, _ := build(t)
		for _, name := range []string{"", "a-tool-nobody-has-heard-of"} {
			if got := a.ToolCategory(name); got != agent.ToolOther {
				t.Errorf("ToolCategory(%q) = %q, want ToolOther: unknown tools must never pass as safe", name, got)
			}
		}
		for name, want := range s.Tools {
			if got := a.ToolCategory(name); got != want {
				t.Errorf("ToolCategory(%q) = %q, want %q", name, got, want)
			}
		}
	})

	t.Run("premium model", func(t *testing.T) {
		a, _ := build(t)
		if a.PremiumModel("") {
			t.Error("an empty model name shouldn't be premium")
		}
	})

	t.Run("connect", func(t *testing.T) { checkConnect(t, s) })

	if a, _ := build(t); isLogSource(a) {
		t.Run("logs", func(t *testing.T) { checkLogs(t, s) })
	}
	if a, _ := build(t); isImporter(a) {
		t.Run("import", func(t *testing.T) { checkImport(t, s) })
	}
	if a, _ := build(t); isAdvisor(a) {
		t.Run("advice", func(t *testing.T) { checkAdvice(t, s) })
	}
}

func isLogSource(a agent.Agent) bool { _, ok := a.(agent.LogSource); return ok }
func isImporter(a agent.Agent) bool  { _, ok := a.(agent.Importer); return ok }
func isAdvisor(a agent.Agent) bool   { _, ok := a.(agent.Advisor); return ok }

func checkConnect(t *testing.T, s Suite) {
	a := s.New(t.TempDir())
	var out bytes.Buffer
	before, err := a.Check(4318)
	if err != nil {
		t.Fatalf("Check before Connect: %v", err)
	}
	if countFailed(before) == 0 {
		t.Error("Check should fail before Connect, or doctor can't spot an agent that isn't set up")
	}

	conn, err := a.Connect(&out, agent.ConnectOptions{OTLPPort: 4318})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	again, err := a.Connect(&out, agent.ConnectOptions{OTLPPort: 4318, Previous: conn})
	if err != nil {
		t.Fatalf("a second Connect must be safe: %v", err)
	}
	if conn.BackupPath != "" && again.BackupPath != conn.BackupPath {
		t.Errorf("a second Connect replaced the backup %q with %q: the original would be lost", conn.BackupPath, again.BackupPath)
	}
	if out.Len() == 0 {
		t.Error("Connect should say what it did")
	}

	checks, err := a.Check(4318)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	for _, c := range checks {
		if c.Label == "" {
			t.Errorf("a check has no label: %+v", c)
		}
		if !c.Pass && !c.Skipped && c.Fix == "" {
			t.Errorf("failed check %q says nothing about how to fix it", c.Label)
		}
	}

	if err := a.Disconnect(&out, again); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if err := a.Disconnect(&out, agent.Connection{}); err != nil {
		t.Errorf("Disconnect with nothing to do must be safe: %v", err)
	}
}

func countFailed(checks []agent.Check) int {
	n := 0
	for _, c := range checks {
		if !c.Pass && !c.Skipped {
			n++
		}
	}
	return n
}

func checkLogs(t *testing.T, s Suite) {
	a := s.New(t.TempDir())
	src := a.(agent.LogSource)

	for _, other := range []string{"", "claude-code", "someone-elses-agent"} {
		if src.OwnsService(other) {
			t.Errorf("OwnsService(%q): claiming it would take another agent's data", other)
		}
	}

	junk := []agent.LogRecord{
		{},
		{Name: "a.record.nobody.has.seen", Ts: 1},
		{Name: "a.record.nobody.has.seen", Ts: 1, Attrs: map[string]any{"session.id": "s"}},
	}
	// Each real record with its values swapped for the wrong types, because
	// attributes come from outside the program.
	for _, rec := range s.Samples {
		broken := map[string]any{}
		for k := range rec.Attrs {
			broken[k] = []any{"not", "what", "you", "expected"}
		}
		junk = append(junk, agent.LogRecord{Name: rec.Name, Ts: rec.Ts}, agent.LogRecord{Name: rec.Name, Ts: rec.Ts, Attrs: broken})
	}
	for _, rec := range junk {
		mustNotPanic(t, fmt.Sprintf("MapLog(%+v)", rec), func() { src.MapLog(rec) })
	}
	if len(src.MapLog(agent.LogRecord{Name: "a.record.nobody.has.seen", Ts: 1})) != 0 {
		t.Error("an unrecognised record should map to nothing: it's already kept raw")
	}

	mapped := 0
	for _, rec := range s.Samples {
		events := src.MapLog(rec)
		mapped += len(events)
		checkEvents(t, a, events)
	}
	if len(s.Samples) > 0 && mapped == 0 {
		t.Error("no sample mapped to any event")
	}
}

func mustNotPanic(t *testing.T, what string, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s panicked: %v. Bad input from an agent must not stop ingest", what, r)
		}
	}()
	fn()
}

// checkEvents holds events to what the agent says about itself.
func checkEvents(t *testing.T, a agent.Agent, events []agent.Event) {
	t.Helper()
	caps := a.Capabilities()
	for _, ev := range events {
		if m := ev.Base(); m.SessionID == "" || m.Ts == 0 {
			t.Errorf("%T needs a session and a timestamp: %+v", ev, m)
		}
		switch e := ev.(type) {
		case agent.Request:
			if e.Tokens.Input < 0 || e.Tokens.Output < 0 || e.Tokens.CacheRead < 0 || e.Tokens.CacheWrite < 0 {
				t.Errorf("negative tokens: %+v", e.Tokens)
			}
			if a.CostSource() != agent.CostPerRequest && e.CostMicros != nil {
				t.Errorf("Request has a cost, but CostSource is %q", a.CostSource())
			}
		case agent.TurnCost:
			if a.CostSource() != agent.CostPerTurn {
				t.Errorf("TurnCost sent, but CostSource is %q", a.CostSource())
			}
		case agent.Tool:
			if e.CallID == "" {
				t.Errorf("Tool has no CallID, so its parts can't be merged: %+v", e)
			}
			if e.Name != "" && e.Category != a.ToolCategory(e.Name) {
				t.Errorf("Tool %q has category %q, but ToolCategory says %q", e.Name, e.Category, a.ToolCategory(e.Name))
			}
			if e.File != "" && !caps.Has(agent.CapFilePaths) {
				t.Errorf("Tool carries a file, so claim CapFilePaths")
			}
			if e.Command != "" && !caps.Has(agent.CapCommands) {
				t.Errorf("Tool carries a command, so claim CapCommands")
			}
			if e.WaitMs != nil && !caps.Has(agent.CapApprovalWait) {
				t.Errorf("Tool carries a wait, so claim CapApprovalWait")
			}
		case agent.Compaction:
			if !caps.Has(agent.CapCompaction) {
				t.Errorf("Compaction sent, so claim CapCompaction")
			}
		case agent.MCPConnection:
			if !caps.Has(agent.CapMCPConnections) {
				t.Errorf("MCPConnection sent, so claim CapMCPConnections")
			}
		}
	}
}

func checkImport(t *testing.T, s Suite) {
	home := t.TempDir()
	if s.SeedImport != nil {
		s.SeedImport(t, home)
	}
	a := s.New(home)
	imp := a.(agent.Importer)
	ctx := context.Background()

	if _, err := s.New(t.TempDir()).(agent.Importer).Import(ctx, ""); err != nil {
		t.Errorf("Import with no local data should return nothing, not fail: %v", err)
	}

	var cursor string
	total := 0
	for step := 0; ; step++ {
		if step > 100000 {
			t.Fatal("Import never finished")
		}
		batch, err := imp.Import(ctx, cursor)
		if err != nil {
			t.Fatalf("Import(%q): %v", cursor, err)
		}
		checkEvents(t, a, batch.Events)
		total += len(batch.Events)
		if batch.More && (batch.Cursor == "" || batch.Cursor == cursor) {
			t.Fatalf("Import asked to continue without moving its cursor from %q", cursor)
		}
		if batch.Cursor != "" {
			cursor = batch.Cursor
		}
		if !batch.More {
			break
		}
	}
	if s.SeedImport != nil && total == 0 {
		t.Error("Import returned nothing from the seeded data")
	}

	again, err := imp.Import(ctx, cursor)
	if err != nil {
		t.Fatalf("Import from the end: %v", err)
	}
	if len(again.Events) != 0 {
		t.Errorf("resuming returned %d events again: they would be stored twice", len(again.Events))
	}
}

func checkAdvice(t *testing.T, s Suite) {
	adv := s.New(t.TempDir()).(agent.Advisor)
	if adv.CompactionDetail() == "" || adv.SmallerModelNote() == "" {
		t.Error("CompactionDetail and SmallerModelNote appear on findings, so they can't be empty")
	}
	if adv.ShellPattern("ls") == "" {
		t.Error("ShellPattern should never be empty: it names the finding")
	}
	for name, fix := range map[string]agent.Fix{
		"ReReadFix":        adv.ReReadFix(),
		"RepeatCommandFix": adv.RepeatCommandFix("ls", "go"),
		"AllowFix":         adv.AllowFix("ls"),
		"RemoveMCPFix":     adv.RemoveMCPFix("server"),
	} {
		if fix.Text != "" && fix.Where == "" {
			t.Errorf("%s gives a fix with no Where, so nobody knows where it goes", name)
		}
	}
}
