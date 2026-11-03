import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
  },
  server: {
    // In production the API and this app are served from the same origin
    // (embed.FS); this proxy makes relative /api/* fetches work identically
    // against a real `token-advisor ui` process during `npm run dev`.
    proxy: {
      '/api': 'http://127.0.0.1:4319',
    },
  },
  test: {
    environment: 'jsdom',
    setupFiles: ['src/test/setup.ts'],
    coverage: {
      provider: 'v8',
      include: ['src/**/*.{ts,tsx}'],
      // main.tsx only mounts the app, and src/test holds the test helpers.
      exclude: ['src/main.tsx', 'src/test/**', 'src/**/*.test.{ts,tsx}'],
      // Paths in lcov.info start from the repository root, as Codecov expects.
      reporter: ['text-summary', ['lcov', { projectRoot: '..' }]],
      // CI fails if the dashboard's coverage drops below these.
      thresholds: { statements: 80, lines: 80 },
    },
  },
})
