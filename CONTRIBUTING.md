# Contributing to infrapal-token-advisor

Thanks for helping out. Bug fixes, new insight checks, dashboard
improvements and docs are all welcome. Small, focused pull requests get
reviewed fastest.

By taking part you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
To report a security problem, see [SECURITY.md](SECURITY.md) instead of
opening an issue.

## Set up

You need:

- Go, at the version in [`go.mod`](go.mod)
- Node.js 24 (only if you change the dashboard under `web/`)

```sh
git clone https://github.com/infrapalhq/infrapal-token-advisor.git
cd infrapal-token-advisor
go build ./...
go test ./...
```

To try a local build against your own Claude Code sessions, build a binary
first (`init` registers the binary's own path as the background service, so
don't use `go run` for it):

```sh
go build ./cmd/token-advisor        # writes ./token-advisor (git-ignored)
./token-advisor init                # patches ~/.claude/settings.json, installs the service
./token-advisor ui                  # opens the dashboard
./token-advisor uninstall --keep-db # undo, keeping your data
```

## Where things live

| Path | What it is |
| --- | --- |
| `cmd/token-advisor` | CLI entry point |
| `internal/otlp` | OTLP/HTTP receiver for Claude Code telemetry |
| `internal/ingest` | Turns OTLP logs, metrics and spans into rows |
| `internal/store` | SQLite store and numbered migrations (`migrations/NNNN_*.sql`) |
| `internal/api` | Dashboard API, insight checks and fix suggestions |
| `internal/ai` | Optional AI Mode (Anthropic API) |
| `internal/service` | launchd / systemd service install |
| `web/` | React dashboard (see [web/README.md](web/README.md)) |
| `internal/webui/dist` | Built dashboard, embedded into the binary |

## Making a change

1. Branch from `main`.
2. Write a test for the change where one makes sense.
3. Run the same checks CI runs:

   ```sh
   gofmt -l .            # should print nothing
   go vet ./...
   go test -race ./...
   ```

4. If you changed anything under `web/`, rebuild the embedded dashboard and
   commit `internal/webui/dist` **in the same PR** as the source change. CI
   fails if they differ.

   ```sh
   scripts/build-web.sh
   npm --prefix web run lint
   ```

5. Schema changes go in a **new** migration file with the next number.
   Never edit a migration that has already been released.

## Commit messages

Start the subject with a type: `feat:`, `fix:`, `docs:`, `ci:`, `chore:`,
`refactor:` or `test:`. The release changelog leaves out `docs:`, `test:`
and `chore:` commits, so pick the type that matches what a user would care
about.

## Sign your commits (DCO)

This project uses the [Developer Certificate of Origin](https://developercertificate.org/).
It is a statement that you wrote the change, or otherwise have the right to
submit it. Add a sign-off to every commit:

```sh
git commit -s -m "fix: handle empty spans"
```

That adds a `Signed-off-by: Your Name <you@example.com>` line. No CLA to
sign.

## License

The project is [MIT licensed](LICENSE). Contributions are accepted under the
same license ("inbound = outbound").

## Pull requests

Fill in the PR template, keep the PR to one change, and make sure CI is
green. A maintainer will review it. Please be patient: this is maintained
by a small team.
