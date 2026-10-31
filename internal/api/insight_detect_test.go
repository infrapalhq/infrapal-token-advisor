package api

import (
	"fmt"
	"strings"
	"testing"
)

// fixture builds insightData directly so each rule is tested in isolation.
func fixture(tools []toolRow, reqs []reqRow, waits map[string]waitRow) *insightData {
	d := &insightData{sessions: map[string]sessionRow{"s": {ID: "s", StartedAt: 1}}, waits: waits, toolByID: map[string]toolRow{}}
	if d.waits == nil {
		d.waits = map[string]waitRow{}
	}
	for i := range tools {
		tools[i].SessionID = "s"
		if tools[i].PromptID == "" {
			tools[i].PromptID = "p1"
		}
		if tools[i].Ts == 0 {
			tools[i].Ts = int64(i + 1)
		}
		tools[i].Success = -1
		d.tools = append(d.tools, tools[i])
	}
	d.reqs = reqs
	return d
}

func read(id, file string, result int64, offset, limit int64) toolRow {
	return toolRow{ID: id, Name: "Read", File: file, ResultBytes: result, Offset: offset, Limit: limit}
}

func bash(id, cmd string, result int64) toolRow {
	return toolRow{ID: id, Name: "Bash", Command: cmd, ResultBytes: result}
}

func edit(id, file string) toolRow {
	return toolRow{ID: id, Name: "Edit", File: file}
}

func TestReReadFiresOnRepeatedUnchangedRead(t *testing.T) {
	d := fixture([]toolRow{
		read("r1", "/repo/main.tf", 31000, 0, 0),
		read("r2", "/repo/main.tf", 31000, 0, 0),
	}, nil, nil)
	occ := detectReRead(d)
	if len(occ) != 1 {
		t.Fatalf("want 1 occurrence, got %d", len(occ))
	}
	o := occ[0]
	if o.Count != 2 || o.Severity != "half" {
		t.Fatalf("2 reads: want count 2 half, got count %d %s", o.Count, o.Severity)
	}
	if o.Tokens == nil || *o.Tokens != 31000/4 {
		t.Fatalf("tokens should be re-read bytes/4, got %v", o.Tokens)
	}
	if len(o.Evidence) != 2 {
		t.Fatalf("evidence should list both reads, got %v", o.Evidence)
	}
}

func TestReReadThirdReadIsFullSeverity(t *testing.T) {
	d := fixture([]toolRow{
		read("r1", "/repo/main.tf", 100, 0, 0),
		read("r2", "/repo/main.tf", 100, 0, 0),
		read("r3", "/repo/main.tf", 100, 0, 0),
	}, nil, nil)
	occ := detectReRead(d)
	if len(occ) != 1 || occ[0].Count != 3 || occ[0].Severity != "full" {
		t.Fatalf("3 reads should be full: %+v", occ)
	}
}

func TestReReadSuppressedByEditBetween(t *testing.T) {
	d := fixture([]toolRow{
		read("r1", "/repo/main.tf", 100, 0, 0),
		edit("e1", "/repo/main.tf"),
		read("r2", "/repo/main.tf", 100, 0, 0),
	}, nil, nil)
	if occ := detectReRead(d); len(occ) != 0 {
		t.Fatalf("an edit between reads means the file changed; got %+v", occ)
	}
}

func TestReReadSuppressedByShellMutation(t *testing.T) {
	d := fixture([]toolRow{
		read("r1", "/repo/main.tf", 100, 0, 0),
		bash("b1", "sed -i 's/a/b/' main.tf", 0),
		read("r2", "/repo/main.tf", 100, 0, 0),
	}, nil, nil)
	if occ := detectReRead(d); len(occ) != 0 {
		t.Fatalf("a shell mutation between reads should suppress; got %+v", occ)
	}
}

func TestReReadPaginatedReadsDoNotOverlap(t *testing.T) {
	d := fixture([]toolRow{
		read("r1", "/repo/big.go", 100, 0, 100),   // lines 0..100
		read("r2", "/repo/big.go", 100, 500, 100), // lines 500..600
	}, nil, nil)
	if occ := detectReRead(d); len(occ) != 0 {
		t.Fatalf("disjoint ranges are not re-reads; got %+v", occ)
	}
}

func TestReReadIsScopedToPrompt(t *testing.T) {
	tools := []toolRow{read("r1", "/repo/main.tf", 100, 0, 0), read("r2", "/repo/main.tf", 100, 0, 0)}
	tools[1].PromptID = "p2"
	d := fixture(tools, nil, nil)
	if occ := detectReRead(d); len(occ) != 0 {
		t.Fatalf("reads in different prompts are not one prompt's re-reads; got %+v", occ)
	}
}

func TestRepeatCommandFiresWithoutChange(t *testing.T) {
	d := fixture([]toolRow{
		bash("b1", "terraform  plan", 18000),
		bash("b2", "terraform plan", 18000),
	}, nil, nil)
	occ := detectRepeatCommand(d)
	if len(occ) != 1 || occ[0].Subject != "terraform plan" {
		t.Fatalf("want one occurrence for terraform plan, got %+v", occ)
	}
	if !strings.Contains(occ[0].Fix, "terraform plan") {
		t.Fatalf("fix should name the command: %q", occ[0].Fix)
	}
}

func TestRepeatCommandSuppressedByEdit(t *testing.T) {
	d := fixture([]toolRow{
		bash("b1", "terraform plan", 18000),
		edit("e1", "/repo/main.tf"),
		bash("b2", "terraform plan", 18000),
	}, nil, nil)
	if occ := detectRepeatCommand(d); len(occ) != 0 {
		t.Fatalf("a file edit between runs makes the rerun legitimate; got %+v", occ)
	}
}

func TestRepeatApprovalSafePatternGetsAllowRule(t *testing.T) {
	waits := map[string]waitRow{"a1": {ToolUseID: "a1", WaitMs: 40000}, "a2": {ToolUseID: "a2", WaitMs: 35000}}
	d := fixture([]toolRow{
		{ID: "a1", Name: "Bash", Command: "ls -la modules/db"},
		{ID: "a2", Name: "Bash", Command: "ls -la modules"},
	}, nil, waits)
	occ := detectRepeatApproval(d)
	if len(occ) != 1 {
		t.Fatalf("want 1 pattern, got %+v", occ)
	}
	if !strings.Contains(occ[0].Fix, `"Bash(ls:*)"`) || occ[0].FixLang != "settings-json" {
		t.Fatalf("safe pattern should yield a settings.json allow rule, got fix=%q lang=%q", occ[0].Fix, occ[0].FixLang)
	}
	if occ[0].Minutes == nil || *occ[0].Minutes < 1.2 || *occ[0].Minutes > 1.3 {
		t.Fatalf("minutes = sum of waits (75s = 1.25 min), got %v", occ[0].Minutes)
	}
}

func TestRepeatApprovalUnsafePatternHasNoFix(t *testing.T) {
	waits := map[string]waitRow{"a1": {ToolUseID: "a1", WaitMs: 5000}, "a2": {ToolUseID: "a2", WaitMs: 5000}}
	d := fixture([]toolRow{
		{ID: "a1", Name: "Bash", Command: "rm -rf build"},
		{ID: "a2", Name: "Bash", Command: "rm -rf dist"},
	}, nil, waits)
	occ := detectRepeatApproval(d)
	if len(occ) != 1 {
		t.Fatalf("want 1 occurrence, got %+v", occ)
	}
	if occ[0].Fix != "" {
		t.Fatalf("a write command must never get an allow rule, got %q", occ[0].Fix)
	}
	if occ[0].Note == "" {
		t.Fatalf("unsafe pattern should explain why it has no fix")
	}
}

func TestApprovalNeedsTwoOccurrences(t *testing.T) {
	waits := map[string]waitRow{"a1": {ToolUseID: "a1", WaitMs: 9000}}
	d := fixture([]toolRow{{ID: "a1", Name: "Bash", Command: "ls"}}, nil, waits)
	if occ := detectRepeatApproval(d); len(occ) != 0 {
		t.Fatalf("one approval is not a pattern; got %+v", occ)
	}
}

func TestSafePatternRejectsWriteMarkers(t *testing.T) {
	cases := map[string]bool{
		"ls -la":                   true,
		"git status":               true,
		"echo hi > notes.txt":      false,
		"find . -name x -delete":   false,
		"git push origin main":     false,
		"cat a | sh":               false,
		"ls -la 2>&1":              true,
		"echo $PATH | tr ':' '\n'": true,
		"ls | xargs rm":            false,
		"git status && rm -rf x":   false,
		"ls -la modules/db":        true,
	}
	for cmd, want := range cases {
		if got := isSafePattern(toolRow{Name: "Bash", Command: cmd}); got != want {
			t.Errorf("isSafePattern(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestOversizedModelFiresOnSmallOpusTurn(t *testing.T) {
	d := fixture([]toolRow{read("r1", "/repo/a.md", 100, 0, 0)}, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "claude-opus-5-5", QuerySource: "repl_main_thread", Output: 400, CostUSD: 0.8},
	}, nil)
	occ := detectOversizedModel(d)
	if len(occ) != 1 || occ[0].USD == nil || *occ[0].USD != 0.8 {
		t.Fatalf("small Opus turn should fire with its spend, got %+v", occ)
	}
}

func TestOversizedModelIgnoresBigOrEditingTurns(t *testing.T) {
	big := fixture(nil, []reqRow{{PromptID: "p1", SessionID: "s", Model: "claude-opus-5-5", QuerySource: "repl_main_thread", Output: 9000}}, nil)
	if occ := detectOversizedModel(big); len(occ) != 0 {
		t.Fatalf("large output should not fire: %+v", occ)
	}
	editing := fixture([]toolRow{edit("e1", "/repo/a.go")}, []reqRow{{PromptID: "p1", SessionID: "s", Model: "claude-opus-5-5", QuerySource: "repl_main_thread", Output: 100}}, nil)
	if occ := detectOversizedModel(editing); len(occ) != 0 {
		t.Fatalf("a turn that edits files should not fire: %+v", occ)
	}
}

func TestAggregateItemsPricesTokensAndRanks(t *testing.T) {
	tok := int64(4000)
	usd := 0.5
	occ := []occurrence{
		{Kind: "re_read", Subject: "/a.tf", Area: "context", SessionID: "s1", Ts: 10, Tokens: &tok, Conf: "Likely", Severity: "half", Title: "a read 2×"},
		{Kind: "re_read", Subject: "/a.tf", Area: "context", SessionID: "s2", Ts: 20, Tokens: &tok, Conf: "Likely", Severity: "full", Title: "a read 2×"},
		{Kind: "compaction", Subject: "", Area: "context", SessionID: "s1", Ts: 30, USD: &usd, Conf: "Measured", Severity: "full", Title: "compacted"},
	}
	items := aggregateItems(occ, 0.0001) // rate: $0.0001 per token
	if len(items) != 2 {
		t.Fatalf("want 2 items, got %d", len(items))
	}
	re := items[0]
	for _, it := range items {
		if it.Kind == "re_read" {
			re = it
		}
	}
	if re.Occurrences != 2 || re.Sessions != 2 {
		t.Fatalf("re_read: want 2 occurrences across 2 sessions, got %d/%d", re.Occurrences, re.Sessions)
	}
	if re.USD == nil || *re.USD < 0.79 || *re.USD > 0.81 {
		t.Fatalf("8000 tokens at $0.0001 = $0.80, got %v", re.USD)
	}
	if re.Severity != "full" {
		t.Fatalf("an item takes the worst severity of its occurrences")
	}
}

func TestPriorityOfSessionFollowsWorstArea(t *testing.T) {
	if priorityOf(map[string]string{"context": "half"}) != "look" {
		t.Fatal("half -> look")
	}
	if priorityOf(map[string]string{"context": "half", "perm": "full"}) != "now" {
		t.Fatal("any full -> now")
	}
	if priorityOf(nil) != "fine" {
		t.Fatal("nothing -> fine")
	}
}

func TestDurationSinceRejectsUnknown(t *testing.T) {
	db := seedDB(t)
	if _, err := durationSince(t.Context(), db, "fortnight-ish"); err == nil {
		t.Fatal("unknown range token must error")
	}
	if v, err := durationSince(t.Context(), db, "all"); err != nil || v != 0 {
		t.Fatalf("all = no cutoff, got %d %v", v, err)
	}
	if v, err := durationSince(t.Context(), db, "install"); err != nil || v != 0 {
		t.Fatalf("install with no ui_state row = no cutoff, got %d %v", v, err)
	}
}

func TestContextBloatFlagsSessionOverLimit(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, QuerySource: "repl_main_thread", Context: 150_000},
		{PromptID: "p2", SessionID: "s", Ts: 2, QuerySource: "repl_main_thread", Context: 369_000},
		{PromptID: "p2", SessionID: "s", Ts: 3, QuerySource: "repl_main_thread", Context: 250_000},
	}, nil)
	occ := detectContextBloat(d)
	if len(occ) != 1 {
		t.Fatalf("want 1 occurrence, got %d", len(occ))
	}
	o := occ[0]
	if o.Area != "context" || o.Severity != "half" || o.Evidence[0] != "u:p2" || o.Count != 1 {
		t.Fatalf("unexpected occurrence: %+v", o)
	}
	if !strings.Contains(o.Detail, "~369K") || !strings.Contains(o.Detail, "1 of 2 prompts") {
		t.Fatalf("detail should give the peak and prompt share: %q", o.Detail)
	}
}

func TestContextBloatIgnoresBackgroundRequests(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, QuerySource: "repl_main_thread", Context: 200_000},
		{PromptID: "p1", SessionID: "s", Ts: 2, QuerySource: "prompt_suggestion", Context: 500_000},
	}, nil)
	if occ := detectContextBloat(d); len(occ) != 0 {
		t.Fatalf("exactly at the limit, plus a background call, should not fire: %+v", occ)
	}
}

func TestManyToolCallsFiresAtThreshold(t *testing.T) {
	tools := make([]toolRow, 0, manyToolCallsPerPrompt)
	for i := 0; i < manyToolCallsPerPrompt-1; i++ {
		tools = append(tools, toolRow{ID: fmt.Sprintf("t%d", i), Name: "Bash"})
	}
	if occ := detectManyToolCalls(fixture(tools, nil, nil)); len(occ) != 0 {
		t.Fatalf("below threshold should not fire: %+v", occ)
	}
	tools = append(tools, toolRow{ID: "last", Name: "Bash"})
	occ := detectManyToolCalls(fixture(tools, nil, nil))
	if len(occ) != 1 || occ[0].Area != "exec" || occ[0].Count != manyToolCallsPerPrompt || occ[0].Evidence[0] != "u:p1" {
		t.Fatalf("at threshold should fire once: %+v", occ)
	}
}

func TestCompactionUsesBilledSizes(t *testing.T) {
	d := fixture(nil, []reqRow{
		{SessionID: "s", Ts: 990, QuerySource: "compact", Context: 421_000},
		{SessionID: "s", Ts: 1001, QuerySource: "prompt_suggestion", Context: 999_000},
		{SessionID: "s", Ts: 1002, QuerySource: "repl_main_thread", Context: 70_000},
	}, nil)
	d.compactions = []compactRow{{ID: 1, SessionID: "s", Ts: 1000, Pre: 223_000, Post: 24_000}}
	occ := detectCompaction(d)
	if len(occ) != 1 || !strings.Contains(occ[0].Title, "~421K → ~70K") {
		t.Fatalf("title should use billed sizes: %+v", occ)
	}
}

func TestCompactionFallsBackToEventSizes(t *testing.T) {
	d := fixture(nil, nil, nil)
	d.compactions = []compactRow{{ID: 1, SessionID: "s", Ts: 1000, Pre: 223_000, Post: 24_000}}
	occ := detectCompaction(d)
	if len(occ) != 1 || !strings.Contains(occ[0].Title, "~223K → ~24K") {
		t.Fatalf("title should fall back to the event: %+v", occ)
	}
}

func TestAggregateItemsDoesNotCountInstallWideAsSession(t *testing.T) {
	items := aggregateItems([]occurrence{{Kind: "dead_mcp", Subject: "x", Area: "context", Severity: "half", Conf: "Measured"}}, 0)
	if len(items) != 1 || items[0].Sessions != 0 {
		t.Fatalf("install-wide item should have 0 sessions: %+v", items)
	}
}

func TestNormalizeMCPServerMatchesBothSpellings(t *testing.T) {
	if normalizeMCPServer("claude.ai Claude Docs") != normalizeMCPServer("claude_ai_Claude_Docs") {
		t.Fatal("connection and tool-call spellings should normalize to the same name")
	}
}

func TestToolFailuresFiresAtThreshold(t *testing.T) {
	var tools []toolRow
	for i := 0; i < minFailedToolCalls; i++ {
		tools = append(tools, bash(fmt.Sprintf("b%d", i), "go test", 0))
	}
	tools = append(tools, toolRow{ID: "r1", Name: "Read"})
	d := fixture(tools, nil, nil)
	for i := range d.tools {
		d.tools[i].Success = 1
		if d.tools[i].Name == "Bash" {
			d.tools[i].Success = 0
		}
	}
	occ := detectToolFailures(d)
	if len(occ) != 1 || occ[0].Count != minFailedToolCalls || len(occ[0].Evidence) != minFailedToolCalls {
		t.Fatalf("want one occurrence listing each failure: %+v", occ)
	}
	if !strings.Contains(occ[0].Detail, "5 of 6") || !strings.Contains(occ[0].Detail, "Bash (5)") {
		t.Fatalf("detail should give share and top tool: %q", occ[0].Detail)
	}
	d.tools[0].Success = 1
	if occ := detectToolFailures(d); len(occ) != 0 {
		t.Fatalf("below threshold should not fire: %+v", occ)
	}
}

func TestOutputSpikesListsLargestFirstAndIsUnpriced(t *testing.T) {
	d := fixture([]toolRow{
		read("small", "/a", outputSpikeTokens*4, 0, 0),
		read("big", "/b", 40_004+4, 0, 0),
		read("bigger", "/c", 120_000, 0, 0),
	}, nil, nil)
	occ := detectOutputSpikes(d)
	if len(occ) != 1 || occ[0].Count != 2 || occ[0].Evidence[0] != "t:bigger" {
		t.Fatalf("exactly at the limit shouldn't count, largest should be first: %+v", occ)
	}
	if occ[0].USD != nil || occ[0].Tokens != nil || occ[0].Conf != "Estimated" {
		t.Fatalf("spikes should be Estimated and unpriced: %+v", occ[0])
	}
}

func TestRejectedWorkShowsPromptCostAsSpentNotAvoidable(t *testing.T) {
	d := fixture([]toolRow{
		{ID: "t1", Name: "Bash", Decision: "reject"},
		{ID: "t2", Name: "Bash", Decision: "accept"},
		{ID: "t3", Name: "Bash", Decision: "reject"},
	}, []reqRow{{PromptID: "p1", SessionID: "s", CostUSD: 2.5}}, nil)
	occ := detectRejectedWork(d)
	if len(occ) != 1 || occ[0].Area != "perm" || occ[0].Count != 2 || occ[0].PromptID != "p1" {
		t.Fatalf("want one perm occurrence for the prompt: %+v", occ)
	}
	if occ[0].USD != nil || occ[0].Spent == nil || *occ[0].Spent != 2.5 {
		t.Fatalf("prompt cost should be Spent, not USD: %+v", occ[0])
	}
}

func TestOversizedModelCoversSonnetUnderItsOwnSubject(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "a", SessionID: "s", Model: "claude-opus-5", QuerySource: "repl_main_thread", Output: 100, CostUSD: 1},
		{PromptID: "b", SessionID: "s", Model: "claude-sonnet-5", QuerySource: "repl_main_thread", Output: 100, CostUSD: 0.5},
		{PromptID: "c", SessionID: "s", Model: "claude-haiku-5-5", QuerySource: "repl_main_thread", Output: 100, CostUSD: 0.1},
	}, nil)
	occ := detectOversizedModel(d)
	if len(occ) != 2 {
		t.Fatalf("want Opus and Sonnet flagged, not Haiku: %+v", occ)
	}
	if occ[0].Subject != "" || occ[1].Subject != "sonnet" || !strings.Contains(occ[1].Note, "Haiku") {
		t.Fatalf("unexpected subjects or note: %+v", occ)
	}
	if occ[1].Evidence[0] != "u:b" {
		t.Fatalf("evidence should match the prompt's thread row: %v", occ[1].Evidence)
	}
}

func TestCompactionAndBloatCarryCostOverLimit(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, QuerySource: "repl_main_thread", Context: 100_000, CostUSD: 1},
		{PromptID: "p2", SessionID: "s", Ts: 2, QuerySource: "repl_main_thread", Context: 300_000, CostUSD: 2},
		{PromptID: "p2", SessionID: "s", Ts: 3, QuerySource: "repl_main_thread", Context: 150_000, CostUSD: 3},
	}, nil)
	d.compactions = []compactRow{{ID: 1, SessionID: "s", Ts: 4, Pre: 300_000, Post: 20_000}}
	for name, occ := range map[string][]occurrence{"compaction": detectCompaction(d), "bloat": detectContextBloat(d)} {
		if len(occ) != 1 || occ[0].Spent == nil || *occ[0].Spent != 5 || occ[0].USD != nil {
			t.Fatalf("%s: want $5 of the over-limit prompt as Spent only: %+v", name, occ)
		}
	}
}

func TestFixFirstItemsRanksHabitsByImpact(t *testing.T) {
	spent, usd := 20.0, 0.01
	habit := &insightItem{Key: "habit", Conf: "Measured", Occurrences: 1, SpentUSD: &spent}
	small := &insightItem{Key: "small", Conf: "Measured", Occurrences: 1, Fix: "x", USD: &usd}
	dead := &insightItem{Key: "dead", Conf: "Measured", Occurrences: 1, Fix: "x"}
	unpriced := &insightItem{Key: "unpriced", Conf: "Measured", Occurrences: 1}
	for _, it := range []*insightItem{habit, small, dead, unpriced} {
		it.Score = priorityScore(it)
	}
	got := fixFirstItems([]*insightItem{unpriced, dead, small, habit})
	if len(got) != 3 || got[0] != habit || got[1] != small || got[2] != dead {
		t.Fatalf("want habit, small, dead; got %s %s %s", got[0].Key, got[1].Key, got[2].Key)
	}
}

func TestProjectNotesNeedsThreeSessionsAndSkipsOtherFiles(t *testing.T) {
	d := &insightData{sessions: map[string]sessionRow{
		"a": {ID: "a", ProjectDir: "/work/repo"},
		"b": {ID: "b", ProjectDir: "/work/repo"},
		"c": {ID: "c", ProjectDir: "/work/repo"},
	}}
	add := func(sid, file string) {
		d.tools = append(d.tools, toolRow{ID: sid + file, SessionID: sid, Name: "Read", File: file, ResultBytes: 4000, Ts: int64(len(d.tools) + 1)})
	}
	for _, sid := range []string{"a", "b", "c"} {
		add(sid, "/work/repo/main.go")
		add(sid, "/work/repo/CLAUDE.md")
		add(sid, "/elsewhere/notes.md")
	}
	add("a", "/work/repo/once.go")

	occ := detectProjectNotes(d)
	if len(occ) != 1 || occ[0].SessionID != "" || occ[0].Count != 1 || occ[0].USD != nil {
		t.Fatalf("want one unpriced project-level occurrence: %+v", occ)
	}
	if occ[0].Fix != "## Key files\n- main.go: <what it does>\n" {
		t.Fatalf("snippet: %q", occ[0].Fix)
	}
	if !strings.Contains(occ[0].Detail, "2K tokens") {
		t.Fatalf("should count reads after the first session: %q", occ[0].Detail)
	}
	it := aggregateItems(occ, 0)[0]
	if it.Sessions != 0 || it.Scope != "Project repo" {
		t.Fatalf("item should carry the project scope: %+v", it)
	}
}

func TestSettingsPromptNeedsACommandAndNoBilling(t *testing.T) {
	billed := map[string]float64{"billed": 0.5}
	for _, tc := range []struct {
		id, text string
		want     bool
	}{
		{"x", "/model opus", true},
		{"x", " /clear ", true},
		{"x", "/modelling is hard", false},
		{"x", "/code-review high", false},
		{"x", "fix the bug", false},
		{"billed", "/model opus", false},
	} {
		if got := isSettingsPrompt(promptRow{PromptID: tc.id, Text: tc.text}, billed); got != tc.want {
			t.Errorf("%s %q = %v, want %v", tc.id, tc.text, got, tc.want)
		}
	}
}

func TestReceiptSplitsModelsAndBackgroundSpend(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "p", SessionID: "s", Model: "claude-opus-5", QuerySource: "repl_main_thread", CostUSD: 2},
		{PromptID: "p", SessionID: "s", Model: "claude-haiku-5-5", QuerySource: "away_summary", CostUSD: 0.25},
		{PromptID: "p", SessionID: "s", Model: "claude-haiku-5-5", QuerySource: "agent:builtin:Explore", CostUSD: 0.5},
	}, nil)
	r := buildReceipt(d, nil, "s")
	if r["background_usd"] != 0.25 {
		t.Fatalf("only Claude Code's own requests count as background: %v", r["background_usd"])
	}
	models := r["by_model"].([]map[string]any)
	if len(models) != 2 || models[0]["model"] != "claude-opus-5" || models[1]["cost_usd"] != 0.75 {
		t.Fatalf("by_model: %v", models)
	}
}
