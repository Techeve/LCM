// Legt Brotli- und gzip-Fassungen neben die gehashten Dateien. Sie werden ins
// Binary eingebettet: So kostet die Kompression nur einmal beim Bauen, nicht bei
// jeder Anfrage, und auch die stärkste Brotli-Stufe ist bezahlbar.

import { readFile, readdir, writeFile } from 'node:fs/promises';
import { resolve } from 'node:path';
import { promisify } from 'node:util';
import { brotliCompress, constants, gzip } from 'node:zlib';
import type { Plugin } from 'vite';

const brotli = promisify(brotliCompress);
const gz = promisify(gzip);

const COMPRESSIBLE = /\.(js|css|svg|json)$/;
/** Darunter sparen die Kopfzeilen mehr als die Kompression. */
const MIN_BYTES = 1024;

export function compressible(fileName: string, size: number): boolean {
  return COMPRESSIBLE.test(fileName) && size >= MIN_BYTES;
}

async function compressFile(path: string): Promise<void> {
  const data = await readFile(path);
  if (!compressible(path, data.length)) return;
  await writeFile(`${path}.br`, await brotli(data, { params: { [constants.BROTLI_PARAM_QUALITY]: constants.BROTLI_MAX_QUALITY } }));
  await writeFile(`${path}.gz`, await gz(data, { level: constants.Z_BEST_COMPRESSION }));
}

export function compressPlugin(): Plugin {
  let assets = '';

  return {
    name: 'lcm-compress',
    apply: 'build',
    configResolved(config) {
      assets = resolve(config.root, config.build.outDir, 'assets');
    },
    async closeBundle(error) {
      if (error) return;
      const names = await readdir(assets);
      await Promise.all(names.map((name) => compressFile(resolve(assets, name))));
    },
  };
}
