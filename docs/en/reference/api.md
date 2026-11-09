---
sidebar:
  order: 24
title: Frontend & API
description: The API client, the data store, live data, routing, input validation, and the dev workflow of the web interface.
---

This page describes the **frontend architecture** around the API. For the REST
API itself - authentication, permissions, example requests, and the full endpoint
reference - see the [REST API](/en/reference/rest-api/) page.

The web interface lives in `ui/`: Lit, TypeScript, and Vite, with its own
components and no CSS framework. The rules for new pages are in `ui/README.md`.

## Basic principle

Pages **never** call `fetch()` directly. Requests per domain live in `ui/src/api/`
(`servers.ts`, `groups.ts`, …) and go through the API client.

```ts
import { LiveData } from '../lib/live-data';

readonly #servers = new LiveData(this, {
  url: () => SERVERS_URL,
  topics: () => ['servers'],
});
```

`LiveData` delivers data straight from the tab's store, refreshes it in the
background, and reloads when an event for a subscribed topic arrives. There is no
polling. Changing calls go through `api.post/put/patch/delete`, followed by
`store.invalidate(prefix)`.

## What the API client handles centrally

`ui/src/lib/api.ts` is the only place with `fetch`:

- **Session:** the operator chooses under *Settings → General* how the browser
  carries it. By default (`memory`) the `session_token` of the sign-in lives only
  in a module variable of the tab and is sent as `X-LCM-Session` - never in
  `localStorage`, `sessionStorage`, IndexedDB, cache, or a service worker, and
  there is no cookie. A reload, a new tab, or closing it signs you out. In the
  cookie variant (HTTPS only) the session lives in the httpOnly cookie
  `__Host-lcm_session`, which no script can read.
- **CSRF:** only with the cookie session, every changing request carries the
  `X-CSRF-Token` header; a browser does not send the header of the memory
  variant by itself and does not need it.
- **Files and streams:** a link to a protected path would not carry the session
  of the memory variant. Downloads therefore go through `api.download` (fetch
  and blob), the live log through `api.stream` (SSE over fetch instead of
  `EventSource`), and the event channel fetches a one-time ticket before every
  connection (`POST /events/ticket`).
- **Normalized errors:** every error response becomes an `ApiError` with `status`,
  `code`, and `params`. `code` is the translation key `errors.<code>`; the text
  comes from `errorText(err)`.
- **Expired session:** a 401 leads to the login and back to the page afterwards.

## Permissions

`auth.can('servers:write')` hides actions. This is purely cosmetic; the binding
check is always done by the server's RBAC middleware.

## Input validation

The backend is the authority: it checks every input itself and responds with a
code that names the reason. The web interface reports the same thing earlier, at
the field (`ui/src/lib/validate.ts`).

## Routing

A custom router with the History API and real paths (`ui/src/lib/router.ts`). Pages
are listed in `ui/src/routes.ts` and load on first use. Everything that defines a
view is in the address: tabs as a path segment, filters, sorting, and page as a
query. If a page holds unsaved changes, the router asks before leaving.

## Dev workflow

```sh
make dev
```

starts the Go backend on `:9310` and Vite on `:5174` (with an `/api` proxy to the
backend). Changes to the web interface appear immediately; backend changes need a
restart of `make dev`.

Tests: `npm test` in `ui/` for the unit tests, `make test-e2e` for the Playwright
tests against the real binary.
