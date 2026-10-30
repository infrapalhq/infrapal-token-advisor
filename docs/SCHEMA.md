# Database schema

token-advisor stores everything in one SQLite database,
`~/.infrapal-token-advisor/token-advisor.db`. The schema is defined by the
numbered files in
[`internal/store/migrations`](../internal/store/migrations), which are the
source of truth for every column.

## Tables

| Group | Tables | Holds |
| --- | --- | --- |
| Raw input | `events_raw` | Every log event as received, written before it's projected into the tables below |
| Sessions | `sessions`, `interactions`, `assistant_responses`, `compactions` | Sessions, prompts, responses and context compactions |
| Requests | `api_requests`, `tool_calls`, `spans`, `metric_points`, `mcp_server_connections` | API calls, tool use, traces, metrics and MCP connections |
| Search | `interactions_fts`, `assistant_responses_fts` | Full-text indexes, kept in step by triggers |
| Staging | `pending_interaction_spans`, `pending_blocked_on_user_spans` | Spans waiting for the row they belong to |
| Dashboard | `finding_state`, `fix_marks`, `ai_recommendations`, `ui_state` | Your decisions on findings, AI Mode output and UI state |
| Internal | `ingest_errors`, `schema_migrations` | Failed writes and the applied migrations |

The `v_*` views hold the derived figures the dashboard shows. Business logic
lives in those views, not in the API handlers.

## How migrations run

Every command that opens the database runs any pending migrations:

1. A database from a **newer** build is refused with a message to update
   token-advisor. An older build can't know what a newer schema means.
2. A database that already has data is **backed up** first, to
   `token-advisor.db.backup-v<version>-<time>-<id>`. If the backup fails, for
   example on a full disk, nothing is changed.
3. Each migration runs in its own transaction, so a failure leaves the database
   at the last version that worked. Another process opening the database at the
   same time waits, then skips what's already been applied.

The two newest backups are kept. Backups hold everything the database did, so
they're private to your user, the retention cleanup deletes them after 30 days,
and `token-advisor purge` deletes them straight away.

## Restoring a backup

Use a backup from the version you're going back to, with a build from that time:

```sh
token-advisor stop
cd ~/.infrapal-token-advisor
mv token-advisor.db token-advisor.db.broken
rm -f token-advisor.db-wal token-advisor.db-shm
cp token-advisor.db.backup-v15-... token-advisor.db
```

Then run `token-advisor init`. A build that's newer than the backup migrates it
again on the way up, which is what you want after a bad release has been fixed.

## Adding a migration

1. Add `internal/store/migrations/NNNN_description.sql`, using the next number.
   Numbers can't skip. Don't write `BEGIN` or `COMMIT`: the runner wraps the file
   in a transaction.
2. Run `go test ./internal/store`. It prints a line to add to
   `internal/store/testdata/migrations.sha256`, which marks the migration as
   released.
3. Never edit a migration that has been released, because installs have already
   run it. The same test fails if one changes. Fix mistakes with a new migration.
4. Add new tables with captured data to `purgeTables` in
   `internal/cli/purge.go`, or `purge` and retention won't delete from them.

The tests check that an install on any older version reaches exactly the same
schema as a fresh one.
