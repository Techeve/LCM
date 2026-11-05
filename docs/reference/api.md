---
sidebar:
  order: 24
title: Frontend & API
description: Der API-Client, der Datenspeicher, Live-Daten, Routing, die Eingabeprüfung und der Dev-Workflow der Oberfläche.
---

Diese Seite beschreibt die **Frontend-Architektur** rund um die API. Für die
REST-API selbst - Authentifizierung, Berechtigungen, Beispiel-Requests und die
vollständige Endpunkt-Referenz - siehe [REST-API](/reference/rest-api/).

Die Oberfläche liegt in `ui/`: Lit, TypeScript und Vite, eigene Komponenten, kein
CSS-Framework. Die Regeln für neue Seiten stehen in `ui/README.md`.

## Grundprinzip

Seiten rufen **niemals** `fetch()` direkt auf. Abrufe je Fachbereich liegen in
`ui/src/api/` (`servers.ts`, `groups.ts` …) und gehen über den API-Client.

```ts
import { LiveData } from '../lib/live-data';

readonly #servers = new LiveData(this, {
  url: () => SERVERS_URL,
  topics: () => ['servers'],
});
```

`LiveData` liefert die Daten sofort aus dem Speicher des Tabs, frischt sie im
Hintergrund auf und lädt neu, wenn ein Ereignis der abonnierten Themen eintrifft.
Abfragen im Takt gibt es nicht. Ändernde Aufrufe gehen über
`api.post/put/patch/delete`, danach `store.invalidate(prefix)`.

## Was der API-Client zentral erledigt

`ui/src/lib/api.ts` ist die einzige Stelle mit `fetch`:

- **Sitzung:** Der Betreiber wählt unter *Einstellungen → Allgemein*, wie der
  Browser sie trägt. Im Standard (`memory`) liegt der `session_token` der
  Anmeldung nur in einer Modulvariable des Tabs und geht als `X-LCM-Session`
  mit - nie in `localStorage`, `sessionStorage`, IndexedDB, Cache oder Service
  Worker, und es gibt kein Cookie. Neu laden, ein neuer Tab oder Schließen
  melden ab. In der Cookie-Variante (nur HTTPS) liegt die Sitzung im
  httpOnly-Cookie `__Host-lcm_session`, das kein Skript auslesen kann.
- **CSRF:** Nur bei der Cookie-Sitzung trägt jede ändernde Anfrage den Header
  `X-CSRF-Token`; die Kopfzeile der Arbeitsspeicher-Variante schickt ein
  Browser nicht von selbst und braucht ihn nicht.
- **Dateien und Ströme:** Ein Link auf einen geschützten Pfad trüge die Sitzung
  der Arbeitsspeicher-Variante nicht mit. Downloads laufen deshalb über
  `api.download` (fetch und Blob), das Live-Log über `api.stream` (SSE per
  fetch statt `EventSource`), und der Ereigniskanal holt vor jedem
  Verbindungsaufbau eine Einmal-Fahrkarte (`POST /events/ticket`).
- **Fehler normalisieren:** Jede Fehlerantwort wird zu einem `ApiError` mit
  `status`, `code` und `params`. `code` ist der Übersetzungsschlüssel
  `errors.<code>`, angezeigt wird der Text über `errorText(err)`.
- **Abgelaufene Sitzung:** Ein 401 führt zur Anmeldung, danach zurück zur Seite.

## Rechte

`auth.can('servers:write')` blendet Aktionen aus. Das ist reine UI-Kosmetik, die
verbindliche Prüfung macht immer der Server per RBAC-Middleware.

## Eingabeprüfung

Das Backend ist die Autorität: Es prüft jede Eingabe selbst und antwortet mit
einem Code, der den Grund nennt. Die Oberfläche meldet dasselbe früher und am Feld
(`ui/src/lib/validate.ts`).

## Routing

Eigener Router mit History API und echten Pfaden (`ui/src/lib/router.ts`), die
Seiten stehen in `ui/src/routes.ts` und werden erst beim Aufruf geladen. Alles, was
eine Ansicht ausmacht, steht in der Adresse: Reiter als Pfadsegment, Filter,
Sortierung und Seite als Query. Hält eine Seite ungespeicherte Änderungen, fragt
der Router vor dem Wechsel nach.

## Dev-Workflow

```sh
make dev
```

startet das Go-Backend auf `:9310` und Vite auf `:5174` (mit `/api`-Proxy aufs
Backend). Änderungen an der Oberfläche erscheinen sofort, Backend-Änderungen
brauchen einen Neustart von `make dev`.

Tests: `npm test` in `ui/` für die Unit-Tests, `make test-e2e` für die
Playwright-Tests gegen das echte Binary.
