package ingest

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// fakeAgent is a second agent, reporting both ways.
type fakeAgent struct {
	agent.Agent // the setup methods aren't used here
	imports     []agent.ImportBatch
	importErr   error
	cursors     []string
}

func (f *fakeAgent) ID() agent.ID { return "fake" }

func (f *fakeAgent) OwnsService(service string) bool { return service == "fake-cli" }

func (f *fakeAgent) MapLog(rec agent.LogRecord) []agent.Event {
	if rec.Name != "known" {
		return nil
	}
	sid, _ := rec.Attrs["fake.conversation"].(string)
	return []agent.Event{agent.Session{Meta: agent.Meta{SessionID: sid, Ts: rec.Ts}, ProjectDir: "/work/fake"}}
}

func (f *fakeAgent) Import(_ context.Context, cursor string) (agent.ImportBatch, error) {
	f.cursors = append(f.cursors, cursor)
	if f.importErr != nil {
		return agent.ImportBatch{}, f.importErr
	}
	if len(f.imports) == 0 {
		return agent.ImportBatch{}, nil
	}
	b := f.imports[0]
	f.imports = f.imports[1:]
	return b, nil
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

func writeEvents(t *testing.T, db *sql.DB, events ...agent.Event) {
	t.Helper()
	if err := saveImportBatch(context.Background(), db, "fake", "import:fake", agent.ImportBatch{Events: events, Cursor: "c"}); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func TestRoute_OwnedRecordGoesToTheAdapter(t *testing.T) {
	db := openTestDB(t)
	sources := logSources([]agent.Agent{&fakeAgent{}})
	e := RawEvent{
		Ts: 1000, EventName: "known", Body: "{}",
		Attrs:         map[string]any{"fake.conversation": "conv-1"},
		ResourceAttrs: map[string]any{"service.name": "fake-cli"},
	}
	if err := writeRoutedBatch(db, sources, []RawEvent{e}); err != nil {
		t.Fatalf("write: %v", err)
	}
	var id, dir string
	if err := db.QueryRow(`SELECT agent, project_dir FROM sessions WHERE session_id = 'conv-1'`).Scan(&id, &dir); err != nil {
		t.Fatalf("read session: %v", err)
	}
	if id != "fake" || dir != "/work/fake" {
		t.Errorf("session = %q, %q", id, dir)
	}
}

func TestRoute_UnknownEventIsStillKeptRaw(t *testing.T) {
	db := openTestDB(t)
	sources := logSources([]agent.Agent{&fakeAgent{}})
	e := RawEvent{Ts: 1, EventName: "brand_new", Body: "{}", ResourceAttrs: map[string]any{"service.name": "fake-cli"}}
	if err := writeRoutedBatch(db, sources, []RawEvent{e}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM events_raw WHERE event_name = 'brand_new'`); n != 1 {
		t.Errorf("raw rows = %d, want 1", n)
	}
}

func TestRoute_AnythingUnownedTakesTheClaudePath(t *testing.T) {
	db := openTestDB(t)
	sources := logSources([]agent.Agent{&fakeAgent{}})
	for _, service := range []string{"claude-code", "", "something-else"} {
		e := RawEvent{
			Ts: 1000, SessionID: "s-" + service, PromptID: "p-" + service, EventName: "user_prompt", Body: "{}",
			Attrs:         map[string]any{"prompt": "hello"},
			ResourceAttrs: map[string]any{"service.name": service},
		}
		if err := writeRoutedBatch(db, sources, []RawEvent{e}); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if n := count(t, db, `SELECT COUNT(*) FROM interactions WHERE prompt_text = 'hello'`); n != 3 {
		t.Errorf("projected prompts = %d, want 3", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM sessions WHERE agent = 'claude-code'`); n != 3 {
		t.Errorf("claude sessions = %d, want 3", n)
	}
}

func TestLogSources_SkipsAgentsThatDontExportLogs(t *testing.T) {
	type setupOnly struct{ agent.Agent }
	if got := logSources([]agent.Agent{setupOnly{}}); len(got) != 0 {
		t.Errorf("sources = %v", got)
	}
}

func TestAgentEvents_RequestsKeepUnknownCostNull(t *testing.T) {
	db := openTestDB(t)
	cost := int64(2500)
	m := agent.Meta{SessionID: "s", PromptID: "p", Ts: 10}
	writeEvents(t, db,
		agent.Request{Meta: m, Model: "m1", Tokens: agent.Tokens{Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4}, CostMicros: &cost, Success: true},
		agent.Request{Meta: m, Model: "m2", Background: true},
	)
	if n := count(t, db, `SELECT cost_usd_micros FROM api_requests WHERE model = 'm1'`); n != 2500 {
		t.Errorf("cost = %d", n)
	}
	if n := count(t, db, `SELECT cache_creation_tokens + cache_read_tokens FROM api_requests WHERE model = 'm1'`); n != 7 {
		t.Errorf("cache tokens = %d", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM api_requests WHERE model = 'm2' AND cost_usd_micros IS NULL AND query_source = ?`, queryBackground); n != 1 {
		t.Errorf("an unknown cost must stay NULL, not zero, and be marked background")
	}
}

func TestAgentEvents_TurnCostCountsTowardSpend(t *testing.T) {
	db := openTestDB(t)
	writeEvents(t, db, agent.TurnCost{Meta: agent.Meta{SessionID: "s", PromptID: "p", Ts: 10}, Micros: 700})
	if n := count(t, db, `SELECT SUM(cost_usd_micros) FROM api_requests WHERE session_id = 's'`); n != 700 {
		t.Errorf("spend = %d, want 700", n)
	}
}

func TestAgentEvents_ToolMergesPartsByCallID(t *testing.T) {
	db := openTestDB(t)
	wait, dur := int64(900), int64(40)
	ok := true
	m := agent.Meta{SessionID: "s", PromptID: "p", Ts: 10}
	writeEvents(t, db,
		agent.Tool{Meta: m, CallID: "c1", Name: "shell", Command: "ls", Approval: agent.ApprovalUser, WaitMs: &wait},
		agent.Tool{Meta: m, CallID: "c1", DurationMs: &dur, Success: &ok},
		agent.Tool{Meta: m, CallID: "c2", Name: "shell", Approval: agent.ApprovalRejected},
		agent.Tool{Meta: m, CallID: "c3", Name: "shell", Approval: agent.ApprovalAuto},
	)
	if n := count(t, db, `SELECT COUNT(*) FROM tool_calls WHERE tool_use_id = 'c1'
		AND tool_name = 'shell' AND full_command = 'ls' AND blocked_on_user_ms = 900
		AND duration_ms = 40 AND success = 1 AND decision_source = ?`, decisionSourceUser); n != 1 {
		t.Error("the second part should merge into the first without losing anything")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM tool_calls WHERE tool_use_id = 'c2' AND decision = 'reject'`); n != 1 {
		t.Error("a rejected call should read as rejected to the views")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM tool_calls WHERE tool_use_id = 'c3' AND decision_source = ?`, decisionSourceConfig); n != 1 {
		t.Error("an auto-approved call should read as config")
	}
}

func TestAgentEvents_PromptsResponsesCompactionsAndMCP(t *testing.T) {
	db := openTestDB(t)
	m := agent.Meta{SessionID: "s", PromptID: "p1", Ts: 10}
	second := agent.Meta{SessionID: "s", PromptID: "p2", Ts: 20}
	writeEvents(t, db,
		agent.Prompt{Meta: m, Text: "first", Length: 5},
		agent.Prompt{Meta: second, Length: 6},
		agent.Response{Meta: m, MessageID: "r1", Text: "done"},
		agent.Compaction{Meta: m, TokensBefore: 900, TokensAfter: 100},
		agent.MCPConnection{Meta: m, Server: "github", Status: "connected"},
	)
	if n := count(t, db, `SELECT seq FROM interactions WHERE prompt_id = 'p2'`); n != 2 {
		t.Errorf("second prompt's seq = %d, want 2: the timeline orders by it", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM interactions WHERE prompt_id = 'p2' AND prompt_text IS NULL`); n != 1 {
		t.Error("a prompt with no captured text should stay NULL")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM assistant_responses WHERE message_uuid = 'r1' AND query_source = ?`, queryMain); n != 1 {
		t.Error("response not written")
	}
	if n := count(t, db, `SELECT pre_tokens FROM compactions WHERE session_id = 's'`); n != 900 {
		t.Errorf("pre_tokens = %d", n)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM mcp_server_connections WHERE server_name = 'github'`); n != 1 {
		t.Error("MCP connection not written")
	}
}

func TestAgentEvents_WithoutASessionAreDropped(t *testing.T) {
	db := openTestDB(t)
	writeEvents(t, db, agent.Prompt{Meta: agent.Meta{PromptID: "p"}})
	if n := count(t, db, `SELECT COUNT(*) FROM interactions`); n != 0 {
		t.Errorf("interactions = %d", n)
	}
}

func TestAgentEvents_CorrectASessionAMetricCreatedFirst(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.Exec(`INSERT INTO sessions (session_id) VALUES ('s')`); err != nil {
		t.Fatal(err)
	}
	writeEvents(t, db, agent.Prompt{Meta: agent.Meta{SessionID: "s", PromptID: "p", Ts: 1}})
	if n := count(t, db, `SELECT COUNT(*) FROM sessions WHERE session_id = 's' AND agent = 'fake'`); n != 1 {
		t.Error("the session should move to its real agent")
	}
}

func prompt(pid string) agent.Event {
	return agent.Prompt{Meta: agent.Meta{SessionID: "s", PromptID: pid, Ts: 1}, Length: 1}
}

func TestImport_ContinuesUntilDoneAndResumesFromItsCursor(t *testing.T) {
	db := openTestDB(t)
	f := &fakeAgent{imports: []agent.ImportBatch{
		{Events: []agent.Event{prompt("p1")}, Cursor: "offset-1", More: true},
		{Events: []agent.Event{prompt("p2")}, Cursor: "offset-2"},
	}}
	if err := runImport(context.Background(), db, f.ID(), f); err != nil {
		t.Fatalf("runImport: %v", err)
	}
	if err := runImport(context.Background(), db, f.ID(), f); err != nil {
		t.Fatalf("second runImport: %v", err)
	}
	want := []string{"", "offset-1", "offset-2"}
	if len(f.cursors) != 3 || f.cursors[0] != want[0] || f.cursors[1] != want[1] || f.cursors[2] != want[2] {
		t.Errorf("cursors = %q, want %q", f.cursors, want)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM interactions`); n != 2 {
		t.Errorf("interactions = %d, want 2", n)
	}
}

func TestImport_EmptyResultKeepsItsPlace(t *testing.T) {
	db := openTestDB(t)
	f := &fakeAgent{imports: []agent.ImportBatch{{Events: []agent.Event{prompt("p1")}, Cursor: "offset-1"}, {}}}
	for range 3 {
		if err := runImport(context.Background(), db, f.ID(), f); err != nil {
			t.Fatalf("runImport: %v", err)
		}
	}
	if last := f.cursors[len(f.cursors)-1]; last != "offset-1" {
		t.Errorf("cursor = %q, want offset-1: an empty batch mustn't restart the import", last)
	}
}

func TestImport_FailedBatchDoesntMoveTheCursor(t *testing.T) {
	db := openTestDB(t)
	// Dropping a table makes the batch's second event fail to write.
	if _, err := db.Exec(`DROP TABLE compactions`); err != nil {
		t.Fatal(err)
	}
	f := &fakeAgent{imports: []agent.ImportBatch{{
		Events: []agent.Event{
			prompt("p1"),
			agent.Compaction{Meta: agent.Meta{SessionID: "s", Ts: 1}},
		},
		Cursor: "offset-1",
	}}}
	if err := runImport(context.Background(), db, f.ID(), f); err == nil {
		t.Fatal("want the write error")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM ui_state WHERE key = 'import:fake'`); n != 0 {
		t.Error("the cursor must not move past a batch that wasn't saved")
	}
	if n := count(t, db, `SELECT COUNT(*) FROM interactions`); n != 0 {
		t.Error("a failed batch must leave nothing behind, or a retry would duplicate it")
	}
}

func TestImport_SourceErrorIsReturned(t *testing.T) {
	db := openTestDB(t)
	boom := errors.New("can't read file")
	f := &fakeAgent{importErr: boom}
	if err := runImport(context.Background(), db, f.ID(), f); !errors.Is(err, boom) {
		t.Errorf("err = %v", err)
	}
}

func TestImport_StuckImporterStops(t *testing.T) {
	db := openTestDB(t)
	stuck := agent.ImportBatch{Cursor: "same", More: true}
	f := &fakeAgent{imports: []agent.ImportBatch{stuck, stuck, stuck}}
	if err := runImport(context.Background(), db, f.ID(), f); err == nil {
		t.Fatal("an importer that never advances must not spin forever")
	}
}

func TestRunImports_StopsWithItsContextAndRecordsFailures(t *testing.T) {
	db := openTestDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	f := &failThenCancel{fakeAgent: fakeAgent{importErr: errors.New("nope")}, cancel: cancel}
	RunImports(ctx, db, []agent.Agent{f}, 1)
	if n := count(t, db, `SELECT COUNT(*) FROM ingest_errors WHERE signal = 'import'`); n != 1 {
		t.Errorf("recorded errors = %d, want 1", n)
	}
}

// failThenCancel ends RunImports after its first attempt.
type failThenCancel struct {
	fakeAgent
	cancel context.CancelFunc
}

func (f *failThenCancel) Import(ctx context.Context, cursor string) (agent.ImportBatch, error) {
	defer f.cancel()
	return f.fakeAgent.Import(ctx, cursor)
}

func TestRunImports_WithNoImportersReturns(t *testing.T) {
	type setupOnly struct{ agent.Agent }
	RunImports(context.Background(), openTestDB(t), []agent.Agent{setupOnly{}}, 1)
}
