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

## Logs

```sh
token-advisor logs            # the last 100 lines
token-advisor logs --errors   # errors and crashes only
token-advisor logs -f         # follow new lines
```

The log is `~/.infrapal-token-advisor/token-advisor.log`, rotated at 10 MiB with
three old files kept. `token-advisor doctor` and `token-advisor status` report
recent errors and crashes. Set `"log_level"` in `config.json` to `debug`,
`info`, `warn` or `error`, then restart the service.

## Data retention

The collector deletes data older than 6 months, checking once a day. That covers
the database and any raw bodies. To change it, set `"retention_months"` in
`~/.infrapal-token-advisor/config.json`, or `0` to keep everything, then restart
the service. `token-advisor status` shows the setting and the last cleanup.

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
