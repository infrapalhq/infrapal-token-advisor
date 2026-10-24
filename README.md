# infrapal-token-advisor

Local-only token advisor for Claude Code. Collects Claude Code's OpenTelemetry
output into SQLite and serves a local dashboard.

## Development

```sh
go build ./...
go test ./...
scripts/build-web.sh   # build web/ into internal/webui/dist
```

## Privacy

Data stays on your machine. The database holds prompts and file paths, so
treat it as sensitive.

## License

MIT
