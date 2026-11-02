package example

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
	"github.com/infrapalhq/infrapal-token-advisor/internal/agent/agenttest"
)

// samples returns one of each record the imaginary agent sends. For a real
// agent, capture these from a real session.
func samples() []agent.LogRecord {
	base := func(extra map[string]any) map[string]any {
		attrs := map[string]any{"conversation.id": "c1", "turn.id": "t1"}
		for k, v := range extra {
			attrs[k] = v
		}
		return attrs
	}
	return []agent.LogRecord{
		{Name: "example.turn_started", Ts: 1000, Attrs: base(map[string]any{"cwd": "/work/app", "prompt.length": int64(12)})},
		{Name: "example.model_call", Ts: 1100, Attrs: base(map[string]any{"model": "ex-1", "tokens.input": int64(1000), "tokens.cached": int64(800), "tokens.output": int64(50)})},
		{Name: "example.tool", Ts: 1200, Attrs: base(map[string]any{"tool": "open_file", "call.id": "k1", "path": "/work/app/a.go", "output.bytes": int64(4000)})},
	}
}

func TestContract(t *testing.T) {
	agenttest.Run(t, agenttest.Suite{
		New: func(home string) agent.Agent { return New(home) },
		Tools: map[string]agent.ToolCategory{
			"open_file":   agent.ToolRead,
			"find_text":   agent.ToolSearch,
			"write_file":  agent.ToolEdit,
			"run_command": agent.ToolShell,
			"mcp.github":  agent.ToolMCP,
		},
		Samples: samples(),
		SeedImport: func(t *testing.T, home string) {
			var lines strings.Builder
			for i := range 1200 { // more than one batch
				fmt.Fprintf(&lines, `{"session":"c1","ts":%d,"before":900,"after":100}`+"\n", i+1)
			}
			writeLog(t, home, lines.String())
		},
	})
}

func writeLog(t *testing.T, home, content string) {
	t.Helper()
	path := New(home).logPath
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestMapLog_SeparatesCachedTokens(t *testing.T) {
	events := New(t.TempDir()).MapLog(samples()[1])
	req := events[0].(agent.Request)
	if req.Tokens.Input != 200 || req.Tokens.CacheRead != 800 {
		t.Errorf("tokens = %+v", req.Tokens)
	}
}

func TestImport_WaitsForAHalfWrittenLine(t *testing.T) {
	home := t.TempDir()
	writeLog(t, home, `{"session":"c1","ts":1,"before":9,"after":1}`+"\n"+`{"session":"c1","ts":2,`)
	a := New(home)
	first, err := a.Import(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Events) != 1 {
		t.Fatalf("events = %d, want only the finished line", len(first.Events))
	}

	writeLog(t, home, `{"session":"c1","ts":1,"before":9,"after":1}`+"\n"+`{"session":"c1","ts":2,"before":9,"after":1}`+"\n")
	second, err := a.Import(context.Background(), first.Cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Events) != 1 || second.Events[0].Base().Ts != 2 {
		t.Errorf("events = %+v, want just the line that finished", second.Events)
	}
}

func TestImport_SkipsADamagedLine(t *testing.T) {
	home := t.TempDir()
	writeLog(t, home, "not json\n"+`{"session":"c1","ts":2,"before":9,"after":1}`+"\n")
	batch, err := New(home).Import(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Events) != 1 {
		t.Errorf("events = %d, want 1", len(batch.Events))
	}
}

func TestConnect_RestoresWhatWasThere(t *testing.T) {
	home := t.TempDir()
	a := New(home)
	if err := os.MkdirAll(filepath.Dir(a.configPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.configPath, []byte(`{"mine":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	conn, err := a.Connect(&out, agent.ConnectOptions{OTLPPort: 4318})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Disconnect(&out, conn); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(a.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"mine":true}` {
		t.Errorf("config = %s", got)
	}
}
