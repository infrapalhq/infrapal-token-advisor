# Changelog

All notable changes to token-advisor are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
[Semantic Versioning](https://semver.org/). Security fixes are listed under
**Security**, with credit to whoever reported them.

## [Unreleased]

The first release.

### Added

- A local collector that receives Claude Code's OpenTelemetry and stores it in
  SQLite on your machine.
- A dashboard with Overview, Sessions, a step-by-step session timeline, and Fixes.
  Dark mode is the default.
- Checks for re-read files, repeated commands, repeated approvals, compactions,
  failing and unused MCP servers, and oversized models. Each is labelled Measured,
  Likely or Estimated.
- Fixes to paste in, such as `CLAUDE.md` lines and allow rules, offered only for
  read-only commands, with week-by-week tracking of whether they helped.
- An MCP server, so Claude Code can query your usage itself, with slash commands
  to review your usage, explain a session and compare weeks. It runs on your
  Claude Code plan, so there's no API key to add.
- `token-advisor` commands: `init`, `status`, `doctor`, `ui`, `logs`, `export`,
  `purge`, `update`, `restart` and `uninstall`.
- A background service that starts at login and restarts if it stops, using
  launchd on macOS and systemd on Linux.
- Automatic deletion of data older than 6 months, which you can change.
- Database backups before every upgrade, and a refusal to open a database from a
  newer version.
- Support for adding other coding agents through adapters, with a contract test
  suite, an example adapter and a guide. Claude Code is the only agent so far.

### Changed

- The "connect to Claude Code" card moved from Fixes to Settings, with simpler
  wording. The AI recommendations card on Fixes appears once you add a key.
- Models with no share of your spend are left out of the model list on Overview.
- The dashboard follows the approved design. The sidebar has the infrapal logo,
  icons, a time range, and your recent sessions by project. Rows on Overview and
  Sessions are cards, and the session page uses the full sidebar.
- Fixes lists what to fix with tabs for To fix, Marked fixed and Not relevant, and
  filters by problem. Findings that share one fix show as one card. The progress
  report moved to its own page.
- Overview's spend chart shows a bar per week for ranges longer than two weeks,
  and the project list shows the top five and the rest as "Other".
- The light and dark theme switch moved from the sidebar to the bottom of
  Settings.
- A session's receipt lists its prompts costliest first, five at a time, with
  Show more for the rest.
- Every agent turn in a session shows the agent's name, including turns that
  start with tool calls.

### Fixed

- Findings that belong to no session, such as an unused MCP server, are labelled
  global, and Overview's problem cards count them apart from the problems found in
  sessions. Before, they showed as "1 session" or "0 sessions", and the Fixes count
  differed between Overview and Fixes.
- Opening a session with nothing in it no longer blanks the page, and a page that
  crashes now shows a message and a way back instead of a white screen.
- Sessions with no model or tool calls are left out of the lists and counts.
- Assistant replies show their formatting (bold, lists, code) instead of raw
  markdown.
- Overview and Sessions offer the same time ranges and remember your choice. An
  unknown range in the address falls back to 7 days.
- Pages with no match, and sessions that don't exist, show a proper page inside the
  dashboard.
- The dashboard refreshes itself every minute, and opens each page at the top.
- The sidebar now highlights the page you are on.
- Long names and commands no longer make the Fixes page wider than the window.

### Security

- The collector and dashboard only answer to programs on your machine. They refuse
  web pages in your browser, DNS rebinding and oversized payloads.
- The dashboard sends a content security policy and other security headers, so it
  only runs its own scripts and can't be framed by another site.
- Downloads are checked against SHA-256 checksums.
- Releases are signed with Sigstore and carry SLSA build provenance. See
  [checking a release](docs/OPERATIONS.md#checking-a-release).
- Releases are reproducible, so anyone can check a download was built from its
  source. See [reproducing a release](docs/OPERATIONS.md#reproducing-a-release).
