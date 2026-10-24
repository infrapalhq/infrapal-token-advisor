# Token Advisor UI: data plan and spec

Status: draft · Branch: `token-advisor-data-spec` · Date: 2026-10-24

Companion doc: [DATA_GAPS.md](./DATA_GAPS.md) lists every UI element whose data
ccotel cannot supply directly today, and the workaround this spec uses for it.

## 1. Goal

`~/Documents/workspace/token-advisor-ui` is a static HTML/vanilla-JS mock with
four pages (Overview, Sessions, Session, Fixes) and every number hard-coded.
This spec lays out how to:

1. back every element in the mock with real ccotel data,
2. compute as much as possible with **deterministic, static heuristics**
   (SQL views and Go detectors), with no LLM involved,
3. use the user's **Anthropic API key**, entered in the UI, only for the few
   judgments static rules can't make well, and keep a static fallback for each
   so the dashboard is complete without a key.

Out of scope: visual design changes. The mock is the design.

## 2. Principles

- **Static first.** An LLM never produces a number. It only labels, groups or
  phrases things, and only when a key is configured.
- **Every figure carries a confidence label.** The mock already shows
  `Measured` / `Likely`. Definitions:
  - `Measured`: read directly from telemetry (counts, durations, billed cost).
  - `Likely`: a heuristic over measured data that can be wrong in edge cases
    (a re-read after an external edit, for example).
  - `Estimated`: a derived price or token count (bytes→tokens, tokens→$).
  The mock's `≈` prefix maps to `Estimated`.
- **Every fix is runnable or pasteable,** which is the existing
  `computeFindings` invariant. Issues with no config lever go to **Habits**,
  not to fix cards.
- **Local by default.** With no key set, nothing leaves the machine. The
  sidebar's "Local only" badge must change when a key is active (§7.3).

## 3. What ccotel already has (verified against the live DB)

Verified on 2026-10-24 against `~/.ccotel/ccotel.db`: 18 sessions, 108
prompts, 1,771 API requests, 1,793 tool calls, ~2 months of history.

| Source | Gives the UI | Coverage |
|---|---|---|
| `sessions` | start/end, app version | 18/18 |
| `interactions` | prompt text, seq, start, duration | 108/108 have text |
| `api_requests` | model, query_source, all 4 token kinds, cost, ttft, stop_reason, attempt | cost on 1766/1771; matches `cost.usage` metric exactly ($177.42) |
| `tool_calls` | tool, decision + source, blocked ms, file_path, full_command, tool_input JSON, result_size_bytes, MCP server | file_path 540 (all Read/Edit/Write), full_command 956/956 Bash, result_tokens **0** |
| `assistant_responses` | Claude's text (thread "Claude" rows), session titles (`query_source='generate_session_title'`) | 631/631 have text |
| `spans.attrs` | `bash_command_class` (file_read, file_search, vcs, fs_mutation, …) | 243 of 956 Bash calls (only newer CC versions) |
| `mcp_server_connections` | connected servers | 91 rows |
| `metric_points` | active time (user/cli), lines of code, commits | ok |
| `events_raw` `compaction` | `pre_tokens`, `post_tokens`, `trigger` | present, **not projected** to a typed table |
| `finding_state` | dismissed/snoozed/fixed per finding id | no history, no baseline |
| `config.json` + `/api/ai/config` | Anthropic key storage, env override | exists |

## 4. Core derivations (shared by every page)

### 4.1 Session row (new view `v_session_card`)

| Field | Derivation | Label |
|---|---|---|
| `title` | latest `generate_session_title` response for the session (JSON `title`), else first prompt text truncated to 60 chars | Measured |
| `project` | see §4.2 | Likely |
| `date`, `dur` | `started_at`; duration = `active_time.total` user+cli, falling back to `ended_at - started_at` | Measured |
| `tok`, `cost` | sums over `api_requests` | Measured |
| `wait` | Σ user waits (§4.4) | Measured |
| `model` | model with the highest cost share among `repl_main_thread` requests | Measured |
| `areas[]` | per-area severity from that session's detector hits (§5) | — |
| `pr` (priority) | max area severity: any `full` → `now`, any `half` → `look`, else `fine` | — |
| `isNew` | `started_at > ui_state.last_visit_at` (§8) | — |
| `sym[]` | compacted: any C2 hit · failed: tool error rate > 10% · waits: any single wait > 30s · repeats: any C1/E1 hit | Measured |

### 4.2 Project resolution (gap G1)

OTel carries no cwd. Resolution order, persisted to a new `sessions.project_dir`
column at first resolution so it survives Claude Code's 30-day transcript
cleanup:

1. `~/.claude/projects/*/<session_id>.jsonl` exists → its `cwd` field.
2. Walk up from the session's most common `tool_calls.file_path` directory to
   the nearest `.git` and use that dir.
3. Longest common directory prefix of the session's file paths.
4. `(unknown)`.

Display name is the basename. Resolution runs in the ingest writer (cheap
filesystem stat, local only) and in a one-off backfill.

### 4.3 Thread timeline for one session (new endpoint, §6)

Merge-sort these by timestamp, grouped under each prompt divider:

- `div`: interaction (seq, start time, prompt cost)
- `you`: `interactions.prompt_text`
- `claude`: `assistant_responses.response_text` where `query_source='repl_main_thread'`
- `tool`: `tool_calls` (name; label = file_path / full_command / pattern; size = `result_size_bytes/4` tokens, Estimated)
- `wait`: tool calls with a user decision source and a counted wait (§4.4)
- `event`: compaction (from the projected compaction table)

**Context per step (`ctx`)**: for each `api_requests` row in the session,
`input + cache_read + cache_creation` tokens. Each timeline row gets the
context of the **latest API request at or before its timestamp**, and a tool
row gets the context of the **first request after it** (the request that
carries its result). This is an approximation because tool calls carry no
request_id (gap G3). The chart's prompt-width flexes are each prompt's step
count.

### 4.4 User waits (gap G5)

`blocked_on_user_ms` is non-zero even for auto-approved calls (avg 461 ms on
`decision_source='config'`). A wait counts only when `decision_source IN
('user_temporary','user_permanent','user_reject')`. Each wait is capped at
**10 min**, and the excess is reported separately as idle ("away"); the live
DB has a single 22.7 h wait. `AskUserQuestion` and `ExitPlanMode` stay
excluded (existing `blockByDesignTools`).

### 4.5 Pricing

- Tokens → $: the session's own blended rate per model
  (`Σcost/Σtokens`, existing `blendedUSDPerToken`, scoped per model).
- Bytes → tokens: `/4`, Estimated (result_tokens is never populated).
- Minutes are minutes and are never converted to $.

## 5. Detectors (all static)

Detectors become **per-session** and emit *occurrences*
`{kind, subject, session_id, prompt_id, evidence_ids[], tokens_est, usd_est,
minutes, confidence}`. Page-level findings are aggregations of occurrences by
`(kind, subject)`. This replaces today's one-finding-per-kind, all-time model
(gap G8). Severity per occurrence: `full` when it crosses the "fix now"
threshold, else `half`.

| ID | Area | Rule | Fix | Conf. |
|---|---|---|---|---|
| **C1 re_read** | Context | Same `file_path` read ≥2× in one prompt, overlapping offset/limit ranges, with no Edit/Write/`fs_mutation` Bash touching it in between. The first read is "needed". | CLAUDE.md snippet "Grep before reading large files / don't re-read" | Likely |
| **C2 compaction** | Context | Compaction event (pre/post tokens, trigger) or `query_source='compact'` | Habit: new session / `/clear` per task | Measured |
| **C3 mixed_tasks** | Context | A later prompt whose file set has Jaccard < 0.1 with all earlier prompts in the session **and** session context > 100K at that point. LLM upgrade in §7.1. | Habit | Likely |
| **C4 fat_payload** | Context | existing `fatPayloadFinding`, per session | CLAUDE.md | Estimated |
| **C5 dead_mcp** | Context | existing `deadMCPFinding` (connected, 0 calls in the window) | `claude mcp remove` | Measured |
| **E1 repeat_cmd** | Execution | Same normalized `full_command` run ≥2× in one prompt with no Edit/Write/`fs_mutation` in between ("no change") | CLAUDE.md: "Only re-run `<cmd>` after editing `<ext>` files", where `<ext>` is the extensions edited before the first run | Likely |
| **E2 failing_mcp** | Execution | MCP server with ≥5 calls and error rate ≥20% | Habit: "check `<server>` credentials" | Measured |
| **E3 retries** | Execution | `v_retry_waste` | none (stat only) | Measured |
| **P1 repeat_approval** | Permissions | Same normalized pattern hand-approved (`user_temporary`) ≥2× **and** the pattern is on the safe list (gap G6) | settings.json `permissions.allow` diff | Measured |
| **P2 rejected** | Permissions | existing `rejectedWorkFinding` | `permissions.deny` diff | Measured |
| **M1 ancillary_model** | Model | `v_ancillary_model_spend` | none until the lever is verified (gap G12) → stat | Measured |
| **M2 oversized_model** | Model | Opus-class main-thread prompt with output < 2K tokens, no Edit/Write, and ≤3 tool calls → "Sonnet would likely do". LLM upgrade in §7.1. | `/model` habit | Likely |

**Priority** (sorts lists, and labels Sessions' "Priority = impact × confidence
× how often it repeats"):
`score = (usd_est + minutes × 0.10) × conf_weight × log2(1 + occurrences)`,
with `conf_weight` = Measured 1.0, Likely 0.7, Estimated 0.8. The 0.10 $/min
is a ranking weight only and is never displayed.

## 6. Page-by-page data contract

S = static, L = LLM-enhanced (static fallback always exists).

### 6.1 Global (sidebar)

| Element | Source | |
|---|---|---|
| Sessions count | count in current range | S |
| "Fixes · N to do" | open aggregated findings | S |
| "Local only · collector live" | new `GET /api/status`: service running + last event < 10 min | S |
| "Prompt text: logged" | any non-null `prompt_text` in the last 7d, cross-checked with `OTEL_LOG_USER_PROMPTS` in `~/.claude/settings.json` (claudesettings pkg) | S |
| "Run doctor" | new `GET /api/doctor` reusing `cli/doctor.go` checks | S |
| **AI insights · Off / On** | `GET /api/ai/config` → opens Settings (§7.3) | — |

### 6.2 Overview (`index.html`)

| Element | Derivation | |
|---|---|---|
| Period select | `since` = 7d / 30d / today | S |
| Hero "$X spent this week." | Σ cost in range | S |
| Hero lead sentence | template: `{n} things worth fixing — most in {top area}. Fixing the top three recovers about {Σ$} and {Σmin}.` | S (L optional rephrase, off by default) |
| KPI Spend + sparkline + ▼% | daily cost series; vs previous equal-length period | S |
| KPI Tokens + "% from cache" | Σ tokens; cache_read / (input + cache_read + cache_creation) | S |
| KPI Avoidable ≈$ | Σ `usd_est` of open occurrences in range | S |
| KPI Waiting on you | Σ user waits (§4.4) | S |
| KPI Saved so far / "N fixes working" | fix tracking (§8, gap G7) | S |
| Fix first (top 3) | top 3 aggregated findings by priority; "Happens in a of b sessions"; cost cell in the finding's natural unit | S |
| Four core problems | per area: Σ impact, issue count, sub-labels (kinds present), sessions affected, pip = worst severity | S |
| Spend by day (hatched = avoidable) | daily Σ cost; hatched portion = Σ usd_est of occurrences that day | S |
| Spend by project | Σ cost by `project` (§4.2) | S |
| Models footer | cost share by model | S |
| Sessions to review | top 2 sessions with `pr='now'`, with their issue counts | S |
| Export | new `GET /api/export` reusing `cli/export.go` (redacted by default) | S |

### 6.3 Sessions (`sessions.html`)

All fields come from `v_session_card` (§4.1) with `?since=today|7d|30d|3m`.
Filtering, sorting, grouping and search stay client-side over the returned
list, as in the mock. Search covers title and project, and the server-side
FTS `GET /api/search` can be added for prompt-body search. Summary line counts
come from the same list. The footnote's "last visit (Oct 18)" date comes from
`ui_state.last_visit_at`.

### 6.4 Session (`session.html`)

| Element | Derivation | |
|---|---|---|
| Left list | `v_session_card` grouped by project | S |
| Header pills | §4.1 | S |
| Context chart + state text | §4.3 ctx series; "peaked ~X", "compacted N×"; state = Context area severity | S |
| Thread rows + flags | §4.3; flag = any occurrence whose `evidence_ids` contains that row | S |
| Issues tabs (counts, pips) | occurrences grouped by area | S |
| Fix cards | occurrence evidence list (row id, label, size), impact line, fix text, confidence | S |
| Context card "Compacted… Prompt 3 was a separate task" | C2 + C3 | **L** (static fallback: "Prompt 3 touched none of the same files") |
| Model panel narrative ("used X for real work: editing…") | static: model + tool-category mix ("12 edits, 4 plan runs") | **L** optional phrasing |
| Receipt: total, tokens, well-spent % | Σ cost; avoidable = Σ session usd_est; well spent = 1 − avoidable/total | S |
| Receipt: By prompt rows | per interaction cost, tokens, flag count; label = first 40 chars of the prompt | **L** short label (static fallback: truncated prompt) |
| Receipt: avoidable by problem | per-area Σ | S |
| Receipt: token mix | 4 token kinds Σ | S |
| "41 model calls · 13 tool calls" | counts | S |
| Copy as text / Export redacted | client-side / `/api/export?session=` | S |
| Mark as fixed / Copy | `POST /api/fixes/{id}/state` (§8) | S |

### 6.5 Fixes (`fixes.html`)

| Element | Derivation | |
|---|---|---|
| Headline "Avoidable spend is down X% since you started" | avoidable $/week (last full week) vs baseline = first two weeks after `install_at` (gap G9) | S |
| Session health % + Now vs Week 1 bars | share of sessions with pr fine/look/now, this week vs first week | S |
| Avoidable / week, was | same as headline | S |
| Time back | user waits/week, last week vs baseline | S |
| Re-reads / prompt | C1 occurrences / prompts, last week vs baseline | S |
| Cost / active hour | Σ cost / Σ active_time hours | S |
| Week-by-week chart | weekly avoidable $ + healthy %; dashed lines and labels from `fix_marks` | S |
| "Usage stayed steady (6–8 sessions a week)" | sessions/week min–max; show only if CV < 0.3 | S |
| To fix cards | open aggregated findings with a config fix | S |
| Marked fixed cards ("Measuring · day 5 of 7", "So far −$0.52", "24K → 19K") | `fix_marks` baseline vs post-mark metric (§8) | S |
| Habits | aggregated findings with no config lever (C2, C3, E2, M2) | S (C3 L-enhanced) |

## 7. LLM layer (Anthropic key)

### 7.1 What the LLM does (and only this)

| Use | Input sent | Output | Static fallback |
|---|---|---|---|
| **Mixed-task detection** (C3) | prompt texts of one session (truncated to 500 chars each) | per prompt: `same_task: bool`, `task_label` | file-set Jaccard |
| **Prompt short labels** (receipt) | prompt text | ≤5-word label | truncated prompt |
| **Model-fit judgment** (M2) | per prompt: prompt text (≤500 chars), model, tool-category counts, output tokens | `fit: under/ok/over`, one-line reason | M2 heuristic |
| **Session explanation** (existing "Explain this trace", kept) | existing `internal/ai` context bundle | recommendations | none (button hidden) |

Rules: run only on user action ("Analyze with AI" on a session, or a
Settings toggle "auto-analyze new sessions"), cache by
`(session_id, input_hash)` in a new `ai_session_insights` table, and validate
outputs against the schema while dropping ungrounded ones (reuse
`internal/ai/validate.go`). LLM output **never** changes $ or token figures. It
can only reclassify an occurrence's confidence (Likely → Likely (AI-checked))
or add labels.

Model: bump `internal/ai.HaikuModel` from `claude-haiku-4-5` to the current
Haiku (`claude-haiku-5-5`).

### 7.2 Privacy

The first three uses send **prompt text** (the existing explain flow sends
only aggregates), so Settings has a separate opt-in toggle, "Send prompt
text to Anthropic for AI insights", which is off by default. If it's off,
those uses stay static even with a key set.

### 7.3 Key entry UI

- **Where**: a new **Settings** item in the main nav (below Fixes), and the
  sidebar foot line "AI insights · Off — add key" links to it.
- **Settings → AI insights** section:
  - Password input + Save, which calls the existing `PUT /api/ai/config`. The
    key is never echoed back.
  - Status line from `GET /api/ai/config`, extended to
    `{configured, source: "env"|"config"|"", last4}`. With `source=env` the
    input is disabled and shows "Using ANTHROPIC_API_KEY from environment".
  - "Test key", via a new `POST /api/ai/test`, which makes one minimal
    Messages call and returns ok/error. No data is sent.
  - Prompt-text toggle (§7.2) and auto-analyze toggle, both stored in
    `config.json`.
  - Remove key, which sends an empty string to the existing PUT.
- **Sidebar badge**: with no key, "Local only". With a key, "Local · AI on
  request". With auto-analyze on, "Local · AI auto".

## 8. API and schema changes

### New/changed endpoints

```
GET  /api/overview?since=7d          hero, KPIs, top fixes, areas, by-day, by-project, models, review list
GET  /api/sessions/cards?since=7d    v_session_card rows
GET  /api/sessions/{id}/timeline     thread rows + ctx series + prompts
GET  /api/sessions/{id}/issues       occurrences grouped by area + receipt numbers
GET  /api/fixes?since=install        aggregated findings (to fix / marked / habits)
GET  /api/progress?since=install     Fixes-page stats + weekly series + annotations
POST /api/fixes/{id}/state           {state: fixed|dismissed|snoozed|open}  (supersedes /api/findings/{id}/state)
GET  /api/status                     collector liveness, prompt logging, last event
GET  /api/doctor                     doctor checks as JSON
GET  /api/export?format=&since=&session=&include_content=
POST /api/ui/visit                   records last_visit_at
GET  /api/ai/config                  + source, last4, toggles
PUT  /api/ai/config                  + toggles
POST /api/ai/test
POST /api/ai/sessions/{id}/analyze   runs §7.1 for one session (cached)
```

Existing endpoints stay until the old React routes are removed.

### Migrations

- `0012_session_project`: `sessions.project_dir TEXT`.
- `0013_compactions`: typed `compactions(session_id, prompt_id, ts, pre_tokens, post_tokens, trigger, duration_ms)` plus a backfill from `events_raw`. Ingest gets a new `compaction` case in `projectLogEvent`.
- `0014_fix_marks`: `fix_marks(id, finding_id, subject, kind, state, marked_at, baseline_json, measure_until)` as an append-only history. `finding_state` becomes a view over the latest row per finding.
- `0015_ui_state`: `ui_state(key PRIMARY KEY, value)` holding `last_visit_at` and `install_at` (backfilled from `min(sessions.started_at)`).
- `0016_ai_session_insights`: LLM cache from §7.1.
- Views: `v_session_card`, `v_user_waits`.

### Fix measurement (Saved so far, Measuring, before → after)

When a fix is marked, store a baseline of the finding's metric, computed per
session over the previous 14 days (for example re-reads/prompt, approvals of
the pattern/session, or input tokens/request for dead MCP). The measuring
window is **the next 5 sessions or 7 days, whichever comes later**. This
resolves the mock's "5 sessions" vs "day 5 of 7" inconsistency (G7). The
outcome is "working" if the metric dropped by ≥50%, else "not yet". Savings:
`(baseline_rate − post_rate) × sessions_since × usd_per_unit`, labeled
Estimated.

## 9. Implementation phases

1. **Data foundations**: migrations 0012–0015, project resolution and
   backfill, compaction projection, user-wait view. Unit tests per derivation
   use the existing `testutil_test.go` fixtures.
2. **Detectors v2**: per-session occurrence model, C1–M2 static, the safe
   list for P1, priority score, and `/api/fixes`, `/api/sessions/*`. Port the
   existing findings tests.
3. **Overview, Sessions and Session endpoints**, then port the mock into
   `web/` as React routes (replacing Fix/Spend/Trace). Wire it to the API.
4. **Fixes page and fix tracking**: `fix_marks`, `/api/progress`.
5. **AI layer**: Settings page, key UI, `/api/ai/test`, per-session analyze,
   prompt-text consent.
6. **Ops endpoints**: status, doctor, export. Then remove the old routes.

Each phase is live-proven against a running binary per HANDOFF.md's "Testing
technique for live proof".

## 10. Open decisions (need the user)

1. Should `ccotel init` install a `SessionStart` hook that posts `cwd` to
   ccotel? That gives exact project names for all future sessions (see G1),
   at the cost of one more edit to `~/.claude/settings.json`.
2. M1 (background calls on Sonnet): show it as a stat only until a real
   config lever is confirmed, or drop the "Smaller model for background
   calls" fix card from the mock? (G12)
3. Default for the prompt-text consent toggle. This spec defaults it to
   **off**.
