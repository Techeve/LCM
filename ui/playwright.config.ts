import { defineConfig } from '@playwright/test';

// E2E-Tests laufen gegen das ECHTE Go-Binary mit eingebetteter Oberfläche
// (`ui/dist` per `go:embed`), also genau gegen das, was ausgeliefert wird.
// e2e/run-server.sh baut das Binary und startet es mit Test-Konfiguration;
// `ui/dist` muss vorher gebaut sein (`npx vite build`).
//
// Der Port ist über LCM_E2E_PORT umstellbar. Gebraucht wird das, wenn zwei
// Arbeitsbäume desselben Repos gleichzeitig testen: Sonst hält der erste Lauf
// den Port, und der zweite bricht ab, bevor ein einziger Test läuft.
const PORT = Number(process.env.LCM_E2E_PORT || 8090);

// Jeder Worker bekommt eine EIGENE Server-Instanz mit eigener Datenbank auf
// einem eigenen Port, damit sich Tests nicht gegenseitig den Zustand umstellen.
//
// Zwei statt mehr: Die Runner haben vier Kerne und knapp 8 GB, und jede
// Instanz bringt einen Go-Prozess samt SQLite mit, dazu je ein Chromium.
// Über LCM_E2E_WORKERS umstellbar.
const WORKERS = Number(process.env.LCM_E2E_WORKERS || 2);

// WebKit nimmt Secure-Cookies nur über echtes HTTPS an. LCM_E2E_TLS=1 startet den
// Testserver deshalb wie im Betrieb mit HTTPS und eigenem Zertifikat.
const SCHEMA = process.env.LCM_E2E_TLS === '1' ? 'https' : 'http';

export default defineConfig({
  testDir: './e2e',
  // Die Tests einer Datei dürfen auf verschiedene Worker verteilt werden,
  // weil jeder Worker seine eigene Instanz hat.
  fullyParallel: true,
  workers: WORKERS,
  reporter: 'list',
  timeout: 30_000,
  // In der CI einzelne Tests bei Fehlschlag wiederholen (Rest-Flakes auf dem
  // geteilten Runner); lokal sollen Fehler sofort sichtbar bleiben.
  retries: process.env.CI ? 2 : 0,
  // Der geteilte CI-Runner ist unter Last spürbar langsamer als ein
  // Entwickler-Rechner, 5 s Assertion-Frist war dort zu knapp.
  expect: { timeout: 10_000 },
  use: {
    baseURL: `${SCHEMA}://127.0.0.1:${PORT}`,
    ignoreHTTPSErrors: true,
    // Über HTTPS läuft der Service Worker, und in WebKit gehen Anfragen dann an `page.route` vorbei.
    serviceWorkers: SCHEMA === 'https' ? 'block' : 'allow',
    // Die Texte der Tests sind deutsch; ohne Vorgabe richtet sich die Oberfläche nach dem Browser.
    locale: 'de-DE',
    // Die Suite nutzt die Cookie-Variante der Anmeldung, und die gibt es nur über HTTPS. Der Testserver
    // vertraut 127.0.0.1 als Proxy (run-server.sh), und jede Anfrage gibt sich als HTTPS aus; Chromium
    // nimmt die Secure-Cookies auf 127.0.0.1 an. Wer die Arbeitsspeicher-Variante prüft, nimmt den Kopf wieder weg.
    extraHTTPHeaders: { 'X-Forwarded-Proto': 'https' },
    trace: 'on-first-retry',
  },
  // Eine Instanz je Worker. run-server.sh legt pro Port ein eigenes
  // Arbeitsverzeichnis mit eigener Datenbank an.
  webServer: Array.from({ length: WORKERS }, (_, index) => ({
    // Auf einem kalten CI-Cache dauert der Erst-Build des Binarys (inkl.
    // modernc.org/sqlite) mehrere Minuten, daher das großzügige Timeout.
    command: `LCM_E2E_PORT=${PORT + index} sh e2e/run-server.sh`,
    url: `${SCHEMA}://127.0.0.1:${PORT + index}/api/v1/health`,
    ignoreHTTPSErrors: true,
    reuseExistingServer: false,
    timeout: 300_000,
  })),
});
