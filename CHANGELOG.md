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
- Optional AI Mode, which sends only aggregated figures to Anthropic with your own
  key, when you press **Generate**.
- An MCP server, so Claude Code can query your usage itself.
- `token-advisor` commands: `init`, `status`, `doctor`, `ui`, `logs`, `export`,
  `purge`, `update`, `restart` and `uninstall`.
- A background service that starts at login and restarts if it stops, using
  launchd on macOS and systemd on Linux.
- Automatic deletion of data older than 6 months, which you can change.
- Database backups before every upgrade, and a refusal to open a database from a
  newer version.
- Support for adding other coding agents through adapters, with a contract test
  suite, an example adapter and a guide. Claude Code is the only agent so far.

### Security

- The collector and dashboard only answer to programs on your machine. They refuse
  web pages in your browser, DNS rebinding and oversized payloads.
- Downloads are checked against SHA-256 checksums.
