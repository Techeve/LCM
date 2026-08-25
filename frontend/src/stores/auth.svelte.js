/**
 * Globaler Auth-Store (Svelte 5 Runes).
 *
 * Dünner, reaktiver Wrapper um den ApiClient - Komponenten importieren
 * `auth` statt direkt am Client zu hängen:
 *
 *   import { auth } from '../stores/auth.svelte.js';
 *   {#if auth.isLoggedIn} Hallo {auth.user.display_name}! {/if}
 *   {#if auth.can('servers:write')} <button>Server hinzufügen</button> {/if}
 */
import { client } from '../api/client.svelte.js';

export const auth = {
  get isLoggedIn() {
    return client.isLoggedIn;
  },
  get user() {
    return client.user;
  },
  /** RBAC-Permission-Check fürs UI (Server prüft immer zusätzlich!). */
  can(permission) {
    return client.hasPermission(permission);
  },
};
