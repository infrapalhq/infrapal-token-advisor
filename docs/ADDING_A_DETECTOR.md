# Adding a detector

A detector is one check, such as "the same file was read twice with nothing
changed". It's a function and one line in a list, and it works for every agent.

Good detectors find something a person can act on. If you can't say what the user
should do differently, it's probably a statistic, not a detector.

## 1. Write the function

Detectors live in [`internal/api/insight_detect.go`](../internal/api/insight_detect.go).
Each takes `*insightData`, which holds the rows for the time window, and returns
occurrences. There's no database access, model call or randomness, so the same data
always gives the same result.

```go
// detectLargeReads flags a file read that returned a very large result.
func detectLargeReads(d *insightData) []occurrence {
	var out []occurrence
	for _, t := range d.tools {
		if t.Category != agent.ToolRead || t.ResultBytes < largeReadBytes {
			continue
		}
		tokens := t.ResultBytes / 4
		out = append(out, occurrence{
			Kind:      "large_read",
			Area:      "context",
			Subject:   t.File,
			SessionID: t.SessionID,
			PromptID:  t.PromptID,
			Ts:        t.Ts,
			Evidence:  []string{"t:" + t.ID},
			Count:     1,
			Severity:  "half",
			Conf:      "Estimated",
			Tokens:    &tokens,
			Title:     filepath.Base(t.File) + " returned a very large result",
			Detail:    "Reading only the lines you need keeps context small.",
		})
	}
	return out
}
```

Put thresholds in the `const` block near the top of the file, so the rules read as
rules.

**Use tool categories, never tool names.** `t.Category == agent.ToolShell` works for
every agent, and `t.Name == "Bash"` works for one. A test fails if a Claude Code tool
name or file name appears in `internal/api`.

| Field | What it's for |
| --- | --- |
| `Kind` | The detector's name |
| `Area` | `context`, `exec`, `perm` or `model`, the four problems on the Overview |
| `Subject` | Groups occurrences into one item, such as a file or a command. Empty for a habit |
| `Evidence` | Rows the dashboard can jump to: `t:<tool id>`, `p:<prompt id>` or `e:<compaction id>` |
| `Severity` | `full` means fix now, and `half` means worth a look |
| `Conf` | How sure we are. See below |
| `Tokens`, `USD`, `Minutes` | What it cost. Set only what you can honestly work out, and leave the rest nil |

Be honest with `Conf`:

| Label | Use it when |
| --- | --- |
| `Measured` | It's read directly from telemetry |
| `Likely` | A rule applied to measured data, which can be wrong in some cases |
| `Estimated` | A derived figure |

## 2. Add it to the list

Add one line to `detectors`, with the capabilities it needs:

```go
{"large_read", "large file reads", agent.CapFilePaths, detectLargeReads},
```

The second field finishes the sentence "Not checked for this agent: …". The third
is what your check reads beyond tool names and categories. A session from an agent
without it never reaches your function, and the dashboard lists your check as
skipped. Use `0` if every agent can supply the data.

| If you read | Needs |
| --- | --- |
| `t.File` | `agent.CapFilePaths` |
| `t.Command` | `agent.CapCommands` |
| `d.waits` | `agent.CapApprovalWait` |
| `d.compactions` | `agent.CapCompaction` |

## 3. Offer a fix, if there is one

A fix is something to paste in. If there's no setting to change, leave `Fix` empty
and put the advice in `Detail` or `Note`, and the dashboard shows it as a habit.

Fix text belongs to the agent, because every agent has its own config. Add a method
to `agent.Advisor` in [`internal/agent/advice.go`](../internal/agent/advice.go), a
default to `NoAdvice`, and Claude Code's version to
[`internal/agent/claudecode/advice.go`](../internal/agent/claudecode/advice.go). Then
call it through `d.advisor(sessionID)`, and copy `Text`, `Lang` and `Where` to the
occurrence's `Fix`, `FixLang` and `FixWhere`.

## 4. Keep private data private

MCP results leave the machine, so a new kind only sends its numbers there by default.
If your title or detail is safe to send, add the kind to `safeFindingTitles` in
[`internal/api/mcp.go`](../internal/api/mcp.go). If it names a file, command or
project, give it a replacement title with those left out.

## 5. Test it

Add tests to [`internal/api/insight_detect_test.go`](../internal/api/insight_detect_test.go).
`fixture` builds data from tool rows, with helpers such as `read`, `bash` and `edit`.
Test when it should fire and, just as importantly, when it shouldn't:

```go
func TestLargeReadFires(t *testing.T) {
	d := fixture([]toolRow{read("r1", "/repo/big.go", 900000, 0, 0)}, nil, nil)
	if occ := detectLargeReads(d); len(occ) != 1 {
		t.Fatalf("want 1 occurrence, got %d", len(occ))
	}
}

func TestLargeReadIgnoresSmallFiles(t *testing.T) {
	d := fixture([]toolRow{read("r1", "/repo/small.go", 100, 0, 0)}, nil, nil)
	if occ := detectLargeReads(d); len(occ) != 0 {
		t.Fatalf("want no occurrences, got %d", len(occ))
	}
}
```

```bash
go test ./internal/api/...
```

## 6. Document it

Add a row to the issue table in [ACCURACY.md](ACCURACY.md) saying what the rule is,
its label, and when it can be wrong. Users rely on that table to decide whether to
trust a finding. If it's a new kind of problem, add it to "What it finds" in the
[README](../README.md).
