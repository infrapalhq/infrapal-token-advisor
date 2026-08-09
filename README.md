# ccotel

A single-binary, local-only collector for Claude Code's own OpenTelemetry
output. `ccotel` runs an OTLP receiver on your machine, writes everything
Claude Code emits into a SQLite database, and serves an embedded dashboard
for reading it back — cost, tokens, tool usage, permission friction, and a
flame-chart-style waterfall of every prompt's execution.

Single-user, local-only, no network egress. Nothing here talks to any server
other than `127.0.0.1`.

## Install

**Homebrew:**

```sh
brew install ShamikMukhopadhyay/tap/ccotel
```

**Go:**

```sh
go install github.com/ShamikMukhopadhyay/ccotel/cmd/ccotel@latest
```

`ccotel` is a static binary (pure-Go SQLite driver, no cgo) — nothing else to
install.

## Quickstart

```sh
ccotel init      # patches ~/.claude/settings.json, creates ~/.ccotel/, installs a background service
ccotel ui        # opens the dashboard
```

`init` prints exactly what it changed in `settings.json` and backs up the
original to `settings.json.ccotel-backup-<timestamp>` first — it never
clobbers your existing config. **Restart any already-running Claude Code
sessions** after `init`; they won't pick up the new environment variables
otherwise.

Once `init` has run, the background service (see below) keeps recording
every Claude Code session automatically — you don't need `ccotel` running in
a terminal.

## Commands

```
ccotel init            patch ~/.claude/settings.json, create DB, install background service
ccotel start           run the collector in the foreground (what the service invokes)
ccotel stop            stop the background service
ccotel status          is the service up, DB size, event count, last event timestamp
ccotel ui              start the API and serve the embedded dashboard, opens a browser
ccotel doctor          diagnose why no data is arriving
ccotel export          export captured data
ccotel purge           delete captured data
ccotel uninstall       revert settings.json from backup, remove the background service
ccotel version         print the ccotel version
```

Run `ccotel <command> -h` for a command's exact flags. The ones worth calling
out:

- `ccotel init --raw-bodies` — additionally capture full raw API request/
  response bodies to `~/.ccotel/bodies` (see [Privacy](#privacy) — off by
  default for a reason).
- `ccotel ui --no-browser` — start the API + dashboard without trying to
  open a browser (e.g. over SSH, or if you just want the JSON API).
- `ccotel export --format jsonl|csv --since 7d [--include-content]` —
  content (prompts/responses/commands) is redacted unless
  `--include-content` is passed explicitly.
- `ccotel purge --before 30d | --session <id> | --all` — actually reclaims
  disk space (runs `VACUUM`), not just a row delete.
- `ccotel uninstall --keep-db` — remove the service and restore
  `settings.json`, but keep `~/.ccotel/ccotel.db` instead of deleting it.

### `doctor`

The most useful command when data isn't showing up. It checks, in order:
whether the collector is actually listening on the OTLP port, whether
`settings.json` has our env block and it points at the right port, whether a
conflicting `OTEL_*` var is exported in your shell (shell env overrides
`settings.json`), whether an enterprise **managed settings** file is present
that strips developer-set OTLP endpoints on startup, when we last received a
metric/event/span (events with no spans usually means the tracing beta flag
isn't set), and DB/WAL size plus recent ingest errors. Each check prints a
fix suggestion when it fails.

### Background service

- **macOS**: a launchd agent at `~/Library/LaunchAgents/com.ccotel.collector.plist`
  (`RunAtLoad` + `KeepAlive` — if the collector process dies, launchd
  restarts it, since Claude Code does not buffer telemetry to disk if the
  receiver is down).
- **Linux**: a systemd **user** unit at `~/.config/systemd/user/ccotel.service`
  (`systemctl --user enable --now`).
- Anything else: `init` fails gracefully with manual instructions instead of
  guessing at a service manager.

## Ports

| Port | Purpose |
|---|---|
| `4318` | OTLP `http/protobuf` receiver (`/v1/metrics`, `/v1/logs`, `/v1/traces`) |
| `4319` | JSON API + embedded dashboard (`ccotel ui`) |

Both bind to `127.0.0.1` only, never `0.0.0.0`. Configurable in
`~/.ccotel/config.json` if either port is already taken on your machine.

## The dashboard

`ccotel ui` serves seven pages: **Today** (cost/tokens/sessions/active-time
with sparklines), **Sessions** (sortable table), **Waterfall** (the
centerpiece — a Gantt-style timeline of one prompt's LLM calls, tool calls,
and nested subagent spans, with time spent blocked on your own approval
called out separately from Claude's own latency), **Efficiency** (cost vs.
prompt length and turn count), **Waste** (rejected-work cost and permission
friction, framed as allow-rules you should add to your settings), **Tools &
MCP** (usage, success rate, latency, and which connected MCP servers never
actually had a tool called — dead config costing you tool-definition tokens
on every request), and **Search** (full-text search across your own prompt
and response history).

## Privacy

The database contains your prompts, source code fragments, shell commands,
and file paths — this is a tool for reading your own history, so treat
`~/.ccotel/` accordingly:

- `~/.ccotel/` is created at `0700`, `ccotel.db` at `0600`.
- `ccotel purge` reclaims disk space (`VACUUM`), not just deletes rows.
- `ccotel export` redacts prompt/response/command content by default —
  pass `--include-content` to get it.
- `init` prints a one-line reminder of what's now being stored on disk.
- Raw API request/response bodies (full conversation history and system
  prompt, every request) are **never captured unless you pass
  `ccotel init --raw-bodies`**, and even then only to
  `~/.ccotel/bodies` as files — never inlined into the database, which
  would silently truncate at 60 KB and bloat every export.
- Zero outbound network calls, ever. There is no telemetry about the
  telemetry tool.

## Gotchas (read before you trust a number in the dashboard)

These are properties of Claude Code's telemetry export itself, not bugs in
`ccotel` — the dashboard is built to handle all of them explicitly:

1. **Metrics use delta temporality.** Each point is an increment since the
   last export, not a running total. `ccotel` sums them; if you're querying
   the DB directly, don't diff them.
2. **`prompt.id` is not attached to metrics**, deliberately, to avoid
   unbounded cardinality upstream. Metric↔event joins only work at
   `session.id` granularity — there is no per-prompt cost/token breakdown
   that isn't itself derived from `api_requests`, which do carry
   `prompt_id`.
3. **Cost is approximate**, not billing truth — it's computed from token
   counts and published rates, not your actual invoice. The dashboard
   labels it as such.
4. **`OTEL_*` env vars are not passed to subprocesses** — Bash, hooks, MCP
   servers, language servers. Nothing those processes do or emit reaches
   `ccotel`. If you're trying to trace what a hook did, this tool can't see
   it.
5. **Thinking blocks are never exported.** They're excluded from
   `assistant_response` events and redacted even in raw bodies. The
   dashboard has no reasoning-trace data and doesn't imply otherwise.
6. **Content attributes truncate at 60 KB**
   (`CLAUDE_CODE_OTEL_CONTENT_MAX_LENGTH`). `ccotel` detects the truncation
   marker and flags it in the UI rather than silently showing a cut-off
   string as if it were complete.
7. **If the collector is down, exports are lost** — Claude Code does not
   buffer telemetry to disk. This is why the background service is
   configured to restart itself (`KeepAlive` / systemd `enable --now`); if
   you stop it manually with `ccotel stop`, anything Claude Code emits in
   the meantime is gone.
8. **Detailed beta tracing** (`ENABLE_BETA_TRACING_DETAILED`) unlocks extra
   span attributes but needs org allowlisting for interactive sessions and
   is not a stable schema. `ccotel init` does not set this flag for you;
   `ccotel` parses the extra attributes opportunistically if your org has it
   enabled and they show up.
9. **Session transcripts** at `~/.claude/projects/*/*.jsonl` are joinable
   against this database on `message.uuid` / `request_id` / `tool_use_id`,
   but that file format is internal and version-unstable. Deliberately
   **out of scope for v1** — `ccotel` doesn't read or join against them.

## Uninstalling

```sh
ccotel uninstall          # restores settings.json, removes the service, deletes ~/.ccotel
ccotel uninstall --keep-db  # same, but keeps ccotel.db
```

## Development

```sh
go build ./...              # build the Go side
go test ./...                # run the Go test suite
scripts/build-web.sh        # build web/ (Vite/React) and embed it into internal/webui/dist
```

`internal/webui/dist` ships a placeholder `index.html` in source control —
`go build` succeeds without ever running the frontend build, but `ccotel ui`
won't show the real dashboard until `scripts/build-web.sh` has run at least
once. During frontend development, run `ccotel ui --no-browser` (it serves
the JSON API on `4319` regardless of whether the embedded dashboard has been
built) and `npm --prefix web run dev` side by side — the Vite dev server
proxies `/api/*` to that real running `ccotel` process.
