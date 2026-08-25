/**
 * Zentraler API-Einstiegspunkt.
 *
 * Verwendung in Komponenten:
 *   import { api } from '../api';
 *   const servers = await api.servers.list();
 *
 * Neue Feature-API registrieren:
 *   1. Klasse in eigener Datei anlegen (Vorlage: servers.js)
 *   2. Hier importieren und am api-Objekt anhängen.
 */
import { client, ApiError } from './client.svelte.js';
import { AuthApi } from './auth.js';
import { UsersApi } from './users.js';
import { ServersApi } from './servers.js';
import { GroupsApi } from './groups.js';
import { JobsApi } from './jobs.js';
import { SystemApi } from './system.js';
import { ApiKeysApi } from './apikeys.js';
import { LinuxUsersApi } from './linuxusers.js';
import { CustomActionsApi } from './customactions.js';
import { ProfilesApi } from './profiles.js';
import { ProfileBlocksApi } from './profileblocks.js';
import { AppsApi } from './apps.js';
import { NotificationsApi } from './notifications.js';
import { AlertsApi } from './alerts.js';
import { DocsApi } from './docs.js';

export const api = {
  /** Reaktiver Client-Zustand (isLoggedIn, user, hasPermission). */
  client,
  auth: new AuthApi(client),
  users: new UsersApi(client),
  linuxUsers: new LinuxUsersApi(client),
  servers: new ServersApi(client),
  groups: new GroupsApi(client),
  jobs: new JobsApi(client),
  system: new SystemApi(client),
  apiKeys: new ApiKeysApi(client),
  customActions: new CustomActionsApi(client),
  profiles: new ProfilesApi(client),
  profileBlocks: new ProfileBlocksApi(client),
  apps: new AppsApi(client),
  notifications: new NotificationsApi(client),
  alerts: new AlertsApi(client),
  docs: new DocsApi(client),
};

export { ApiError };
