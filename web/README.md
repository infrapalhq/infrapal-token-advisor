# Dashboard (web/)

The React dashboard served by `token-advisor ui`. It is built with
Vite and embedded into the Go binary from `internal/webui/dist`.

## Layout

- `src/main.tsx`: routes
- `src/ta/`: pages (Overview, Sessions, Session, Fixes, Settings), the
  app shell, and `api.ts`, the typed client for the Go API
  (`internal/api/insight_*.go`)

## Develop

Start the API, then the Vite dev server. Vite proxies `/api` to
`127.0.0.1:4319`, so the app talks to the real API with hot reload.

```sh
go run ./cmd/token-advisor ui
npm --prefix web install
npm --prefix web run dev
```

## Check

```sh
npm --prefix web run lint    # oxlint
npm --prefix web run build   # tsc type check + production build
```

## Try it in the binary

The Go binary embeds `internal/webui/dist`, not `web/dist`. To see a change
there, rebuild and copy it from the repo root, then build the binary:

```sh
scripts/build-web.sh
go build ./cmd/token-advisor
```

`internal/webui/dist` is git-ignored, so there's nothing to commit. Release
builds run the script themselves.
