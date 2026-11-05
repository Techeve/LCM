// Prüft die Größe des Start-JavaScripts: alles, was index.html sofort lädt.
// Seiten und Reiter laden erst bei Bedarf und zählen hier nicht.

import { readFileSync } from 'node:fs';
import { gzipSync } from 'node:zlib';

const LIMIT_KB = 150;

const dist = new URL('../dist/', import.meta.url);
const html = readFileSync(new URL('index.html', dist), 'utf8');
const scripts = [...new Set(html.match(/assets\/[\w.-]+\.js/g) ?? [])];
if (scripts.length === 0) {
  console.error('check:bundle: keine Skripte in dist/index.html gefunden');
  process.exit(1);
}

const bytes = scripts.reduce((sum, file) => sum + gzipSync(readFileSync(new URL(file, dist)), { level: 9 }).length, 0);
const kb = (bytes / 1024).toFixed(1);
if (bytes > LIMIT_KB * 1024) {
  console.error(`check:bundle: Start-JavaScript ${kb} KB (gzip), erlaubt sind ${LIMIT_KB} KB`);
  process.exit(1);
}
console.log(`check:bundle: Start-JavaScript ${kb} KB (gzip) von ${LIMIT_KB} KB, ${scripts.length} Dateien`);
