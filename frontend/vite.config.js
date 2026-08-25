import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

// Dev-Modus: `npm run dev` startet Vite auf :5173 und proxied alle
// /api-Requests an das Go-Backend auf :9310 (parallel via `make dev`).
// Produktion: `vite build` schreibt nach dist/, das Go per go:embed
// in das Binary einbettet.
export default defineConfig({
  plugins: [svelte()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:9310',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
});
