# Operating guide

How to run token-advisor day to day, and what to do when something's wrong.

## Background service

`token-advisor init` installs a user service that runs the collector and the
dashboard. No root access is needed.

| | macOS | Linux |
| --- | --- | --- |
| Service manager | launchd | systemd, user unit |
| Definition | `~/Library/LaunchAgents/com.infrapal.token-advisor.plist` | `~/.config/systemd/user/infrapal-token-advisor.service` |
| Starts | When you log in | At boot, if lingering is on; otherwise when you log in |
| If it exits | Restarted | Restarted after 2 seconds |

`token-advisor stop` stops it. It comes back at your next login, or sooner with
`token-advisor restart`.

The service keeps running the file `init` was run from, using its stable link on
your `PATH` if there is one. If you move or delete that file, it can't start. Run
`init` again from the new location. `init` refuses a temporary `go run` binary
for this reason.

### macOS

macOS can block background items. If the service doesn't come back after a
restart, check that token-advisor is allowed under System Settings > General >
Login Items & Extensions.

### Linux

Without lingering, systemd stops user services when you log out. `init` tries to
turn it on, but system policy can block that. `token-advisor doctor` tells you if
it's off. To turn it on yourself:

```sh
loginctl enable-linger
```

## Files

Everything is under `~/.infrapal-token-advisor/`:

| File | Holds |
| --- | --- |
| `token-advisor.db` | The database, with `-wal` and `-shm` files beside it |
| `token-advisor.db.backup-*` | Copies from before a schema upgrade |
| `config.json` | Settings |
| `token-advisor.log` and `.1` to `.3` | The collector's log |
| `collector.log`, `collector.err.log` | Service output: startup failures and Go runtime crashes |
| `bodies/` | Raw API bodies, if you ran `init --raw-bodies` |

## Settings

These live in `config.json`. Run `token-advisor restart` after changing one.

| Key | Default | What it does |
| --- | --- | --- |
| `otlp_port` | `4318` | Telemetry receiver port. Run `token-advisor init` after changing it, so Claude Code's settings match. |
| `api_port` | `4319` | Dashboard port |
| `retention_months` | `6` | How long to keep data. `0` keeps everything. |
| `log_level` | `info` | `debug`, `info`, `warn` or `error` |
| `anthropic_api_key` | none | Your key for AI Mode. Save it in the dashboard's Settings. It's stored in plain text, private to your user. |

`raw_bodies` and `settings_backup_path` are managed by `init`, so leave them alone.

The service can't see your shell's environment, so `ANTHROPIC_API_KEY` from your
shell only reaches a dashboard you run with `token-advisor start` or
`token-advisor ui` in that shell.

## Logs

```sh
token-advisor logs            # the last 100 lines
token-advisor logs --errors   # errors and crashes only
token-advisor logs -f         # follow new lines
```

The log rotates at 10 MiB and keeps three old files, so it stays under 40 MiB. It
holds errors, crash stack traces, counts and ids, never prompt or response text.
`token-advisor status` and `token-advisor doctor` report recent errors and
crashes.

`collector.err.log` catches what happens before logging starts and Go runtime
crashes. It's cut back to its last 1 MiB at startup once it passes 10 MiB.

## Data retention

Once a day the collector deletes data older than `retention_months`, including
raw bodies. It goes by the last run, so a laptop that's often asleep still gets
its cleanup. A session keeps its row until its last prompt is removed.

Your own decisions on fixes and findings are kept, because they aren't captured
data.

SQLite reuses freed space on its own. Once a quarter of the database is free, the
cleanup also shrinks the file. Telemetry waits in memory while that runs.

To delete data yourself:

```sh
token-advisor purge --before 30d
token-advisor purge --session <id>
token-advisor purge --all
```

`purge` also deletes database backups, because they still hold the deleted data.

## Troubleshooting

Start with `token-advisor doctor`. Run it in your own terminal: inside Claude Code
it reports a false failure, because Claude Code sets `CLAUDE_CODE_*` variables for
its own commands.

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| No data in the dashboard | Claude Code was already running during `init` | Restart your Claude Code sessions |
| Still no data | The collector isn't running, or managed settings strip the endpoint | Follow `doctor`'s fixes |
| Service gone after a restart | Binary moved, or blocked by macOS | See [Background service](#background-service), then run `token-advisor init` |
| Service stops at logout on Linux | Lingering is off | `loginctl enable-linger` |
| Page says "Dashboard not built" | Built without the dashboard | Run `scripts/build-web.sh`, then build again |
| "Database is newer than this build" | An older binary ran against a newer database | `token-advisor update`, or see [restoring a backup](SCHEMA.md#restoring-a-backup) |
| Dashboard won't start, collector runs | Something else has the port | `token-advisor logs --errors`, then change `api_port` |
| AI Mode says no key | The service can't see your shell's key | Save the key in Settings |

If it's still not working, [open an issue](https://github.com/infrapalhq/infrapal-token-advisor/issues/new/choose)
with the output of `token-advisor doctor` and `token-advisor logs --errors`. Check
them for anything private first.
