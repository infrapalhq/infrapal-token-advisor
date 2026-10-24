package api

import (
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
