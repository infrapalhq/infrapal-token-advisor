package api

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/example"
	"github.com/infrapalhq/infrapal-token-advisor/internal/ingest"
	"github.com/infrapalhq/infrapal-token-advisor/internal/store"
)

// A record from the imaginary agent, as the OTLP receiver hands it over.
func exampleRecord(name string, ts int64, attrs map[string]any) ingest.RawEvent {
	attrs["conversation.id"] = "conv-1"
	attrs["turn.id"] = "turn-1"
	return ingest.RawEvent{
		Ts: ts, EventName: name, Body: "{}", Attrs: attrs,
		ResourceAttrs: map[string]any{"service.name": "example-cli"},
	}
}

// The whole path for an agent that isn't Claude Code: records and a local file
// go in, and findings in the agent's own terms come out, with nothing in
// between knowing which agent it is.
func TestExampleAgent_EndToEnd(t *testing.T) {
	home := t.TempDir()
	a := example.New(home)
	old := findAgent
	findAgent = func(id agent.ID) (agent.Agent, bool) {
		if id == example.ID {
			return a, true
		}
		return old(id)
	}
	t.Cleanup(func() { findAgent = old })

	logPath := filepath.Join(home, ".example-agent", "sessions.jsonl")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte(`{"session":"conv-1","ts":5000,"before":200000,"after":30000}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(filepath.Join(t.TempDir(), "token-advisor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// Push: OTLP records through the real writer.
	ctx, cancel := context.WithCancel(context.Background())
	writer := ingest.NewWriter(db, a)
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	for _, e := range []ingest.RawEvent{
		exampleRecord("example.turn_started", 1000, map[string]any{"cwd": "/work/app", "prompt.length": int64(12)}),
		exampleRecord("example.model_call", 1100, map[string]any{"model": "ex-1", "tokens.input": int64(1000), "tokens.cached": int64(800), "tokens.output": int64(50)}),
		exampleRecord("example.tool", 1200, map[string]any{"tool": "open_file", "call.id": "k1", "path": "/work/app/a.go", "output.bytes": int64(4000)}),
		exampleRecord("example.tool", 1300, map[string]any{"tool": "open_file", "call.id": "k2", "path": "/work/app/a.go", "output.bytes": int64(4000)}),
		exampleRecord("example.tool", 1400, map[string]any{"tool": "run_command", "call.id": "k3", "command": "make test", "output.bytes": int64(800)}),
		exampleRecord("example.tool", 1500, map[string]any{"tool": "run_command", "call.id": "k4", "command": "make test", "output.bytes": int64(800)}),
	} {
		writer.Enqueue(e)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("writer: %v", err)
	}

	// Pull: the local file through the real import loop.
	importCtx, stopImports := context.WithCancel(context.Background())
	imported := make(chan struct{})
	go func() {
		defer close(imported)
		ingest.RunImports(importCtx, db, []agent.Agent{a}, time.Hour)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM ui_state WHERE key = 'import:example-agent'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the import never finished")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stopImports()
	<-imported

	var stored agent.ID
	var dir string
	if err := db.QueryRow(`SELECT agent, project_dir FROM sessions WHERE session_id = 'conv-1'`).Scan(&stored, &dir); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if stored != example.ID || dir != "/work/app" {
		t.Errorf("session = %q, %q", stored, dir)
	}

	d, occ, err := computeInsights(context.Background(), db, 0, "")
	if err != nil {
		t.Fatalf("computeInsights: %v", err)
	}
	byKind := map[string]occurrence{}
	for _, o := range occ {
		byKind[o.Kind] = o
	}
	for _, kind := range []string{"re_read", "repeat_cmd", "compaction"} {
		if _, ok := byKind[kind]; !ok {
			t.Errorf("no %s finding: got %v", kind, occ)
		}
	}
	if got := byKind["repeat_cmd"]; !strings.Contains(got.Fix, ".example-rules") || got.FixWhere != "add to .example-rules" {
		t.Errorf("repeat_cmd should use the adapter's fix: %+v", got)
	}
	if got := byKind["re_read"].Fix; got != "" {
		t.Errorf("the adapter has no re-read advice, so there should be no fix: %q", got)
	}
	for _, o := range occ {
		if o.USD != nil {
			t.Errorf("%s has a dollar figure, but this agent reports none", o.Kind)
		}
		for _, text := range []string{o.Fix, o.Detail, o.Note, o.Title} {
			if strings.Contains(text, "CLAUDE.md") || strings.Contains(text, "Claude") {
				t.Errorf("%s mentions Claude to another agent's user: %q", o.Kind, text)
			}
		}
	}
	if !d.costUnknown("conv-1") {
		t.Error("the session's cost should be unknown")
	}
	if len(d.reqs) != 1 || d.reqs[0].Input != 200 || d.reqs[0].CacheRead != 800 {
		t.Errorf("requests = %+v", d.reqs)
	}
}

// Anything Claude-specific belongs in its adapter. A check or handler that
// names it here would quietly stop working for every other agent.
func TestAPI_HasNoClaudeSpecificStrings(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{`"Read"`, `"Bash"`, `"Edit"`, `"Write"`, `"Grep"`, `"Glob"`, `CLAUDE.md`, `claude mcp`, `settings.json`, `"opus"`, `repl_main_thread`}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, s := range banned {
				if strings.Contains(line, s) {
					t.Errorf("%s:%d mentions %s: move it to the adapter", file, i+1, s)
				}
			}
		}
	}
}
