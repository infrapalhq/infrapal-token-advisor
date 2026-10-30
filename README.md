# infrapal-token-advisor

Local-only token advisor for Claude Code. Collects Claude Code's OpenTelemetry
output into SQLite and serves a local dashboard.

## Background service

`token-advisor init` installs a user service that runs the collector and the
dashboard (http://127.0.0.1:4319). It starts when you log in and restarts if it
exits, so it comes back after a reboot. `token-advisor stop` stops it until your
next login. Run `token-advisor doctor` if it doesn't come back.

Run `init` from a built binary (not `go run`) that will stay where it is: the
service keeps running that exact file.

On macOS, make sure token-advisor is allowed under System Settings > General >
Login Items & Extensions. On Linux, `init` enables lingering so the service
keeps running after you log out.

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
