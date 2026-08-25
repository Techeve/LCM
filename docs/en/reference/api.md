---
sidebar:
  order: 24
title: Frontend & API
description: API service classes, the central ApiClient, the auth store, Svelte 5 runes patterns, routing, and the dev workflow.
---

This page describes the **frontend architecture** around the API (service
classes, Svelte patterns). For the REST API itself - authentication,
permissions, example requests, and the full endpoint reference - see the
[REST API](/en/reference/rest-api/) page.

## Basic principle

Svelte components **never** call `fetch()` directly. Instead there is one service class per backend resource in `frontend/src/api/`, bundled in the central `api` object:

```svelte
<script>
  import { api, ApiError } from '../api';

  let servers = $state([]);

  async function load() {
    servers = await api.servers.list();
  }
</script>
```

Available: `api.auth`, `api.users`, `api.linuxUsers`, `api.servers`, `api.groups`, `api.jobs`, `api.system`, `api.apiKeys`, `api.customActions`, `api.notifications`, `api.alerts`, plus `api.client` (reactive state). Shared formatting helpers (timestamps, GB, CVE severities) live in `frontend/src/lib/format.js`.

## What the ApiClient handles centrally

`frontend/src/api/client.svelte.js` is the only place with `fetch`:

- **Attach the JWT automatically:** if a session is active, every request gets the `Authorization: Bearer …` header. Components know nothing about tokens.
- **Normalize errors:** every error response becomes an `ApiError` with `status` and `message` (from the backend's `{"error": "..."}` body).
- **Auto-logout on 401:** if the server responds with 401 (token expired/invalid), the client discards the session and redirects to `/login` - implemented exactly once, applies everywhere.
- **Session persistence:** token + profile are stored in `localStorage`; a page reload keeps the login.

The auth state is a `$state` field in a `.svelte.js` file - which makes `api.client.isLoggedIn`, `api.client.user`, and `hasPermission()` **reactive**: UI that reads them updates automatically on login/logout.

## The auth store

For components there is the slim wrapper `frontend/src/stores/auth.svelte.js`:

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

`auth.can()` is pure UI cosmetics (hiding buttons) - the authoritative check is always done by the server via the RBAC middleware.

## Creating a new API class

Example "Notes" (the backend routes already exist, see [Architecture](/en/reference/architecture/)):

**1. Class** - `frontend/src/api/notes.js` (template: `alerts.js` or `customactions.js`):

```js
export class NotesApi {
  #client;
  constructor(client) { this.#client = client; }

  getAll()        { return this.#client.get('/notes'); }
  create(title, body) { return this.#client.post('/notes', { title, body }); }
  remove(id)      { return this.#client.delete(`/notes/${id}`); }
}
```

**2. Register** - in `frontend/src/api/index.js`:

```js
import { NotesApi } from './notes.js';

export const api = {
  // ...
  notes: new NotesApi(client),
};
```

**3. Use** - `await api.notes.getAll()` in any component.

## The template's Svelte 5 patterns

**Local state + loading** (e.g. `pages/Dashboard.svelte`):

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

**Props + callback upward** (e.g. `components/ReconnectWizard.svelte`):

```svelte
<script>
  let { server, open = $bindable(false), onDone = () => {} } = $props();
  // ... nach erfolgreichem Abschluss:
  onDone();
</script>
```

**Error handling:** always catch `ApiError` and show `e.status`/`e.message` - that turns a 403 ("fehlende Berechtigung: servers:write") into a comprehensible UI message instead of a console exception.

## Routing

`svelte-spa-router` with **hash routing** (`/#/users`): works in the single binary without server configuration and in `file://` contexts. Routes are defined in `App.svelte`; a new page = a new file in `pages/` + an entry in the `routes` object. Links with `use:link`, programmatically with `push('/pfad')`.

## Dev workflow

```sh
make dev
```

starts the Go backend on `:9310` and Vite on `:5173` (with an `/api` proxy to the backend). Frontend changes appear instantly via hot reload; for final testing run `make build` and start the binary.
