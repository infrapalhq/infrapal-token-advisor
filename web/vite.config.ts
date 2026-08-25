import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
  },
  server: {
    // In production the API and this app are served from the same origin
    // (embed.FS, milestone 7); this proxy makes relative /api/* fetches
    // work identically against a real `ccotel ui` process during `npm run dev`.
    proxy: {
      // Dev-only second backend (built from this checkout, real DB, extra
      // views/metrics not on the release build at :4319) for the Optimize
      // page only. Registered before '/api' and given a non-overlapping
      // prefix — Vite's proxy matches by string prefix, so '/api-dev'
      // would otherwise be swallowed by the '/api' rule below.
      '/opt-api': {
        target: 'http://127.0.0.1:4320',
        rewrite: (path) => path.replace(/^\/opt-api/, '/api'),
      },
      '/api': 'http://127.0.0.1:4319',
    },
  },
})
