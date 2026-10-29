## What and why

<!-- What does this change, and what problem does it solve? Link the issue if there is one (Fixes #123). -->

## How I tested it

<!-- Commands you ran, and what you checked in the dashboard if the UI changed. -->

## Checklist

- [ ] `gofmt -l .` prints nothing, and `go vet ./...` and `go test -race ./...` pass
- [ ] If `web/` changed: ran `scripts/build-web.sh` and committed `internal/webui/dist`
- [ ] If the schema changed: added a new migration, didn't edit an existing one
- [ ] Commits are signed off (`git commit -s`)
