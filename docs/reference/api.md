---
sidebar:
  order: 24
title: Frontend & API
description: API-Service-Klassen, der zentrale ApiClient, der auth-Store, Svelte-5-Runes-Patterns, Routing und der Dev-Workflow.
---

Diese Seite beschreibt die **Frontend-Architektur** rund um die API
(Service-Klassen, Svelte-Patterns). Für die REST-API selbst - Authentifizierung,
Berechtigungen, Beispiel-Requests und die vollständige Endpunkt-Referenz -
siehe [REST-API](/reference/rest-api/).

## Grundprinzip

Svelte-Komponenten rufen **niemals** `fetch()` direkt auf. Stattdessen gibt es pro Backend-Ressource eine Service-Klasse in `frontend/src/api/`, gebündelt im zentralen `api`-Objekt:

```svelte
<script>
  import { api, ApiError } from '../api';

  let servers = $state([]);

  async function load() {
    servers = await api.servers.list();
  }
</script>
```

Verfügbar: `api.auth`, `api.users`, `api.linuxUsers`, `api.servers`, `api.groups`, `api.jobs`, `api.system`, `api.apiKeys`, `api.customActions`, `api.notifications`, `api.alerts` sowie `api.client` (reaktiver Zustand). Gemeinsame Formatierungs-Helfer (Zeitstempel, GB, CVE-Schweregrade) liegen in `frontend/src/lib/format.js`.

## Was der ApiClient zentral erledigt

`frontend/src/api/client.svelte.js` ist die einzige Stelle mit `fetch`:

- **JWT automatisch anhängen:** Ist eine Session aktiv, bekommt jeder Request den `Authorization: Bearer …`-Header. Komponenten wissen nichts von Tokens.
- **Fehler normalisieren:** Jede Fehlerantwort wird zu einer `ApiError` mit `status` und `message` (aus dem `{"error": "..."}`-Body des Backends).
- **Auto-Logout bei 401:** Antwortet der Server mit 401 (Token abgelaufen/ungültig), verwirft der Client die Session und leitet auf `/login` um - genau einmal implementiert, gilt überall.
- **Session-Persistenz:** Token + Profil liegen in `localStorage`; ein Seiten-Reload behält den Login.

Der Auth-Zustand ist ein `$state`-Feld in einer `.svelte.js`-Datei - dadurch sind `api.client.isLoggedIn`, `api.client.user` und `hasPermission()` **reaktiv**: UI, das davon liest, aktualisiert sich bei Login/Logout automatisch.

## Der auth-Store

Für Komponenten gibt es den schlanken Wrapper `frontend/src/stores/auth.svelte.js`:

```svelte
<script>
  import { auth } from '../stores/auth.svelte.js';
</script>

{#if auth.isLoggedIn}
  Hallo, {auth.user.display_name}!
{/if}

{#if auth.can('servers:write')}
  <button>Server hinzufügen</button>
{/if}
```

`auth.can()` ist reine UI-Kosmetik (Buttons ausblenden) - die verbindliche Prüfung macht immer der Server per RBAC-Middleware.

## Eine neue API-Klasse anlegen

Beispiel „Notes" (Backend-Routen existieren bereits, siehe [Architektur](/reference/architecture/)):

**1. Klasse** - `frontend/src/api/notes.js` (Vorlage: `alerts.js` oder `customactions.js`):

```js
export class NotesApi {
  #client;
  constructor(client) { this.#client = client; }

  getAll()        { return this.#client.get('/notes'); }
  create(title, body) { return this.#client.post('/notes', { title, body }); }
  remove(id)      { return this.#client.delete(`/notes/${id}`); }
}
```

**2. Registrieren** - in `frontend/src/api/index.js`:

```js
import { NotesApi } from './notes.js';

export const api = {
  // ...
  notes: new NotesApi(client),
};
```

**3. Verwenden** - `await api.notes.getAll()` in jeder Komponente.

## Svelte-5-Patterns des Templates

**Lokaler Zustand + Laden** (z.B. `pages/Dashboard.svelte`):

```svelte
<script>
  let servers = $state([]);
  let error = $state('');

  // $derived berechnet abgeleitete Werte automatisch neu:
  let reachableCount = $derived(servers.filter((s) => s.reachable).length);

  // $effect lädt initial und bei Änderung getrackter Abhängigkeiten:
  $effect(() => {
    void auth.isLoggedIn;   // Abhängigkeit: neu laden nach Login/Logout
    load();
  });
</script>
```

**Props + Callback nach oben** (z.B. `components/ReconnectWizard.svelte`):

```svelte
<script>
  let { server, open = $bindable(false), onDone = () => {} } = $props();
  // ... nach erfolgreichem Abschluss:
  onDone();
</script>
```

**Fehlerbehandlung:** immer `ApiError` abfangen und `e.status`/`e.message` anzeigen - so wird ein 403 („fehlende Berechtigung: servers:write") zur verständlichen UI-Meldung statt zu einer Konsolen-Exception.

## Routing

`svelte-spa-router` mit **Hash-Routing** (`/#/users`): funktioniert im Single-Binary ohne Server-Konfiguration und in `file://`-Kontexten. Routen sind in `App.svelte` definiert; neue Seite = neue Datei in `pages/` + ein Eintrag im `routes`-Objekt. Links mit `use:link`, programmatisch mit `push('/pfad')`.

## Dev-Workflow

```sh
make dev
```

startet das Go-Backend auf `:9310` und Vite auf `:5173` (mit `/api`-Proxy aufs Backend). Frontend-Änderungen erscheinen per Hot-Reload sofort; fürs finale Testen `make build` und das Binary starten.
