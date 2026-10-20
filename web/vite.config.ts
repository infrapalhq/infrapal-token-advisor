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
    // (embed.FS); this proxy makes relative /api/* fetches work identically
    // against a real `ccotel ui` process during `npm run dev`.
    proxy: {
      '/api': 'http://127.0.0.1:4319',
    },
  },
})
