/// <reference types="vitest/config" />
import { configDefaults } from 'vitest/config';
import { defineConfig } from 'vite';
import { compressPlugin } from './vite-plugins/compress.ts';
import { swPlugin } from './vite-plugins/sw.ts';

// Entwicklung: `npm run dev` startet Vite auf :5174 und leitet /api (auch den
// Ereigniskanal per WebSocket) an das Go-Backend auf :9310 weiter.
// Produktion: `vite build` schreibt nach dist/, Go bettet es ins Binary ein.
// LCM_UI_PORT und LCM_API_PORT erlauben mehrere Entwicklungsumgebungen
// nebeneinander.
const uiPort = Number(process.env.LCM_UI_PORT ?? 5174);
const apiPort = Number(process.env.LCM_API_PORT ?? 9310);

export default defineConfig({
  plugins: [swPlugin(), compressPlugin()],
  server: {
    port: uiPort,
    strictPort: true,
    proxy: {
      '/api': { target: `http://127.0.0.1:${apiPort}`, ws: true },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2023',
    modulePreload: { polyfill: false },
    rolldownOptions: {
      output: {
        // Seiten und Sprachen sind dynamische Importe und bleiben von selbst
        // eigene Teile. Lit ändert sich selten und bekommt eine eigene Datei, die
        // der Browser über Releases hinweg behält; xterm lädt nur das Terminal.
        codeSplitting: {
          groups: [
            { name: 'lit', test: /node_modules[\\/](lit|@lit|lit-html|lit-element)[\\/]/ },
            { name: 'xterm', test: /node_modules[\\/]@xterm[\\/]/ },
          ],
        },
      },
    },
  },
  test: {
    environment: 'happy-dom',
    // Die Playwright-Tests unter e2e/ laufen über `npm run test:e2e` gegen das Binary.
    exclude: [...configDefaults.exclude, 'e2e/**'],
  },
});
