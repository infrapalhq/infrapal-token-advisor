# Build prompt: `ccotel` — local Claude Code telemetry collector

> Paste this into a fresh Claude Code session in an empty directory. Work through it in the milestone order at the bottom rather than all at once.

---

## What we're building

`ccotel` — a single-binary Go tool that runs as a local OTLP receiver, captures everything Claude Code emits via OpenTelemetry into a SQLite database, and serves an embedded React dashboard for exploring it.

Single-user, local-only, no network egress. Install via `brew install` or `go install`, run `ccotel init` once, and it records forever in the background.

**Read the upstream spec first**: https://code.claude.com/docs/en/monitoring-usage — fetch it and treat it as the source of truth for every metric name, event name, and attribute. Do not rely on memory for the telemetry schema. If any detail in this prompt conflicts with the live docs, the docs win; tell me where they diverged.

---

## Constraints

- **Go**, module `github.com/<me>/ccotel`. Ask me for the module path before scaffolding.
- **Pure-Go SQLite** — use `modernc.org/sqlite`, not `mattn/go-sqlite3`. CGO must stay off so we get a genuinely static cross-compiled binary.
- **React UI embedded via `embed.FS`** — the Vite build output is compiled into the binary. `ccotel ui` serves it from localhost and opens a browser. No separate install step, no node runtime at execution time.
- **OTLP over `http/protobuf` only.** Skip gRPC in v1; it drags in most of grpc-go for a loopback-only receiver. Use `go.opentelemetry.io/proto/otlp` for the generated types.
- Everything binds to `127.0.0.1`. Never `0.0.0.0`.
- Go 1.22+, standard library `net/http` routing (no gorilla/chi unless you can justify it).

---

## CLI surface

```
ccotel init            # patch ~/.claude/settings.json, create DB, install background service
ccotel start           # run collector in foreground (what the service invokes)
ccotel stop
ccotel status          # is the service up, DB size, event count, last event timestamp
ccotel ui              # start API + serve embedded React app, open browser
ccotel doctor          # diagnose why no data is arriving
ccotel export --format jsonl|csv --since 7d
ccotel purge --before 30d | --session <id> | --all
ccotel uninstall       # revert settings.json from backup, remove service, optionally keep DB
```

### `init` specifics

Merge into `~/.claude/settings.json` — read, parse, merge the `env` block, write back. **Back up to `settings.json.ccotel-backup-<timestamp>` first** and preserve existing keys and formatting as far as possible. Never clobber the file.

Keys to set (full-content capture, which I've chosen deliberately):

```json
{
  "env": {
    "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
    "OTEL_METRICS_EXPORTER": "otlp",
    "OTEL_LOGS_EXPORTER": "otlp",
    "OTEL_TRACES_EXPORTER": "otlp",
    "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA": "1",
    "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
    "OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
    "OTEL_METRIC_EXPORT_INTERVAL": "10000",
    "OTEL_LOGS_EXPORT_INTERVAL": "2000",
    "OTEL_TRACES_EXPORT_INTERVAL": "2000",
    "OTEL_METRICS_INCLUDE_VERSION": "true",
    "OTEL_METRICS_INCLUDE_ENTRYPOINT": "true",
    "OTEL_LOG_USER_PROMPTS": "1",
    "OTEL_LOG_ASSISTANT_RESPONSES": "1",
    "OTEL_LOG_TOOL_DETAILS": "1",
    "OTEL_LOG_TOOL_CONTENT": "1"
  }
}
```

`OTEL_LOG_RAW_API_BODIES` stays **off** by default — add `ccotel init --raw-bodies` to enable it in `file:~/.ccotel/bodies` mode (never inline mode; inline truncates at 60 KB and bloats the DB). Print a clear warning when it's enabled: raw bodies contain the entire conversation history and system prompt on every request.

`init` must print exactly what it changed and remind me that **already-running Claude Code sessions won't pick up the new env** — restart them.

### Background service

- macOS: write a launchd plist to `~/Library/LaunchAgents/com.ccotel.collector.plist`, `RunAtLoad` + `KeepAlive`, load it.
- Linux: write a systemd **user** unit to `~/.config/systemd/user/ccotel.service`, `systemctl --user enable --now`.
- Detect the platform; fail gracefully with manual instructions on anything else.

### `doctor`

The most useful command in the tool. Check and report on each, with a fix suggestion for each failure:

1. Is the collector process listening on 4318?
2. Does `~/.claude/settings.json` contain our env block, and does the endpoint match the port we're bound to?
3. Are there conflicting `OTEL_*` variables exported in the user's shell that would override settings.json?
4. Is there a **managed settings** file present that would strip our endpoint? (Enterprise managed settings remove developer-set OTLP endpoints at startup — this is a real failure mode at work.)
5. When did we last receive a metric / event / span? If events but no spans, the traces beta flag likely isn't set.
6. DB size, WAL size, any ingest errors in the last hour.

---

## Ingest pipeline

Three handlers: `POST /v1/metrics`, `/v1/logs`, `/v1/traces`. Decode protobuf into the OTLP Go types, then:

**Two-phase write.** Insert the raw decoded record into a catch-all table *first*, then project into typed tables in the same transaction. Claude Code's telemetry schema is explicitly in beta and adds attributes constantly — an unknown field must degrade to "typed column is NULL", never to data loss or a 500. Log unknown attribute keys at debug level so I can see when upstream adds something.

Respond `200` fast, buffer, and write in batches. A slow write must never make Claude Code block or drop an export.

### Schema

```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA foreign_keys=ON;

CREATE TABLE sessions (
  session_id TEXT PRIMARY KEY,
  started_at INTEGER, ended_at INTEGER,
  start_type TEXT, app_version TEXT, app_entrypoint TEXT,
  user_id TEXT, user_email TEXT, org_id TEXT, terminal_type TEXT,
  resource_attrs TEXT           -- JSON
);

CREATE TABLE interactions (      -- one row per prompt.id
  prompt_id TEXT PRIMARY KEY,
  session_id TEXT,
  seq INTEGER, started_at INTEGER, duration_ms INTEGER,
  prompt_text TEXT, prompt_length INTEGER,
  command_name TEXT, command_source TEXT,
  message_uuid TEXT, trace_id TEXT, root_span_id TEXT
);

CREATE TABLE assistant_responses (
  message_uuid TEXT PRIMARY KEY,
  prompt_id TEXT, session_id TEXT, ts INTEGER,
  model TEXT, request_id TEXT, query_source TEXT,
  response_text TEXT, response_length INTEGER
);

CREATE TABLE api_requests (
  id INTEGER PRIMARY KEY,
  request_id TEXT, client_request_id TEXT,
  prompt_id TEXT, session_id TEXT, ts INTEGER,
  model TEXT, query_source TEXT, speed TEXT, effort TEXT,
  input_tokens INTEGER, output_tokens INTEGER,
  cache_read_tokens INTEGER, cache_creation_tokens INTEGER,
  cost_usd_micros INTEGER, duration_ms INTEGER, ttft_ms INTEGER,
  stop_reason TEXT, attempt INTEGER, success INTEGER, status_code INTEGER,
  error TEXT,
  agent_name TEXT, skill_name TEXT, plugin_name TEXT,
  mcp_server_name TEXT, mcp_tool_name TEXT,
  request_body_ref TEXT, response_body_ref TEXT
);

CREATE TABLE tool_calls (
  tool_use_id TEXT PRIMARY KEY,
  prompt_id TEXT, session_id TEXT, ts INTEGER,
  tool_name TEXT, tool_source TEXT,
  decision TEXT, decision_source TEXT,
  blocked_on_user_ms INTEGER, execution_ms INTEGER, duration_ms INTEGER,
  success INTEGER, error_type TEXT, error TEXT,
  input_size_bytes INTEGER, result_size_bytes INTEGER, result_tokens INTEGER,
  mcp_server_name TEXT, mcp_tool_name TEXT, mcp_server_scope TEXT,
  skill_name TEXT, subagent_type TEXT,
  file_path TEXT, full_command TEXT, git_commit_id TEXT,
  tool_input TEXT               -- JSON
);

CREATE TABLE spans (
  span_id TEXT PRIMARY KEY, trace_id TEXT, parent_span_id TEXT,
  span_type TEXT, name TEXT, start_ns INTEGER, end_ns INTEGER,
  status TEXT, session_id TEXT, prompt_id TEXT,
  agent_id TEXT, parent_agent_id TEXT, attrs TEXT
);

CREATE TABLE metric_points (
  id INTEGER PRIMARY KEY, ts INTEGER, name TEXT, value REAL,
  session_id TEXT, model TEXT, attrs TEXT
);

CREATE TABLE events_raw (        -- catch-all, written before projection
  id INTEGER PRIMARY KEY, ts INTEGER, session_id TEXT, prompt_id TEXT,
  event_name TEXT, seq INTEGER, body TEXT
);
```

Add indexes on every `session_id`, `prompt_id`, and `ts` column. `interactions.prompt_text` and `assistant_responses.response_text` should be FTS5-indexed so the UI can full-text search my own prompt history — that alone justifies the tool.

Handle out-of-order arrival: a `tool_decision` event can land before or after the matching `tool_result`, and spans arrive on a different flush cycle than events. Upsert on `tool_use_id`, merging fields rather than overwriting with NULLs. Same for `interactions` (created by `user_prompt` event, enriched by the `claude_code.interaction` span).

### Schema migrations

Versioned migrations from day one, tracked in a `schema_version` table, applied automatically on start. This will change often.

---

## Derived views

Create these as SQL views so the API is thin:

- `v_prompt_efficiency` — per `prompt_id`: llm turn count, total tokens, cost, cache hit ratio, tool call count, rejected count, tool error count, wall-clock duration, time blocked on my approval, whether it triggered compaction, whether any request hit `max_tokens`.
- `v_wasted_spend` — cost of API requests whose resulting tool calls were rejected, per session and per day.
- `v_permission_friction` — per tool + parameter shape: count of `user_temporary` / `user_permanent` decisions, total `blocked_on_user_ms`. This surfaces the allow-rules I should be adding.
- `v_mcp_inventory` — every MCP server that connected, vs. every MCP server that actually had a tool called. The difference is dead config costing me tool-definition tokens on every request.
- `v_skill_effectiveness` — skill invocations split by `invocation_trigger`, so I can see which skills Claude never picks up proactively.
- `v_session_summary` — per session: duration, active time, cost, tokens, tools, commits, PRs, lines added/removed, compaction count.
- `v_model_performance` — p50/p95 `ttft_ms` and `duration_ms`, error rate, by model + effort + speed.

---

## HTTP API

`GET /api/...` on a separate port from the OTLP receiver (default 4319). JSON, no auth (loopback only), CORS off.

Endpoints for: sessions list + detail, interaction detail (full waterfall: spans, api requests, tool calls, nested subagents), search across prompts/responses, each of the derived views, and a timeseries endpoint with a `bucket` param.

---

## React dashboard

Vite + React + TypeScript + Tailwind, built to `web/dist`, embedded. Recharts for charts.

**Pages:**

1. **Today** — cost, tokens, sessions, active time for the current day, with sparklines against the trailing week. Recent interactions list.
2. **Sessions** — sortable table, drill into one session.
3. **Interaction waterfall** — the centrepiece. For one `prompt.id`: the prompt text, then a Gantt-style timeline of `llm_request` and `tool` spans, with subagent spans nested and indented under their parent. Show `blocked_on_user` segments in a visually distinct way — that's *my* latency, not Claude's. Click any tool call for its full input/output.
4. **Efficiency** — `v_prompt_efficiency` sorted by cost descending, with a scatter of prompt length vs. turn count.
5. **Waste** — rejected-work cost and permission friction, framed as "here are the allow-rules you should add."
6. **Tools & MCP** — usage, success rate, latency, plus the dead-config list from `v_mcp_inventory`.
7. **Search** — FTS across my prompt and response history.

**Design direction.** Don't ship the default AI-dashboard look: avoid the cream/serif/terracotta palette, avoid the near-black-with-one-acid-accent palette, avoid generic card grids with a big number and a small label. This is a developer instrument for reading agent execution traces — the reference points are a profiler flame chart and an oscilloscope, not a SaaS analytics page. Pick a deliberate palette and type pairing, and make the waterfall the signature element that the whole UI is built around. Show me your token system (4–6 named hex values, display/body/mono faces, layout concept) before you write any UI code, and tell me what you'd have done by default and why you chose otherwise. Mono face carries a lot of weight here — choose it properly.

Quality floor without ceremony: keyboard focus visible, `prefers-reduced-motion` respected, readable at 1280px and up (mobile is not a target for this).

---

## Privacy

The DB will contain my prompts, source code fragments, shell commands, and file paths.

- `~/.ccotel/` at `0700`, `ccotel.db` at `0600`.
- `ccotel purge` must actually reclaim space (`VACUUM`), not just delete rows.
- `ccotel export` redacts prompt/response/command content unless `--include-content` is passed.
- Print a one-line reminder after `init` about what's being stored on disk.
- No telemetry about the telemetry tool. Zero outbound network calls, ever.

---

## Gotchas to handle explicitly

Bake these into the code and note them in the README:

1. **Metrics use delta temporality by default** — points are increments, not running totals. Sum them; don't diff them.
2. **`prompt.id` is not on metrics**, deliberately, to avoid unbounded cardinality. Metric↔event joins only work at `session.id` granularity. Do not fake it.
3. **Cost is approximate**, not billing truth. Label it as such in the UI.
4. **`OTEL_*` vars are not passed to subprocesses** — Bash, hooks, MCP servers, language servers. Nothing they emit reaches us.
5. **Thinking blocks are never exported.** Excluded from `assistant_response`, redacted in raw bodies. The UI must not imply we have reasoning traces.
6. **Content attributes truncate at 60 KB** (`CLAUDE_CODE_OTEL_CONTENT_MAX_LENGTH`). Detect the truncation marker and flag it in the UI rather than showing a silently cut-off string.
7. **If the collector is down, exports are lost.** Claude Code does not buffer to disk. This is why the service must be `KeepAlive`.
8. **Detailed beta tracing** (`ENABLE_BETA_TRACING_DETAILED`) unlocks extra span attributes but needs org allowlisting for interactive sessions and is not a stable schema. Don't set it in `init`; parse the attributes if they show up.
9. Session transcripts at `~/.claude/projects/*/*.jsonl` are joinable on `message.uuid` / `request_id` / `tool_use_id`, but the format is internal and version-unstable. **Out of scope for v1** — leave a note in the README, don't build it.

---

## Testing

- Golden-file tests: capture real OTLP protobuf payloads from a live session into `testdata/`, replay them through the ingest path, assert on the projected rows.
- A `ccotel replay <dir>` hidden command that feeds fixtures in, so the UI can be developed without generating traffic.
- Out-of-order arrival tests: `tool_result` before `tool_decision`, spans after events, session end before some of its metrics.
- Test that unknown attributes land in `events_raw` and don't break projection.

---

## Build order

Do these as separate milestones. **Stop after each and show me what you have before continuing.**

1. Scaffold + CLI skeleton + config + SQLite schema + migrations.
2. OTLP receiver for logs/events only. Ingest to `events_raw`. Prove data lands with a real Claude Code session.
3. Projection into typed tables. Add metrics and traces receivers.
4. `init`, `doctor`, service install/uninstall. This is the point where it's genuinely usable.
5. Derived views + HTTP API.
6. React UI — design plan first, then build, starting with the interaction waterfall.
7. `embed.FS` wiring, goreleaser config, Homebrew tap formula, README.

Start with milestone 1. Before you write code, fetch the monitoring docs, confirm the current metric and event names against what's in this prompt, and tell me about anything that's changed.
