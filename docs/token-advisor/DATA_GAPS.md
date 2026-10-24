# Token Advisor UI: data gaps and workarounds

Companion to [SPEC.md](./SPEC.md). Each entry is a UI element whose data
ccotel cannot supply exactly as the mock shows it. Each one gives what's
missing, the evidence (checked against the live `~/.ccotel/ccotel.db` on
2026-10-24), the workaround the spec uses, and how the UI should label it.

Severity:
- 🔴 **missing**: no source today; the workaround adds new data or storage.
- 🟠 **approximate**: derivable, but the number is a heuristic or estimate.
- 🟡 **caveat**: derivable, but naive use gives wrong numbers.

| # | Sev | UI element | Short |
|---|---|---|---|
| G1 | 🔴 | Project name everywhere | no cwd in telemetry |
| G2 | 🟠 | Per-tool token sizes ("31K tok") | result_tokens never populated |
| G3 | 🟠 | Context-per-step bars on tool rows | tool calls have no request_id |
| G4 | 🟡 | Compaction "~180K before" | data exists, not projected |
| G5 | 🟡 | "Waiting on you" times | auto-approved calls have wait ms; huge outliers |
| G6 | 🔴 | "Read-only and on the safe list" | current fix builder has no safe list |
| G7 | 🔴 | Saved so far, Measuring, before → after | no fix history or baseline stored |
| G8 | 🔴 | Per-session issues, per-pattern fixes | findings are one per kind, all-time |
| G9 | 🟠 | "Since install", "Week 1" baselines | no install date stored |
| G10 | 🔴 | NEW since last visit | no visit tracking |
| G11 | 🟠 | "Prompt 3 was a separate task" | semantic judgment |
| G12 | 🟠 | Model–task mismatch | no ground truth; lever for background calls unverified |
| G13 | 🟠 | "terraform plan ran with nothing changed" (Measured in mock) | external edits invisible |
| G14 | 🟡 | Re-read "3 times" | offset/limit reads aren't re-reads |
| G15 | 🟡 | Thread "You"/"Claude" text | only when content logging is on |
| G16 | 🔴 | Run doctor, Export, collector live | CLI only, no API |
| G17 | 🟠 | ≈$ for token waste | no price table; waste is really carried context |

---

### G1 🔴 Project name
**Missing**: OTel resource attrs carry only host/os/service. There is no cwd,
repo or branch, and `events_raw` has no cwd key anywhere.
**Evidence**: `json_each(sessions.resource_attrs)` gives host.arch, os.type,
os.version, service.name, service.version only.
**Workaround** (SPEC §4.2): the cwd field of the session's transcript
`~/.claude/projects/*/<id>.jsonl` → nearest `.git` above the session's file
paths → common path prefix → `(unknown)`. Persist the result in
`sessions.project_dir` at ingest time.
**Caveat**: only **6 of 18** sessions still have a transcript, because Claude
Code's `retention_sweep` deletes them after 30 days. That's why it must be
captured at ingest. Older sessions with no file tool calls show `(unknown)`.
**Better fix (needs a decision)**: `ccotel init` installs a `SessionStart`
hook that sends `{session_id, cwd}` to ccotel. That's exact, but it's one
more settings.json edit.
**Label**: no label when the source is the transcript or a hook. With the
git or prefix fallback, a muted "inferred" tooltip.

### G2 🟠 Per-tool token size
**Missing**: `tool_calls.result_tokens` is 0/1793 populated.
**Workaround**: `result_size_bytes / 4` (1734/1793 populated), the same as the
existing `fatPayloadFinding`.
**Label**: Estimated (`≈31K`).

### G3 🟠 Context-per-step on tool rows
**Missing**: tool spans and events carry no `request_id`, so a tool result
can't be tied to the API request that consumed it.
**Workaround**: assign each tool row the context of the first API request
after its timestamp (SPEC §4.3). Text and prompt rows get the latest request
at or before them. API-request-level context is exact.
**Label**: chart hint "approximate per step". Peak and compaction figures are
exact.

### G4 🟡 Compaction
**Have**: a `compaction` event with `pre_tokens=223912`,
`post_tokens=24940`, `trigger=auto`. That's better than the mock's "likely".
**Gap**: it lands only in `events_raw`. There's no typed table, and
`v_session_summary` counts `query_source='compact'` instead.
**Workaround**: migration 0013 plus an ingest case and backfill.
**Label**: Measured. Change the mock's "Compacted… (likely)" to plain
"Compacted".

### G5 🟡 Waiting on you
**Caveat 1**: auto-approved calls (`decision_source=config`) average 461 ms of
`blocked_on_user_ms`, and 335 of them are over 1 s. Summing all of it
inflates "waiting".
**Caveat 2**: `user_temporary` max is **81,647,735 ms (22.7 h)**, from a user
who walked away.
**Workaround**: count only user decision sources, cap each wait at 10 min,
and report the remainder as "idle" (SPEC §4.4).
**Label**: Measured, with "capped at 10 min per approval" in a tooltip.

### G6 🔴 "Read-only and on the safe list — we never suggest allowing writes"
**Missing**: the existing `buildAllowRuleFix` suggests **every** hand-approved
Bash prefix, writes included. The mock's promise doesn't hold today.
**Have**: the `bash_command_class` span attr (file_read, file_search,
shell_builtin, text_transform, vcs, fs_mutation, …), but only on **243 of 956**
Bash calls (newer Claude Code versions).
**Workaround**: suggest a pattern only if (a) its class is in {file_read,
file_search, shell_builtin, text_transform}, or a `vcs` read subcommand (git
status/log/diff/show), **and** (b) its prefix is on a static allowlist (`ls`,
`cat`, `head`, `tail`, `wc`, `grep`, `rg`, `find`, `git status`, `git log`,
`git diff`, `kubectl get`, `kubectl describe`, `terraform plan`, …).
Unclassified commands not on the allowlist are never suggested; they show as
"approved N× — review manually" with no copy button.

### G7 🔴 Fix tracking: Saved so far, Measuring, before → after
**Missing**: `finding_state` stores only the latest state and `updated_at`.
There's no history, no baseline metric and no measuring window, so "Saved
$0.90", "Input per request 24K → 19K" and "Waiting/week 14m → 1m" can't be
computed.
**Mock inconsistency**: Session page says "Measuring your next 5 sessions"
but Fixes says "Measuring · day 5 of 7".
**Workaround**: append-only `fix_marks` with `baseline_json` captured at mark
time, and a window of max(5 sessions, 7 days) (SPEC §8).
**Label**: savings are Estimated (counterfactual). Before → after values are
Measured.
**Note**: fixes marked before this ships have no baseline. Show "marked
before measuring existed".

### G8 🔴 Finding granularity
**Missing**: `computeFindings` returns at most one finding per kind,
all-time, with a fixed ID (`permission_friction`). The mock needs per-session
issues, per-pattern fixes ("Allow `ls`" separate from "Allow `kubectl get`"),
per-day avoidable $ and weekly history.
**Workaround**: per-session detector occurrences keyed by `(kind, subject)`
(SPEC §5). Existing `finding_state` rows map onto the new IDs by kind; any
pattern under an old kind-level "fixed" is treated as marked on that date.

### G9 🟠 Install date and baselines
**Missing**: no `installed_at` stored.
**Workaround**: `ui_state.install_at` = `min(sessions.started_at)` backfill,
and set by `init` from then on. The baseline is the first 14 days after it.
If there are fewer than 4 weeks of history, hide "down X% since you started"
and show the absolute numbers only.

### G10 🔴 NEW since last visit
**Missing**: no record of dashboard visits.
**Workaround**: `POST /api/ui/visit` on Sessions page load, stored in
`ui_state.last_visit_at`. That's server-side, so it's consistent across
browsers.

### G11 🟠 Mixed tasks ("Prompt 3 was a separate task")
**Missing**: this is a semantic judgment; no field encodes "task".
**Static**: file-set Jaccard < 0.1 between a prompt and all earlier prompts,
with session context > 100K. It's weak for prompts with no file tools, which
get no verdict.
**LLM** (key + prompt-text consent): same-task classification over the
prompt texts.
**Label**: Likely. With AI: "Likely · AI-checked".

### G12 🟠 Model–task mismatch
**(a) Per session**: there's no ground truth for "a smaller model would have
done". Static heuristic M2 (Opus, small output, no edits, few tools) is
Likely. LLM optional.
**(b) "Sonnet writing session titles"**: the live data contradicts the mock.
`generate_session_title` already runs on **Haiku**. The non-Haiku background
calls are `away_summary`, `agent_summary` and `prompt_suggestion` on Sonnet
or Opus. ccotel's own `0011` migration notes that routing for these isn't
user-configurable.
**Workaround**: show it as a stat in the Model area, with **no fix card**,
until a real setting is confirmed. That means the mock's "Smaller model for
background calls" fix and its "smaller bg model" annotation can't be backed
today. Open decision in SPEC §10.

### G13 🟠 "Ran again with nothing changed" labeled Measured
**Caveat**: ccotel sees only Claude's own Edit/Write/`fs_mutation` calls. A
user edit in their editor between two `terraform plan` runs is invisible.
**Workaround**: the rule is "no Claude-side change in between".
**Label**: Likely, not Measured as in the mock.

### G14 🟡 Re-reads
**Caveat**: `tool_input` shows paginated reads (`offset`, `limit`) of the same
file, which aren't waste.
**Workaround**: count a re-read only when line ranges overlap and no change
to the file happened in between. Treat a missing offset/limit as the whole
file.

### G15 🟡 Thread text
**Have**: prompt_text 108/108 and response_text 631/631 on this install.
**Caveat**: both depend on Claude Code content logging
(`OTEL_LOG_USER_PROMPTS`, etc.). Without it, the rows render as
"[prompt not logged]" and the sidebar shows "Prompt text: not logged".
Session titles still work because `generate_session_title` responses are
there whenever responses are logged. Otherwise the title falls back to
"Session · <date>".

### G16 🔴 Run doctor, Export, collector live
**Missing**: these exist only as CLI commands (`ccotel doctor`, `ccotel
export`, `ccotel status`).
**Workaround**: thin API wrappers that reuse the CLI packages (SPEC §8).

### G17 🟠 Dollar value of token waste
**Missing**: no per-model price table in ccotel.
**Workaround**: use the install's own blended $/token per model. The real cost
of a re-read is its tokens carried as cache reads in every later request of
the session, so the simple "tokens × rate" understates it. The spec keeps the
simple form for consistency.
**Label**: Estimated (`≈`).

---

### Verified OK (no gap)
Spend, tokens, cache share, per-prompt cost, model mix, token-kind split,
call counts, active time, session titles (from Claude Code's own
`generate_session_title`), hand-approval counts per command, rejections,
MCP inventory and error rates, retries. Cost from `api_requests` matches the
`cost.usage` metric exactly ($177.42 both).
