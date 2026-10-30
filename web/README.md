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

## Ship a change

The Go binary embeds `internal/webui/dist`, not `web/dist`. After changing
anything under `web/`, rebuild and copy it from the repo root:

```sh
scripts/build-web.sh
```

Commit the updated `internal/webui/dist` in the same PR as the source change.
