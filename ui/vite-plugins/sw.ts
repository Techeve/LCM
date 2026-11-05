// Baut den Service Worker als eigene, ungehashte Datei /sw.js und setzt die
// gehashten Dateien des aktuellen Builds samt Build-Kennung ein. Die Kennung
// ändert sich genau dann, wenn sich eine Datei ändert.

import { createHash } from 'node:crypto';
import { resolve } from 'node:path';
import { build, type Plugin } from 'vite';

/** URLs der gehashten Build-Dateien (Skripte, Styles, Schriften), ohne Source Maps. */
export function precacheAssets(fileNames: string[]): string[] {
  return fileNames
    .filter((name) => name.startsWith('assets/') && !name.endsWith('.map'))
    .map((name) => `/${name}`)
    .sort();
}

export function buildId(assets: string[]): string {
  return createHash('sha256').update(assets.join('\n')).digest('hex').slice(0, 12);
}

export function swPlugin(): Plugin {
  let root = '';
  let outDir = '';
  let assets: string[] = [];

  return {
    name: 'lcm-service-worker',
    apply: 'build',
    configResolved(config) {
      root = config.root;
      outDir = resolve(config.root, config.build.outDir);
    },
    generateBundle(_options, bundle) {
      assets = precacheAssets(Object.keys(bundle));
    },
    async closeBundle(error) {
      if (error) return;
      await build({
        configFile: false,
        root,
        logLevel: 'warn',
        define: { __ASSETS__: JSON.stringify(assets), __BUILD_ID__: JSON.stringify(buildId(assets)) },
        build: {
          outDir,
          emptyOutDir: false,
          copyPublicDir: false,
          target: 'es2023',
          rolldownOptions: {
            input: resolve(root, 'src/sw.ts'),
            output: { format: 'iife', entryFileNames: 'sw.js' },
          },
        },
      });
    },
  };
}
