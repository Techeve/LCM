# LCM-Oberfläche (`ui/`)

Die Oberfläche von LCM (Plan: `doc/plan-2026-10-31-b-frontend-neubau.md`).
Lit, TypeScript, Vite, eigene Komponenten, kein CSS-Framework.

```bash
npm run dev            # Vite auf :5174, /api → Backend auf :9310
npm run build          # tsc + Produktionsbau nach dist/
npm test               # vitest
npm run check:locales  # Übersetzungen vollständig, kein fest eingetragener Text
npm run test:e2e       # Playwright gegen das echte Binary (vorher npm run build)
```

Komponentenkatalog mit allen Bausteinen und Testdaten: `/catalog.html` (nur im
Entwicklungsmodus).

## Aufbau

| Verzeichnis | Inhalt |
|---|---|
| `src/lib/` | Router, i18n, API-Client, Datenspeicher je Tab, Ereigniskanal, Auth, Theme, Palette, Tastenkürzel |
| `src/components/` | wiederverwendbare `lcm-*`-Bausteine; Texte nur über Eigenschaften und Slots |
| `src/shell/` | Rahmen der Anwendung (Hauptmenü, Palette, Hinweise) |
| `src/pages/` | Seiten, je Bereich ein Unterverzeichnis (`pages/servers/` …) |
| `src/api/` | Typen und Abrufe je Fachbereich (`api/servers.ts` …) |
| `src/locales/` | eine JSON-Datei je Sprache |
| `src/styles/` | Tokens, Schriften, Grundstil |

## Regeln für Seiten

- **Seite = Custom Element** `lcm-page-<name>`, lazy in `src/routes.ts`
  eingetragen (`{ path, tag, load }`). Pfadteile aus `router.state.match.params`,
  Query aus `router.state.query`, Änderungen über
  `new SubscribeController(this, (l) => router.onChange(l))`.
- **Alles, was die Ansicht ausmacht, steht in der URL**: Reiter, Filter,
  Sortierung, Seite, offenes Fenster (`router.setQuery({...}, { replace })`).
- **Daten** über `store.subscribe(url, onData, onError)` bzw. `store.get(url)`:
  sofort aus dem Speicher, im Hintergrund aufgefrischt. Ändernde Aufrufe über
  `api.post/put/patch/delete`, danach `store.invalidate(prefix)`.
- **Live** über `events.subscribe(['server:42'])` (Rückgabe = Abmelden beim
  Verlassen). Ereignisse heißen nur „hat sich geändert“, Details per REST.
  Die Zuordnung Ereignis → ungültige URLs steht in `lib/live.ts`.
- **Kein Abfragen im Takt.**
- **Texte** nur über `t('bereich.schluessel', params)`; Fehler über
  `errorText(err)`. Schlüssel nach Bereich (`servers.packages.upgrade.label`),
  Hilfe nach Muster `….help` (Erklärblase), `….on`/`….off` (Schalter),
  `….confirm` (Bestätigung), `….explainer` (Erklärspalte). Deutsch in Du-Form,
  kurz, Folge vor Technik. Datum, Zahl, Dauer über `lib/format.ts`.
- **Erklärungen**: Server-Aktionen immer als `lcm-server-action` mit `help`
  und Vorschau aus `GET /api/v1/servers/:id/actions/:action/preview`
  (beim Öffnen der Blase laden). Komplexe Seiten bekommen eine `lcm-explainer`.
  Kein Text unter Knöpfen, keine Erklärung für Selbsterklärendes.
- **Bestätigungen** mit `lcm-dialog` und `impacts` (welche Server, was läuft,
  Ausfallzeit, umkehrbar?); zerstörend mit getipptem Namen (`expect`).
- **Leerzustände** erklären die Seite und den ersten Schritt
  (`lcm-empty-state`), Laden mit `lcm-skeleton`.
- **CSS**: nur Tokens aus `src/styles/tokens.css`, nur logische Eigenschaften
  (`margin-inline-start`), keine Inline-Styles, kein `<style>` im Template
  (CSP `style-src 'self'`). Icons als Inline-SVG (`lcm-icon`), keine Emoji.
- **Palette**: wenige Kernfunktionen der Seite mit `palette.register(this, [...])`.
- **Ungespeichertes** mit `unsaved.set(this, dirty)`, bei `lcm-save-bar` geschieht
  das über `dirty`. Der Router fragt dann nach, bevor ein Benutzer die Seite
  wechselt. Strg/Cmd+S über das Ereignis `lcm-save`.
- **Rechte**: `auth.can('servers:write')` blendet Aktionen aus, das Backend
  prüft trotzdem.
- **Anmeldung im Browser** (Einstellung `browser_session_mode`, siehe
  `docs/reference/security-model.md`): Standard ist der Arbeitsspeicher, die
  Sitzung gilt nur im Tab und geht verloren bei Neuladen, neuem Tab und
  Schließen. Daraus folgt für den Code:
  - Den `session_token` hält nur `lib/api.ts` in einer Modulvariable, nie in
    `localStorage`, `sessionStorage`, IndexedDB, Cache oder Service Worker.
  - Kein Link (`href`) auf einen geschützten API-Pfad und kein `EventSource`:
    sie trügen die Kopfzeile nicht mit. Downloads über `api.download` und
    `lib/download.ts`, Ströme über `api.stream`.
  - Kein `location.reload()` als Antwort auf einen Fehler, wo sich Daten oder
    ein Modul neu laden lassen. Nur ein Versionswechsel lädt neu (der Benutzer
    meldet sich dann neu an und kommt zur selben Adresse zurück).
  - WebSockets verbinden mit einer Einmal-Fahrkarte (`lib/events.ts`).
  - Die E2E-Suite läuft auf der Cookie-Variante: Der Testserver vertraut
    127.0.0.1 als Proxy, Playwright schickt `X-Forwarded-Proto: https`, die
    Fixture stellt die Instanz auf „cookie“. Die Arbeitsspeicher-Variante prüft
    `e2e/anmeldung-varianten.spec.ts`.
