# Security and privacy

infrapal-token-advisor runs on your machine and records your Claude Code
sessions, including prompts and file paths. This page explains what it
stores, what leaves your machine, and how to report a security problem.

## Supported versions

Security fixes go into the latest release only. Please upgrade before
reporting.

## Report a vulnerability

**Do not open a public issue.** Report it privately through
[GitHub private vulnerability reporting](https://github.com/infrapalhq/infrapal-token-advisor/security/advisories/new).

Include the version (`token-advisor version`), your OS, and steps
to reproduce. We aim to:

- acknowledge the report within 3 business days
- fix or mitigate a confirmed critical issue within 30 days, and publish an
  advisory once a fixed release is out

These are targets for a small team, not a guarantee.

Examples of what we treat as security issues: another user or a website
reading your telemetry, the API or receiver accepting connections from
other machines, the database or config being readable by other users, or
AI Mode sending more than described below.

## What leaves your machine

| Component | Network access | What is sent |
| --- | --- | --- |
| Telemetry receiver | Listens on `127.0.0.1:4318` only | Nothing. Claude Code on the same machine sends to it. |
| Dashboard and API | Listens on `127.0.0.1:4319` only, while the collector runs. Refuses requests whose `Host` isn't localhost and writes from other sites, so web pages can't reach your data. | Nothing. |
| `token-advisor update` | `api.github.com` and `github.com`, only when you run it | Nothing but ordinary requests for the release files. |
| AI Mode (off by default) | `https://api.anthropic.com` with your own API key, only when you press **Generate** | Aggregated figures and labels: costs, token and call counts, error and retry rates, tool and MCP server names, model names, session and prompt ids, and the dashboard's insight titles and details. **Never** prompt or response text, tool input or output, file paths or shell commands. |
| MCP server (off until you run `claude mcp add`) | None of its own: Claude Code runs `token-advisor mcp` over stdio, and sends tool results to Anthropic as part of your conversation | Metrics and ids: costs, token and call counts, model, tool and MCP server names, session and prompt ids, and finding titles. Never prompts, responses, file paths, commands, error text or user identity. |

There is no usage telemetry, automatic update check or crash reporting.

## What is stored locally

Everything lives under `~/.infrapal-token-advisor/`, created with mode `0700`:

| File | Contents | Mode |
| --- | --- | --- |
| `token-advisor.db` (+ `-wal`, `-shm`) | Every recorded session: prompts, assistant responses, tool calls and their content, file paths, shell commands, costs | `0600` |
| `config.json` | Ports, settings, and your Anthropic API key if you saved one in the dashboard (stored in plain text) | `0600` |
| `bodies/` | Raw API request/response bodies, only if `raw_bodies` is turned on | `0700` dir |
| `token-advisor.log` and rotated `.1` to `.3` | The collector's log, at most 40 MiB in all: errors, crash stack traces, counts and ids. Never prompt or response text. | `0600` |
| `token-advisor.db.backup-*` | Copies of the database from before a schema upgrade, kept for at most 30 days | `0600` |
| `collector.log`, `collector.err.log` | Service output, kept small: startup failures and Go runtime crashes | |

Treat the database as sensitive: it can contain anything you or Claude
typed, including secrets pasted into a prompt. Set `ANTHROPIC_API_KEY` in
the environment instead of saving the key if you'd rather not store it.

The file permissions are access control, not encryption.

## Changes to your Claude Code settings

`init` backs up `~/.claude/settings.json`, then adds OpenTelemetry
environment variables to it. These include `OTEL_LOG_USER_PROMPTS`,
`OTEL_LOG_ASSISTANT_RESPONSES`, `OTEL_LOG_TOOL_DETAILS` and
`OTEL_LOG_TOOL_CONTENT`, which make Claude Code send prompt, response and
tool content to the local receiver.

## Removing your data

- The collector deletes data older than 6 months, including raw bodies.
  Set `retention_months` in `config.json` to change this, or `0` to keep
  everything.
- `token-advisor purge --before 30d` deletes data older than 30
  days (`--all` deletes everything). It also deletes database backups.
- `token-advisor uninstall` stops and removes the service,
  restores `~/.claude/settings.json` from the backup, and deletes
  `~/.infrapal-token-advisor/`. Add `--keep-db` to keep the data.
