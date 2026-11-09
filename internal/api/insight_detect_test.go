package api

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
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
		tools[i].Category = toolCategory("", tools[i].Name)
		d.tools = append(d.tools, tools[i])
	}
	for i := range reqs {
		reqs[i].Context = reqs[i].Input + reqs[i].CacheRead + reqs[i].CacheCreate // as loadInsightData does
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
		if got := isSafePattern(toolRow{Name: "Bash", Category: agent.ToolShell, Command: cmd}); got != want {
			t.Errorf("isSafePattern(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestOversizedModelFiresOnSmallOpusTurn(t *testing.T) {
	d := fixture([]toolRow{read("r1", "/repo/a.md", 100, 0, 0)}, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "claude-opus-5-5", QuerySource: "repl_main_thread", Output: 400, CostUSD: 0.8},
	}, nil)
	occ := detectOversizedModel(d)
	// Output only: Sonnet 5.5's output rate is half Opus 5.5's.
	if len(occ) != 1 || occ[0].USD == nil || !near(*occ[0].USD, 0.4) {
		t.Fatalf("small Opus turn should fire with the saving on Sonnet, got %+v", occ)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestOversizedModelSavesLittleOnCacheReads(t *testing.T) {
	// Cache reads cost the same on Opus 5.5 and Sonnet 5.5, so a turn that is
	// almost all cache reads saves almost nothing.
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "claude-opus-5-5", QuerySource: "repl_main_thread", CacheRead: 1_000_000, Output: 100, CostUSD: 0.202},
	}, nil)
	occ := detectOversizedModel(d)
	if len(occ) != 1 || occ[0].USD == nil {
		t.Fatalf("want one priced occurrence, got %+v", occ)
	}
	// Opus: 0.20 + 0.002 = 0.202. Sonnet: 0.20 + 0.001 = 0.201.
	if want := 0.202 * (1 - 0.201/0.202); !near(*occ[0].USD, want) {
		t.Errorf("saving = %v, want %v", *occ[0].USD, want)
	}
}

func TestOversizedModelUnknownPriceHasNoDollars(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "claude-opus-4-1", QuerySource: "repl_main_thread", Output: 100, CostUSD: 0.5},
	}, nil)
	occ := detectOversizedModel(d)
	if len(occ) != 1 || occ[0].USD != nil || occ[0].Tokens != nil {
		t.Fatalf("an unpriced model should still fire, with no dollar figure: %+v", occ)
	}
}

func TestContextBloatPricesOnlyTheExcess(t *testing.T) {
	// 300K of cache reads on Opus 5.5 and a big answer (so it isn't a small
	// turn). List cost: 300K × 0.20 + 5K × 20 = 0.06 + 0.10 = 0.16. Excess
	// 200K × 0.20 = 0.04, a quarter of it, so a quarter of the actual cost.
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 5, Model: "claude-opus-5-5", QuerySource: "repl_main_thread", CacheRead: 300_000, Output: 5000, CostUSD: 0.32},
		{PromptID: "p1", SessionID: "s", Ts: 6, Model: "claude-opus-5-5", QuerySource: "repl_main_thread", CacheRead: 90_000, Output: 5000, CostUSD: 0.2},
		{PromptID: "p2", SessionID: "s", Ts: 7, Model: "claude-opus-5-5", QuerySource: "compact", CacheRead: 300_000, CostUSD: 0.06},
	}, nil)
	occ := detectContextBloat(d)
	if len(occ) != 1 {
		t.Fatalf("want one occurrence for the session, got %+v", occ)
	}
	o := occ[0]
	if o.USD == nil || !near(*o.USD, 0.08) {
		t.Errorf("USD = %v, want 0.08", o.USD)
	}
	if o.Count != 1 || o.Severity != "full" || o.Ts != 5 || len(o.Evidence) != 1 || o.Evidence[0] != "p:p1" {
		t.Errorf("unexpected occurrence %+v", o)
	}
}

func TestContextBloatSkipsSmallTurns(t *testing.T) {
	// A small Opus turn is already counted by the model check.
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "claude-opus-5-5", QuerySource: "repl_main_thread", CacheRead: 300_000, Output: 100, CostUSD: 0.06},
	}, nil)
	if occ := detectContextBloat(d); len(occ) != 0 {
		t.Fatalf("a small turn shouldn't count twice: %+v", occ)
	}
}

func TestContextBloatFallsBackToBlendedRate(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "some-model", QuerySource: "repl_main_thread", CacheRead: 150_000, Output: 5000, CostUSD: 1},
	}, nil)
	d.blendedRate = 0.000001
	occ := detectContextBloat(d)
	if len(occ) != 1 || occ[0].USD == nil || !near(*occ[0].USD, 0.05) || occ[0].Severity != "half" {
		t.Fatalf("want 50K excess at the blended rate, got %+v", occ)
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

func TestDetectors_HaveUniqueKinds(t *testing.T) {
	seen := map[string]bool{}
	for _, det := range detectors {
		if det.kind == "" || det.run == nil {
			t.Errorf("incomplete detector: %+v", det)
		}
		if seen[det.kind] {
			t.Errorf("two detectors use the kind %q", det.kind)
		}
		seen[det.kind] = true
	}
}

// Another agent's tools have other names. Only their category should matter.
func TestDetectors_WorkFromCategoriesNotNames(t *testing.T) {
	tool := func(id, name string, c agent.ToolCategory, file, cmd string) toolRow {
		return toolRow{ID: id, SessionID: "s", PromptID: "p1", Ts: 1, Name: name, Category: c, File: file, Command: cmd, ResultBytes: 4000}
	}
	d := &insightData{toolByID: map[string]toolRow{}, waits: map[string]waitRow{}, tools: []toolRow{
		tool("1", "open_file", agent.ToolRead, "/w/a.go", ""),
		tool("2", "open_file", agent.ToolRead, "/w/a.go", ""),
		tool("3", "exec_command", agent.ToolShell, "", "go test ./..."),
		tool("4", "exec_command", agent.ToolShell, "", "go test ./..."),
	}}
	if got := detectReRead(d); len(got) != 1 {
		t.Errorf("re-reads = %d, want 1", len(got))
	}
	if got := detectRepeatCommand(d); len(got) != 1 {
		t.Errorf("repeat commands = %d, want 1", len(got))
	}

	d.tools = append(d.tools[:1:1], tool("5", "apply_patch", agent.ToolEdit, "/w/a.go", ""), d.tools[1])
	if got := detectReRead(d); len(got) != 0 {
		t.Errorf("an edit under another name should still count as a change: %d re-reads", len(got))
	}
}

func TestToolCategory_UnknownAgentHasNoCategories(t *testing.T) {
	if got := toolCategory("no-such-agent", "Read"); got != agent.ToolOther {
		t.Errorf("category = %q", got)
	}
	if got := toolCategory("claude-code", "Read"); got != agent.ToolRead {
		t.Errorf("category = %q", got)
	}
}

// limitedAgent has commands but no file paths, approval times or dollar figures.
type limitedAgent struct{ agent.Agent }

func (limitedAgent) Name() string                   { return "Limited Agent" }
func (limitedAgent) Capabilities() agent.Capability { return agent.CapCommands }
func (limitedAgent) CostSource() agent.CostSource   { return agent.CostNone }
func (limitedAgent) PremiumModel(m string) bool     { return m == "big-model" }

func withLimitedAgent(t *testing.T) {
	t.Helper()
	old := findAgent
	findAgent = func(id agent.ID) (agent.Agent, bool) {
		if id == "limited" {
			return limitedAgent{}, true
		}
		return old(id)
	}
	t.Cleanup(func() { findAgent = old })
}

func twoAgentData() *insightData {
	shell := func(id, sid string) toolRow {
		return toolRow{ID: id, SessionID: sid, PromptID: "p-" + sid, Ts: 1, Name: "x", Category: agent.ToolShell, Command: "go test", ResultBytes: 400}
	}
	read := func(id, sid string) toolRow {
		return toolRow{ID: id, SessionID: sid, PromptID: "p-" + sid, Ts: 1, Name: "x", Category: agent.ToolRead, File: "/a.go", ResultBytes: 400}
	}
	return &insightData{
		sessions: map[string]sessionRow{"claude": {ID: "claude", Agent: "claude-code"}, "other": {ID: "other", Agent: "limited"}},
		toolByID: map[string]toolRow{},
		waits:    map[string]waitRow{},
		tools: []toolRow{
			read("r1", "claude"), read("r2", "claude"), read("r3", "other"), read("r4", "other"),
			shell("c1", "other"), shell("c2", "other"),
		},
		reqs: []reqRow{
			{PromptID: "p-claude", SessionID: "claude", QuerySource: agent.MainQuerySource, Model: "claude-opus-5-5", Output: 10},
			{PromptID: "p-other", SessionID: "other", QuerySource: agent.MainQuerySource, Model: "big-model", Output: 10},
		},
		compactions: []compactRow{{SessionID: "claude"}, {SessionID: "other"}},
	}
}

func TestWithCapability_KeepsOnlySessionsThatQualify(t *testing.T) {
	withLimitedAgent(t)
	d := twoAgentData()
	view := d.withCapability(agent.CapFilePaths)
	if len(view.tools) != 2 {
		t.Fatalf("tools = %d: only the Claude session has file paths", len(view.tools))
	}
	if got := detectReRead(view); len(got) != 1 || got[0].SessionID != "claude" {
		t.Errorf("re-reads = %+v, want only Claude's", got)
	}
	if len(view.compactions) != 1 || view.compactions[0].SessionID != "claude" {
		t.Errorf("compactions = %+v", view.compactions)
	}
	if len(view.reqs) != 1 {
		t.Errorf("reqs = %+v", view.reqs)
	}
	if got := detectRepeatCommand(d.withCapability(agent.CapCommands)); len(got) != 1 {
		t.Errorf("a check the agent can support should still run: %d", len(got))
	}
	if len(d.tools) != 6 {
		t.Errorf("the original data changed: %d tools", len(d.tools))
	}
}

// With every session able to supply it, a check gets the very same data, so
// Claude Code's findings can't change.
func TestWithCapability_ChangesNothingForClaudeCode(t *testing.T) {
	d := &insightData{sessions: map[string]sessionRow{"a": {Agent: "claude-code"}, "b": {}}}
	a, _ := agentFor("claude-code")
	if got := d.withCapability(a.Capabilities()); got != d {
		t.Error("should return d itself")
	}
}

func TestWithCapability_UnknownAgentSupportsNothing(t *testing.T) {
	d := &insightData{sessions: map[string]sessionRow{"a": {Agent: "no-such-agent"}}}
	if d.capable("a", agent.CapCommands) {
		t.Error("an unknown agent shouldn't qualify")
	}
	if !d.capable("a", 0) {
		t.Error("a check needing nothing applies to everyone")
	}
}

func TestOversizedModel_AsksTheSessionsAgent(t *testing.T) {
	withLimitedAgent(t)
	d := twoAgentData()
	d.tools = nil // a turn with many tools isn't a small one
	got := detectOversizedModel(d)
	if len(got) != 2 {
		t.Fatalf("occurrences = %d, want one per agent's premium model", len(got))
	}
}

func TestCostUnknown(t *testing.T) {
	withLimitedAgent(t)
	d := twoAgentData()
	if !d.costUnknown("other") {
		t.Error("an agent with no cost source has unknown costs")
	}
	if d.costUnknown("claude") || d.costUnknown("") {
		t.Error("Claude Code reports cost, and a finding with no session isn't tied to an agent")
	}
}

// The limited agent has no Advisor: it should get findings, with no fix.
func TestDetectors_AgentWithNoAdvisorStillGetsFindings(t *testing.T) {
	withLimitedAgent(t)
	d := twoAgentData()
	got := detectRepeatCommand(d)
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1", len(got))
	}
	if got[0].Fix != "" || got[0].FixWhere != "" {
		t.Errorf("fix = %+v, want none", got[0])
	}
	if text := d.advisor("other").CompactionDetail(); strings.Contains(text, "Claude") {
		t.Errorf("another agent shouldn't be told about Claude: %q", text)
	}
	if got := d.advisor("other").ShellPattern("ls"); got != "ls" {
		t.Errorf("ShellPattern = %q, want the plain prefix", got)
	}
}

// An allow rule is permanent, so a command that can write must never be called
// safe, whatever shape it comes in.
func FuzzIsSafePattern(f *testing.F) {
	for _, seed := range []string{"ls -la", "git status", "cat a | grep b", "echo hi > out", "ls 2>&1", "rm -rf /", "ls; rm x", "find . -delete", "cat $(whoami)", "`id`", "sed -i s/a/b/ f"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, cmd string) {
		if !isSafePattern(toolRow{Name: "x", Category: agent.ToolShell, Command: cmd}) {
			return
		}
		stripped := strings.TrimSpace(cmd)
		for _, r := range harmlessRedirects {
			stripped = strings.ReplaceAll(stripped, r, "")
		}
		for _, m := range unsafeBashMarkers {
			if strings.Contains(stripped, m) {
				t.Errorf("%q was called safe, but contains %q", cmd, m)
			}
		}
		for _, seg := range pipelineSplit.Split(stripped, -1) {
			if !safeBashPrefixes[bashPattern(strings.TrimSpace(seg))] {
				t.Errorf("%q was called safe, but %q isn't read-only", cmd, seg)
			}
		}
	})
}

func FuzzParseToolInput(f *testing.F) {
	for _, seed := range []string{"", "{}", "null", "[]", `{"file_path":"/a","offset":1,"limit":2}`, `{"offset":"x"}`, `{"offset":1e999}`} {
		f.Add(seed)
	}
	f.Fuzz(func(_ *testing.T, input string) {
		parseToolInput(input, &toolRow{})
	})
}

func TestTimelineLabels(t *testing.T) {
	for n, want := range map[int64]string{512: "512", 31_500: "31K", 1_250_000: "1.2M"} {
		if got := humanTokens(n); got != want {
			t.Errorf("humanTokens(%d) = %q, want %q", n, got, want)
		}
	}
	cases := []struct {
		row  toolRow
		want string
	}{
		{toolRow{Name: "Read", File: "/repo/main.go"}, "main.go"},
		{toolRow{Name: "Bash", Category: agent.ToolShell, Command: "go   test  ./..."}, "go test ./..."},
		{toolRow{Name: "mcp__gh__list", MCPServer: "gh"}, "gh"},
		{toolRow{Name: "Task"}, "Task"},
	}
	for _, c := range cases {
		if got := toolLabel(c.row); got != c.want {
			t.Errorf("toolLabel(%+v) = %q, want %q", c.row, got, c.want)
		}
	}
}

func mcpCall(id, server string, ok bool) toolRow {
	success := int64(1)
	if !ok {
		success = 0
	}
	return toolRow{ID: id, SessionID: "s", PromptID: "p1", Ts: 1, Name: "mcp__" + server + "__x", Category: agent.ToolMCP, MCPServer: server, Success: success}
}

// The rule: at least minCallsForFailingMCP calls, with failingMCPRate or more
// failing. Below either, a server is fine.
func TestFailingMCP_FiresOnlyPastBothThresholds(t *testing.T) {
	var tools []toolRow
	for i := range 5 {
		tools = append(tools, mcpCall(fmt.Sprintf("a%d", i), "flaky", i < 2))   // 3 of 5 fail
		tools = append(tools, mcpCall(fmt.Sprintf("b%d", i), "steady", i != 0)) // 1 of 5 fails: 20%
	}
	tools = append(tools, mcpCall("c1", "rare", false), mcpCall("c2", "rare", false)) // too few calls
	d := &insightData{tools: tools}

	byServer := map[string]occurrence{}
	for _, o := range detectFailingMCP(d) {
		byServer[o.Subject] = o
	}
	flaky, ok := byServer["flaky"]
	if !ok {
		t.Fatalf("flaky should be flagged: %v", byServer)
	}
	if flaky.Count != 3 || len(flaky.Evidence) != 3 || !strings.Contains(flaky.Title, "failed 3 of 5") {
		t.Errorf("flaky = %+v", flaky)
	}
	if _, ok := byServer["steady"]; !ok {
		t.Error("exactly 20% failing should be flagged")
	}
	if _, ok := byServer["rare"]; ok {
		t.Error("two calls are too few to judge")
	}
}

func TestApprovalPattern_ForEachKindOfTool(t *testing.T) {
	d := &insightData{sessions: map[string]sessionRow{"s": {Agent: "claude-code"}}}
	cases := []struct {
		row  toolRow
		want string
	}{
		{toolRow{SessionID: "s", Name: "Bash", Category: agent.ToolShell, Command: "git status"}, "Bash(git status:*)"},
		{toolRow{SessionID: "s", Name: "query", Category: agent.ToolMCP, MCPServer: "db"}, "db · query"},
		{toolRow{SessionID: "s", Name: "Read", Category: agent.ToolRead}, "Read"},
	}
	for _, c := range cases {
		if got := d.approvalPattern(c.row); got != c.want {
			t.Errorf("approvalPattern(%+v) = %q, want %q", c.row, got, c.want)
		}
	}
}

func TestIsSafePattern_NonShellTools(t *testing.T) {
	for cat, want := range map[agent.ToolCategory]bool{
		agent.ToolRead: true, agent.ToolSearch: true,
		agent.ToolEdit: false, agent.ToolMCP: false, agent.ToolOther: false,
	} {
		if got := isSafePattern(toolRow{Name: "x", Category: cat}); got != want {
			t.Errorf("isSafePattern(%s) = %v, want %v", cat, got, want)
		}
	}
}

func TestCommandShapes(t *testing.T) {
	if got := bashPattern(""); got != "" {
		t.Errorf("bashPattern(empty) = %q", got)
	}
	for cmd, want := range map[string]string{
		"":                        "",
		"npm run build --watch":   "npm run",
		"cd /Users/me/My Project": "cd",
		"go test":                 "go test",
	} {
		if got := normalizeBashPrefix(cmd); got != want {
			t.Errorf("normalizeBashPrefix(%q) = %q, want %q", cmd, got, want)
		}
	}
	for tok, want := range map[string]bool{"": false, "build": true, "run-all": true, "./x": false, "--flag": false} {
		if got := isSubcommandLike(tok); got != want {
			t.Errorf("isSubcommandLike(%q) = %v, want %v", tok, got, want)
		}
	}
	if got := truncate("abcdef", 4); got != "abc…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("abc", 4); got != "abc" {
		t.Errorf("truncate of a short string = %q", got)
	}
}

func TestInstallWideItemsBelongToNoSession(t *testing.T) {
	occ := []occurrence{
		{Kind: "dead_mcp", Area: "context", Subject: "a", Severity: "half", Conf: "Measured", Title: "a", Fix: "x"},
		{Kind: "re_read", Area: "context", Subject: "f", SessionID: "s1", Severity: "half", Conf: "Measured", Title: "f"},
	}
	items := aggregateItems(occ, 0)
	got := map[string]int{}
	for _, it := range items {
		got[it.Kind] = it.Sessions
	}
	if got["dead_mcp"] != 0 {
		t.Errorf("install-wide item counted %d sessions, want 0", got["dead_mcp"])
	}
	if got["re_read"] != 1 {
		t.Errorf("session item counted %d sessions, want 1", got["re_read"])
	}
	areas := areaSummaries(items, occ)
	if areas[0].Items != 2 || areas[0].Sessions != 1 || areas[0].InstallWide != 1 {
		t.Errorf("context area = %+v, want 2 items, 1 session, 1 global", areas[0])
	}
	if areas[0].Severity != "half" {
		t.Errorf("a session problem sets the area's severity, got %q", areas[0].Severity)
	}

	// A global finding is counted, but with no session problem the area is fine.
	globalOnly := aggregateItems(occ[:1], 0)
	only := areaSummaries(globalOnly, occ[:1])[0]
	if only.Items != 1 || only.InstallWide != 1 || only.Sessions != 0 || only.Severity != "fine" {
		t.Errorf("global-only area = %+v, want 1 item, 1 global, 0 sessions, fine", only)
	}
}

// pricedAgent is limitedAgent with a price list of its own.
type pricedAgent struct {
	limitedAgent
	rates   map[string]agent.Rates
	cheaper map[string]string
}

func (a pricedAgent) CostSource() agent.CostSource { return agent.CostPerRequest }

func (a pricedAgent) ModelRates(m string) (agent.Rates, bool) {
	r, ok := a.rates[m]
	return r, ok
}

func (a pricedAgent) CheaperModel(m string) (string, bool) {
	c, ok := a.cheaper[m]
	return c, ok
}

func withPricedAgent(t *testing.T, a pricedAgent) {
	t.Helper()
	withLimitedAgent(t)
	old := findAgent
	findAgent = func(id agent.ID) (agent.Agent, bool) {
		if id == "priced" {
			return a, true
		}
		return old(id)
	}
	t.Cleanup(func() { findAgent = old })
}

func TestModelPricesNeedAnAgentThatKnowsThem(t *testing.T) {
	withPricedAgent(t, pricedAgent{
		rates:   map[string]agent.Rates{"big-model": {Input: 1}},
		cheaper: map[string]string{"big-model": "unpriced-model"},
	})
	d := &insightData{sessions: map[string]sessionRow{
		"gone":    {ID: "gone", Agent: "no-such-agent"},
		"limited": {ID: "limited", Agent: "limited"},
		"priced":  {ID: "priced", Agent: "priced"},
	}}
	for _, sid := range []string{"gone", "limited"} {
		r := reqRow{SessionID: sid, Model: "big-model"}
		if _, ok := d.rates(r); ok {
			t.Errorf("%s: rates should be unknown", sid)
		}
		if _, _, _, ok := d.cheaperModel(r); ok {
			t.Errorf("%s: cheaper model should be unknown", sid)
		}
	}
	if r, ok := d.rates(reqRow{SessionID: "priced", Model: "big-model"}); !ok || r.Input != 1 {
		t.Errorf("rates = %+v, %v", r, ok)
	}
	if _, _, _, ok := d.cheaperModel(reqRow{SessionID: "priced", Model: "small-model"}); ok {
		t.Error("a model with nothing cheaper has no suggestion")
	}
	if name, _, _, ok := d.cheaperModel(reqRow{SessionID: "priced", Model: "big-model"}); ok || name != "unpriced-model" {
		t.Errorf("a suggestion with no price can't be used: %q, %v", name, ok)
	}
}

func TestOversizedModelSkipsRequestsWithNoTokens(t *testing.T) {
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "claude-opus-5-5", QuerySource: "repl_main_thread"},
		{PromptID: "p1", SessionID: "s", Ts: 2, Model: "claude-opus-5-5", QuerySource: "repl_main_thread", Output: 100, CostUSD: 0.002},
	}, nil)
	occ := detectOversizedModel(d)
	if len(occ) != 1 || occ[0].USD == nil || !near(*occ[0].USD, 0.001) {
		t.Fatalf("an empty request should add nothing, got %+v", occ)
	}
}

func TestContextBloatWithNoPriceKeepsTokens(t *testing.T) {
	// No list price and no blended rate: the excess stays as tokens.
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "some-model", QuerySource: "repl_main_thread", CacheRead: 150_000, Output: 5000},
		{PromptID: "p1", SessionID: "s", Ts: 2, Model: "some-model", QuerySource: "repl_main_thread", CacheRead: 130_000, Output: 5000},
	}, nil)
	occ := detectContextBloat(d)
	if len(occ) != 1 || occ[0].USD != nil || occ[0].Tokens == nil || *occ[0].Tokens != 80_000 {
		t.Fatalf("want 80K excess tokens and no dollars, got %+v", occ)
	}
	if occ[0].Count != 2 || len(occ[0].Evidence) != 1 {
		t.Errorf("two requests in one prompt should give one piece of evidence: %+v", occ[0])
	}
}

func TestContextBloatWithZeroRatesUsesBlendedRate(t *testing.T) {
	withPricedAgent(t, pricedAgent{rates: map[string]agent.Rates{"free-model": {}}})
	d := fixture(nil, []reqRow{
		{PromptID: "p1", SessionID: "s", Ts: 1, Model: "free-model", QuerySource: "repl_main_thread", CacheRead: 110_000, Output: 5000},
	}, nil)
	d.sessions["s"] = sessionRow{ID: "s", Agent: "priced"}
	d.blendedRate = 0.000001
	occ := detectContextBloat(d)
	if len(occ) != 1 || occ[0].USD == nil || !near(*occ[0].USD, 0.01) {
		t.Fatalf("a zero list price can't scale a cost, so use the blended rate: %+v", occ)
	}
}
