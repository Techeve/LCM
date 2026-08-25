import { defineConfig } from '@playwright/test';

// E2E-Tests laufen gegen das ECHTE Go-Binary mit eingebettetem Frontend -
// getestet wird also genau das, was ausgeliefert wird.
// e2e/run-server.sh baut Frontend + Binary und startet es auf :8090
// mit deterministischer Test-Konfiguration (e2e/config.json).
//
// Der Port ist ueber LCM_E2E_PORT umstellbar. Gebraucht wird das, wenn zwei
// Arbeitsbaeume desselben Repos gleichzeitig testen (z.B. ein git-worktree
// neben dem Hauptbaum): Sonst haelt der erste Lauf den Port, und der zweite
// bricht mit „already used" ab, bevor ein einziger Test laeuft.
const PORT = Number(process.env.LCM_E2E_PORT || 8090);
const BASE = `http://127.0.0.1:${PORT}`;

// Jeder Worker bekommt eine EIGENE Server-Instanz mit eigener Datenbank auf
// einem eigenen Port. Vorher lief alles gegen eine einzige Instanz, weshalb
// nur ein Worker moeglich war - Tests haetten sich sonst gegenseitig den
// Zustand umgestellt. Mit getrennten Instanzen faellt dieser Grund weg.
//
// Zwei statt mehr: Die Runner haben vier Kerne und knapp 8 GB, und jede
// Instanz bringt einen Go-Prozess samt SQLite mit, dazu je ein Chromium.
// Ueber LCM_E2E_WORKERS umstellbar.
const WORKERS = Number(process.env.LCM_E2E_WORKERS || 2);

export default defineConfig({
  testDir: './e2e',
  // Die Tests eines Files duerfen auf verschiedene Worker verteilt werden -
  // moeglich, weil jeder Worker seine eigene Instanz hat.
  fullyParallel: true,
  workers: WORKERS,
  reporter: 'list',
  timeout: 30_000,
  // In der CI einzelne Tests bei Fehlschlag wiederholen (Standard-Praxis
  // gegen Rest-Flakes auf dem geteilten Runner); lokal sollen Fehler
  // sofort sichtbar bleiben.
  retries: process.env.CI ? 2 : 0,
  // Der geteilte CI-Runner ist unter Last spürbar langsamer als ein
  // Entwickler-Rechner - 5s Assertion-Frist war dort zu knapp (Flakes).
  expect: { timeout: 10_000 },
  use: {
    baseURL: BASE,
    trace: 'on-first-retry',
  },
  // Eine Instanz je Worker. run-server.sh legt pro Port ein eigenes
  // Arbeitsverzeichnis mit eigener Datenbank an.
  webServer: Array.from({ length: WORKERS }, (_, i) => ({
    // run-server.sh kompiliert das echte Go-Binary - auf einem kalten
    // CI-Cache dauert der Erst-Build (inkl. modernc.org/sqlite) mehrere
    // Minuten, daher großzügiges Timeout.
    command: `LCM_E2E_PORT=${PORT + i} sh e2e/run-server.sh`,
    url: `http://127.0.0.1:${PORT + i}/api/v1/health`,
    reuseExistingServer: false,
    timeout: 300_000,
  })),
});
