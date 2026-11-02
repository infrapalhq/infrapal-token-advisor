package api

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/infrapalhq/infrapal-token-advisor/internal/agent"
)

// Detectors. Every detector is a pure function over insightData and uses fixed
// rules: no model call, no randomness. Each emits occurrences; occurrences are
// aggregated into items by (kind, subject) in insight_items.go.

// detector is one check. A check reads tool categories rather than tool names,
// so it works for any agent. It only sees sessions from agents that have what
// it needs, so that "no problems" never means "couldn't look".
type detector struct {
	kind  string
	needs agent.Capability
	run   func(d *insightData) []occurrence
}

// detectors lists every check. To add one, write its function and add a line.
var detectors = []detector{
	{"re_read", agent.CapFilePaths, detectReRead},
	{"compaction", agent.CapCompaction, detectCompaction},
	{"repeat_cmd", agent.CapCommands, detectRepeatCommand},
	{"repeat_approval", agent.CapApprovalWait, detectRepeatApproval},
	{"failing_mcp", 0, detectFailingMCP},
	{"oversized_model", 0, detectOversizedModel},
	{"dead_mcp", agent.CapMCPConnections, detectDeadMCP},
}

// occurrence is one instance of a detected problem in one session.
type occurrence struct {
	Kind      string
	Area      string // context | exec | perm | model
	Subject   string // groups occurrences into one item; "" for habits
	SessionID string
	PromptID  string
	Ts        int64
	Evidence  []string // row ids the UI can jump to: "t:<tool_use_id>", "e:<compaction id>", "p:<prompt id>"
	Count     int      // repetitions behind this occurrence
	Severity  string   // full | half
	Conf      string   // Measured | Likely | Estimated
	Tokens    *int64
	USD       *float64
	Minutes   *float64
	Title     string
	Detail    string
	Fix       string
	FixLang   string
	FixWhere  string // where the fix goes, for the dashboard
	Note      string
}

// Thresholds. Named so the rules read as rules.
const (
	minRereadsForFullSeverity = 2 // re-reads of one file in one prompt
	minRepeatsForFullSeverity = 2 // extra runs of one command with no change between
	minApprovalsForPattern    = 2 // hand approvals of one pattern to matter at all
	minCallsForFailingMCP     = 5
	failingMCPRate            = 0.2
	oversizedOutputTokens     = 2000 // a whole prompt whose answer fits in this is "small"
	oversizedMaxToolCalls     = 3
)

// mutatingBash matches shell commands that change files or repo state.
var mutatingBash = regexp.MustCompile(`(^|[;&|]\s*)(rm|mv|cp|touch|mkdir|tee|chmod|chown)\b|\bsed\s+-i\b|\bgit\s+(checkout|restore|commit|add|reset|rebase|merge|stash|apply|cherry-pick|switch)\b|\bterraform\s+(apply|destroy)\b|>`)

// isMutation reports whether a tool call may have changed files.
func isMutation(t toolRow) bool {
	if t.Category == agent.ToolEdit {
		return true
	}
	return t.Category == agent.ToolShell && mutatingBash.MatchString(t.Command)
}

// normalizeCommand collapses whitespace so "terraform  plan" and
// "terraform plan" are the same command.
func normalizeCommand(cmd string) string {
	return strings.Join(strings.Fields(cmd), " ")
}

// detectReRead flags a file read again inside one prompt when the earlier read
// still covers the same lines and nothing changed the file in between. The
// first read is the one that was needed; each later overlapping read counts.
// Rule-based: the "still in context" assumption is that no edit or shell
// mutation happened between the reads, and edits made outside Claude are
// invisible, so it stays Likely.
func detectReRead(d *insightData) []occurrence {
	type read struct {
		idx         int
		start, end  int64
		id          string
		resultBytes int64
	}
	type group struct {
		session, prompt, file string
		ts                    int64
		reads                 []read
		rereads               int
		tokens                int64
	}
	const whole = int64(1) << 62

	lastMutFile := map[string]int{} // file -> index of last edit to it
	lastMutAny := -1                // index of last shell mutation
	reads := map[string][]read{}    // "prompt|file" -> reads in that prompt
	groups := map[string]*group{}
	var order []string

	for i, t := range d.tools {
		if isMutation(t) && t.File != "" {
			lastMutFile[t.File] = i
		} else if isMutation(t) {
			lastMutAny = i
		}
		if t.Category != agent.ToolRead || t.File == "" {
			continue
		}
		key := t.PromptID + "|" + t.File
		start, end := t.Offset, whole
		if t.Limit > 0 {
			end = t.Offset + t.Limit
		}
		cur := read{idx: i, start: start, end: end, id: t.ID, resultBytes: t.ResultBytes}
		for _, prev := range reads[key] {
			mutatedSince := lastMutFile[t.File] > prev.idx || lastMutAny > prev.idx
			if mutatedSince {
				continue
			}
			if prev.start < cur.end && cur.start < prev.end {
				g, ok := groups[key]
				if !ok {
					g = &group{session: t.SessionID, prompt: t.PromptID, file: t.File, ts: t.Ts}
					groups[key] = g
					order = append(order, key)
					g.reads = append(g.reads, prev)
				}
				g.rereads++
				g.tokens += t.ResultBytes / 4
				g.reads = append(g.reads, cur)
				break
			}
		}
		reads[key] = append(reads[key], cur)
	}

	var out []occurrence
	for _, key := range order {
		g := groups[key]
		total := g.rereads + 1
		tokens := g.tokens
		ev := make([]string, 0, len(g.reads))
		for _, r := range g.reads {
			ev = append(ev, "t:"+r.id)
		}
		sev := "half"
		if g.rereads >= minRereadsForFullSeverity {
			sev = "full"
		}
		fix := d.advisor(g.session).ReReadFix()
		out = append(out, occurrence{
			Kind:      "re_read",
			Area:      "context",
			Subject:   g.file,
			SessionID: g.session,
			PromptID:  g.prompt,
			Ts:        g.ts,
			Evidence:  ev,
			Count:     total,
			Severity:  sev,
			Conf:      "Likely",
			Tokens:    &tokens,
			Title:     fmt.Sprintf("%s read %d× in one prompt", filepath.Base(g.file), total),
			Detail:    fmt.Sprintf("%d re-read(s) with no edit to the file between them.", g.rereads),
			Fix:       fix.Text,
			FixLang:   fix.Lang,
			FixWhere:  fix.Where,
		})
	}
	return out
}

// detectCompaction reports each compaction as a Measured context event. It is
// a habit (no config lever), so it has no fix and no tokens to price.
func detectCompaction(d *insightData) []occurrence {
	var out []occurrence
	for _, c := range d.compactions {
		sev := "full"
		out = append(out, occurrence{
			Kind:      "compaction",
			Area:      "context",
			Subject:   "",
			SessionID: c.SessionID,
			Ts:        c.Ts,
			Evidence:  []string{fmt.Sprintf("e:%d", c.ID)},
			Count:     1,
			Severity:  sev,
			Conf:      "Measured",
			Title:     fmt.Sprintf("Context compacted: ~%dK → ~%dK", c.Pre/1000, c.Post/1000),
			Detail:    d.advisor(c.SessionID).CompactionDetail(),
		})
	}
	return out
}

// detectRepeatCommand flags the same shell command run again within one prompt
// with no file change in between ("terraform plan ran again with nothing
// changed"). Labelled Likely because edits made outside Claude are invisible.
func detectRepeatCommand(d *insightData) []occurrence {
	type key struct{ prompt, cmd string }
	type group struct {
		session string
		ts      int64
		repeats int
		tokens  int64
		ev      []string
	}
	groups := map[key]*group{}
	var order []key

	seq := 0                 // count of mutations seen so far, global
	lastRun := map[key]int{} // key -> mutation seq at its last run
	firstID := map[key]string{}
	for _, t := range d.tools {
		if isMutation(t) {
			seq++
			continue
		}
		if t.Category != agent.ToolShell || t.Command == "" {
			continue
		}
		k := key{t.PromptID, normalizeCommand(t.Command)}
		prevSeq, seen := lastRun[k]
		if seen && prevSeq == seq {
			g, ok := groups[k]
			if !ok {
				g = &group{session: t.SessionID, ts: t.Ts, ev: []string{"t:" + firstID[k]}}
				groups[k] = g
				order = append(order, k)
			}
			g.repeats++
			g.tokens += t.ResultBytes / 4
			g.ev = append(g.ev, "t:"+t.ID)
		}
		if !seen {
			firstID[k] = t.ID
		}
		lastRun[k] = seq
	}

	// Edited extensions per session, for the fix wording.
	extBySession := map[string]string{}
	extCount := map[string]map[string]int{}
	for _, t := range d.tools {
		if t.Category == agent.ToolEdit && t.File != "" {
			ext := strings.TrimPrefix(filepath.Ext(t.File), ".")
			if ext == "" {
				continue
			}
			if extCount[t.SessionID] == nil {
				extCount[t.SessionID] = map[string]int{}
			}
			extCount[t.SessionID][ext]++
		}
	}
	for sid, m := range extCount {
		best, bestN := "", 0
		for e, n := range m {
			if n > bestN || (n == bestN && e < best) {
				best, bestN = e, n
			}
		}
		extBySession[sid] = best
	}

	var out []occurrence
	for _, k := range order {
		g := groups[k]
		tokens := g.tokens
		sev := "half"
		if g.repeats >= minRepeatsForFullSeverity {
			sev = "full"
		}
		fix := d.advisor(g.session).RepeatCommandFix(k.cmd, extBySession[g.session])
		out = append(out, occurrence{
			Kind:      "repeat_cmd",
			Area:      "exec",
			Subject:   k.cmd,
			SessionID: g.session,
			PromptID:  k.prompt,
			Ts:        g.ts,
			Evidence:  g.ev,
			Count:     g.repeats + 1,
			Severity:  sev,
			Conf:      "Likely",
			Tokens:    &tokens,
			Title:     fmt.Sprintf("`%s` ran again with nothing changed", truncate(k.cmd, 60)),
			Detail:    fmt.Sprintf("%d extra run(s) in one prompt with no file change between.", g.repeats),
			Fix:       fix.Text,
			FixLang:   fix.Lang,
			FixWhere:  fix.Where,
		})
	}
	return out
}

// Read-only command prefixes that are safe to suggest for permanent allow.
// Anything not on this list is never suggested, so a fix can't widen what
// Claude may change.
var safeBashPrefixes = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true, "grep": true,
	"rg": true, "find": true, "pwd": true, "tree": true, "stat": true, "file": true,
	"du": true, "df": true, "echo": true, "tr": true, "sort": true, "uniq": true, "cut": true, "git status": true, "git log": true,
	"git diff": true, "git show": true, "git branch": true, "kubectl get": true,
	"kubectl describe": true, "terraform plan": true, "terraform show": true,
}

// harmlessRedirects are stripped before the write-marker check: they discard
// or merge output without writing a file.
var harmlessRedirects = []string{"2>&1", "2>/dev/null", ">/dev/null", "1>/dev/null"}

// unsafeBashMarkers disqualify a whole command: output redirection to a file,
// command substitution, or flags that write or execute.
var unsafeBashMarkers = []string{">", "`", "$(", "-delete", "-exec", "sed -i", "--write"}

// pipelineSplit separates the segments of a pipeline or command list. Each
// segment must be read-only on its own.
var pipelineSplit = regexp.MustCompile(`\|\||&&|\||;`)

// bashPattern is the permission pattern a Bash approval would allow. A
// read-only command reduces to its name (so "ls -la modules" and "ls -la
// modules/db" are both "ls"); a two-word read-only prefix ("git status") keeps
// both words; anything else uses the existing prefix normalisation.
func bashPattern(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) >= 2 && safeBashPrefixes[fields[0]+" "+fields[1]] {
		return fields[0] + " " + fields[1]
	}
	if safeBashPrefixes[fields[0]] {
		return fields[0]
	}
	return normalizeBashPrefix(cmd)
}

// isSafePattern reports whether a tool call's approval can be suggested for
// permanent allow. A read-only tool qualifies. A Bash command qualifies when
// no segment of it writes: every piped or chained command must itself be a
// read-only pattern, and the command must not redirect output to a file.
func isSafePattern(t toolRow) bool {
	if t.Category != agent.ToolShell {
		return t.Category == agent.ToolRead || t.Category == agent.ToolSearch
	}
	cmd := strings.TrimSpace(t.Command)
	for _, r := range harmlessRedirects {
		cmd = strings.ReplaceAll(cmd, r, "")
	}
	for _, m := range unsafeBashMarkers {
		if strings.Contains(cmd, m) {
			return false
		}
	}
	segments := pipelineSplit.Split(cmd, -1)
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" || !safeBashPrefixes[bashPattern(seg)] {
			return false
		}
	}
	return len(segments) > 0
}

// approvalPattern is the pattern an approval would allow, in the agent's terms.
func (d *insightData) approvalPattern(t toolRow) string {
	if t.Category == agent.ToolShell {
		return d.advisor(t.SessionID).ShellPattern(bashPattern(t.Command))
	}
	if t.MCPServer != "" {
		return fmt.Sprintf("%s · %s", t.MCPServer, t.Name)
	}
	return t.Name
}

// detectRepeatApproval groups hand-approved calls by the pattern they would
// allow. Two or more approvals of one pattern is a pattern. Safe patterns get
// an allow rule from the agent; unsafe ones are reported with no fix.
func detectRepeatApproval(d *insightData) []occurrence {
	type group struct {
		pattern  string
		safe     bool
		session  string
		ts       int64
		waitMs   int64
		count    int
		evidence []string
	}
	groups := map[string]*group{}
	var order []string
	for _, t := range d.tools {
		w, ok := d.waits[t.ID]
		if !ok || t.Name == "" {
			continue
		}
		pat := d.approvalPattern(t)
		g, ok := groups[pat]
		if !ok {
			g = &group{pattern: pat, safe: isSafePattern(t), session: t.SessionID, ts: t.Ts}
			groups[pat] = g
			order = append(order, pat)
		}
		g.count++
		g.waitMs += w.WaitMs
		g.evidence = append(g.evidence, "t:"+t.ID)
	}

	var out []occurrence
	for _, pat := range order {
		g := groups[pat]
		if g.count < minApprovalsForPattern {
			continue
		}
		minutes := float64(g.waitMs) / 60000
		var fix agent.Fix
		note := "Approved by hand more than once. Not suggested for permanent allow: it is not a read-only command."
		if g.safe {
			note = ""
			fix = d.advisor(g.session).AllowFix(g.pattern)
		}
		sev := "half"
		if minutes >= 1 {
			sev = "full"
		}
		out = append(out, occurrence{
			Kind:      "repeat_approval",
			Area:      "perm",
			Subject:   g.pattern,
			SessionID: g.session,
			Ts:        g.ts,
			Evidence:  g.evidence,
			Count:     g.count,
			Severity:  sev,
			Conf:      "Measured",
			Minutes:   &minutes,
			Title:     fmt.Sprintf("%s approved by hand %d×", g.pattern, g.count),
			Detail:    fmt.Sprintf("%.1f min waiting on your approval.", minutes),
			Fix:       fix.Text,
			FixLang:   fix.Lang,
			FixWhere:  fix.Where,
			Note:      note,
		})
	}
	return out
}

// detectFailingMCP flags an MCP server whose calls fail often. A habit: the
// lever is the server's credentials or config, not a settings line.
func detectFailingMCP(d *insightData) []occurrence {
	type stat struct {
		calls, fails int
		session      string
		ts           int64
		ev           []string
	}
	stats := map[string]*stat{}
	var order []string
	for _, t := range d.tools {
		if t.MCPServer == "" {
			continue
		}
		s, ok := stats[t.MCPServer]
		if !ok {
			s = &stat{session: t.SessionID, ts: t.Ts}
			stats[t.MCPServer] = s
			order = append(order, t.MCPServer)
		}
		s.calls++
		if t.Success == 0 {
			s.fails++
			s.ev = append(s.ev, "t:"+t.ID)
		}
	}
	var out []occurrence
	for _, name := range order {
		s := stats[name]
		if s.calls < minCallsForFailingMCP || float64(s.fails)/float64(s.calls) < failingMCPRate {
			continue
		}
		out = append(out, occurrence{
			Kind:      "failing_mcp",
			Area:      "exec",
			Subject:   name,
			SessionID: s.session,
			Ts:        s.ts,
			Evidence:  s.ev,
			Count:     s.fails,
			Severity:  "half",
			Conf:      "Measured",
			Title:     fmt.Sprintf("%s failed %d of %d calls", name, s.fails, s.calls),
			Detail:    "Check the server's credentials or configuration.",
		})
	}
	return out
}

// detectOversizedModel flags a prompt answered on a premium model whose
// answer was small and which used no file changes and few tools: the kind of
// turn a smaller model would likely handle. A habit, Likely.
func detectOversizedModel(d *insightData) []occurrence {
	type agg struct {
		session, model string
		ts             int64
		output         int64
		cost           float64
		tools          int
		mutated        bool
	}
	byPrompt := map[string]*agg{}
	var order []string
	for _, r := range d.reqs {
		if r.PromptID == "" || r.QuerySource != agent.MainQuerySource || !d.premiumModel(r) {
			continue
		}
		a, ok := byPrompt[r.PromptID]
		if !ok {
			a = &agg{session: r.SessionID, model: r.Model, ts: r.Ts}
			byPrompt[r.PromptID] = a
			order = append(order, r.PromptID)
		}
		a.output += r.Output
		a.cost += r.CostUSD
	}
	for _, t := range d.tools {
		a, ok := byPrompt[t.PromptID]
		if !ok {
			continue
		}
		a.tools++
		if isMutation(t) {
			a.mutated = true
		}
	}
	var out []occurrence
	for _, pid := range order {
		a := byPrompt[pid]
		if a.output >= oversizedOutputTokens || a.mutated || a.tools > oversizedMaxToolCalls {
			continue
		}
		usd := a.cost
		out = append(out, occurrence{
			Kind:      "oversized_model",
			Area:      "model",
			Subject:   "",
			SessionID: a.session,
			PromptID:  pid,
			Ts:        a.ts,
			Evidence:  []string{"p:" + pid},
			Count:     1,
			Severity:  "half",
			Conf:      "Likely",
			USD:       &usd,
			Title:     fmt.Sprintf("Small turn on %s", a.model),
			Detail:    fmt.Sprintf("Answer of ~%d output tokens with %d tool call(s) and no file changes. Spent $%.2f on this turn.", a.output, a.tools, a.cost),
			Note:      d.advisor(a.session).SmallerModelNote(),
		})
	}
	return out
}

// detectDeadMCP reports connected MCP servers that never had a tool called.
// Install-wide (the inventory view is all-time), so it has no session.
func detectDeadMCP(d *insightData) []occurrence {
	var out []occurrence
	for _, name := range d.deadMCP {
		fix := d.advisor("").RemoveMCPFix(name)
		out = append(out, occurrence{
			Kind:     "dead_mcp",
			Area:     "context",
			Subject:  name,
			Count:    1,
			Severity: "half",
			Conf:     "Measured",
			Title:    name + " connected but never used",
			Detail:   "Its tool definitions ride along on every request.",
			Fix:      fix.Text,
			FixLang:  fix.Lang,
			FixWhere: fix.Where,
		})
	}
	return out
}

// truncate shortens s to n runes with an ellipsis.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// sortOccurrences orders occurrences by time, then kind, for stable output.
func sortOccurrences(occ []occurrence) {
	sort.SliceStable(occ, func(i, j int) bool {
		if occ[i].Ts != occ[j].Ts {
			return occ[i].Ts < occ[j].Ts
		}
		return occ[i].Kind < occ[j].Kind
	})
}

// normalizeBashPrefix reduces a full Bash command to its first two
// whitespace-separated tokens (e.g. "npm run build --watch" -> "npm run"),
// a heuristic stand-in for a stable command "shape" — not a validated
// Claude Code permission-rule pattern. The second token is only kept when it
// looks like a stable subcommand name (isSubcommandLike) — otherwise (e.g.
// "cd /Users/me/My Project", where the second token is an
// unbounded, possibly space-escaped path) it's dropped, since including a
// one-off path in the suggested rule would make it neither a sensible
// generalization nor a valid copy-pasteable pattern.
func normalizeBashPrefix(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return ""
	}
	if len(fields) > 1 && isSubcommandLike(fields[1]) {
		return fields[0] + " " + fields[1]
	}
	return fields[0]
}

// isSubcommandLike reports whether tok looks like a stable subcommand name
// (letters, digits, '-', '_' only) rather than a path, flag, or other
// unbounded argument.
func isSubcommandLike(tok string) bool {
	if tok == "" {
		return false
	}
	for _, r := range tok {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' {
			return false
		}
	}
	return true
}
