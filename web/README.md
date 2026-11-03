# Dashboard (web/)

The React dashboard served by `token-advisor ui`. It is built with
Vite and embedded into the Go binary from `internal/webui/dist`.

## Layout

- `src/main.tsx`: mounts the app
- `src/App.tsx`: routes
- `src/ta/`: pages (Overview, Sessions, Session, Fixes, Settings), the
  app shell, and `api.ts`, the typed client for the Go API
  (`internal/api/insight_*.go`)
- `src/test/`: test helpers, including a fake API and sample data
- `public/`: files served as they are, such as `theme.js`

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
npm --prefix web run lint      # oxlint
npm --prefix web run build     # tsc type check + production build
npm --prefix web run test      # Vitest
npm --prefix web run coverage  # Vitest, failing below 80% coverage
```

Tests sit next to the code they test, as `*.test.ts` or `*.test.tsx`. They
render whole pages with [Testing Library](https://testing-library.com/) against
a fake API (`src/test/api.ts`), so they check what a person would see and click.

## Content security policy

The Go server sends a content security policy (`internal/api/headers.go`) that
allows only the dashboard's own scripts, styles, fonts and images. So:

- No inline `<script>` or `<style>` in `index.html`. Put them in a file. The
  `style` prop in React is fine, because React sets styles through the DOM.
- No scripts, fonts or styles from other sites. Install them with npm, so
  they're bundled.
- `fetch` can only reach the dashboard's own API.

## Try it in the binary

The Go binary embeds `internal/webui/dist`, not `web/dist`. To see a change
there, rebuild and copy it from the repo root, then build the binary:

```sh
scripts/build-web.sh
go build ./cmd/token-advisor
```

`internal/webui/dist` is git-ignored, so there's nothing to commit. Release
builds run the script themselves.
