// Einstellungen der Bereiche System und Zugang: globale Einstellungen, Subscription, Backups, Frühwarnung,
// Benutzer, Rollen, API-Schlüssel und MCP. Lesen läuft über den Datenspeicher
// (`store.subscribe(url, …)`), Ändern über `settingsApi`; danach werden die betroffenen Adressen neu geladen.
// Die weiteren Einstellungsbereiche stehen in `settings-more.ts`.

import { api } from '../lib/api';
import { store } from '../lib/store';

export const GLOBAL_URL = '/api/v1/settings/global';
export const MAIL_CHANNEL_URL = '/api/v1/settings/mail-channel';
export const SUBSCRIPTION_URL = '/api/v1/subscription';
export const BACKUPS_URL = '/api/v1/system/backups';
export const ADVISORY_STATUS_URL = '/api/v1/security/advisories/status';
export const USERS_URL = '/api/v1/users';
export const ROLES_URL = '/api/v1/roles';
export const API_KEYS_URL = '/api/v1/apikeys';

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function textOrNull(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null;
}

function numberOf(value: unknown, fallback = 0): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

function list(value: unknown): Raw[] {
  return Array.isArray(value) ? value.filter(isRecord) : [];
}

// ---- globale Einstellungen ----

/** Die globalen Einstellungen als flache Tabelle; nur Text, Zahlen und Schalter bleiben erhalten. */
export type GlobalSettings = Record<string, string | number | boolean>;

export function readGlobal(raw: unknown): GlobalSettings {
  const settings: GlobalSettings = {};
  if (!isRecord(raw)) return settings;
  for (const [key, value] of Object.entries(raw)) {
    if (typeof value === 'string' || typeof value === 'boolean' || (typeof value === 'number' && Number.isFinite(value))) settings[key] = value;
  }
  return settings;
}

export interface MailChannel {
  enabled: boolean;
}

export function readMailChannel(raw: unknown): MailChannel {
  return { enabled: isRecord(raw) && raw.enabled === true };
}

export interface AdvisoryStatus {
  enabled: boolean;
  localCopy: boolean;
  lastPollAt: string | null;
  mirroredAt: string | null;
  /** Systeme, deren Daten sich spiegeln lassen; leer, solange kein Server einen Paketbestand hat. */
  mirrorable: string[];
}

export function readAdvisoryStatus(raw: unknown): AdvisoryStatus {
  const r = isRecord(raw) ? raw : {};
  return {
    enabled: r.enabled === true,
    localCopy: r.local_copy === true,
    lastPollAt: textOrNull(r.last_poll_at),
    mirroredAt: textOrNull(r.mirrored_at),
    mirrorable: Array.isArray(r.mirrorable) ? r.mirrorable.filter((item): item is string => typeof item === 'string') : [],
  };
}

// ---- Subscription ----

export type AptChannel = 'community' | 'beta' | 'enterprise';
export const APT_CHANNELS: AptChannel[] = ['community', 'beta', 'enterprise'];

export interface Subscription {
  configured: boolean;
  instanceId: string;
  serviceUrl: string;
  customer: string;
  keyPrefix: string;
  status: string;
  expiresAt: string;
  daysLeft: number | null;
  lastCheckAt: string | null;
  lastError: string;
  includedServers: number;
  managedServers: number;
  quotaExceeded: boolean;
  aptChannel: AptChannel;
  aptAvailable: boolean;
}

export function readSubscription(raw: unknown): Subscription {
  const r = isRecord(raw) ? raw : {};
  const channel = APT_CHANNELS.find((c) => c === r.apt_channel) ?? 'community';
  return {
    configured: r.configured === true,
    instanceId: text(r.instance_id),
    serviceUrl: text(r.service_url),
    customer: text(r.customer),
    keyPrefix: text(r.key_prefix),
    status: text(r.status),
    expiresAt: text(r.expires_at),
    daysLeft: typeof r.days_left === 'number' ? r.days_left : null,
    lastCheckAt: textOrNull(r.last_check_at),
    lastError: text(r.last_error),
    includedServers: numberOf(r.included_servers),
    managedServers: numberOf(r.managed_servers),
    quotaExceeded: r.quota_exceeded === true,
    aptChannel: channel,
    aptAvailable: r.apt_available === true,
  };
}

/** Gilt der Vertrag und erlaubt damit den Enterprise-Paketkanal? */
export function enterpriseAllowed(subscription: Subscription): boolean {
  return subscription.configured && subscription.status === 'active';
}

// ---- Backups ----

export interface Backup {
  id: number;
  fileName: string;
  sizeBytes: number;
  trigger: string;
  encryption: 'passphrase' | 'recipients';
  createdAt: string | null;
}

export function readBackups(raw: unknown): Backup[] {
  return list(raw).map((r) => ({
    id: numberOf(r.id),
    fileName: text(r.file_name),
    sizeBytes: numberOf(r.size_bytes),
    trigger: text(r.trigger),
    encryption: r.encryption === 'recipients' ? 'recipients' : 'passphrase',
    createdAt: textOrNull(r.created_at),
  }));
}

export interface StagedRestore {
  restart: 'auto' | 'manual';
  message: string;
}

export function readStagedRestore(raw: unknown): StagedRestore {
  const r = isRecord(raw) ? raw : {};
  return { restart: r.restart === 'auto' ? 'auto' : 'manual', message: text(r.message) };
}

export interface BackupRecipientPair {
  publicKey: string;
  privateKey: string;
}

export interface BackupScheduleBody {
  enabled: boolean;
  interval_hours: number;
  retention: number;
  time: string;
  dir: string;
  auto_restart: boolean;
  passphrase: string;
  recipients: string;
}

// ---- Benutzer, Rollen, API-Schlüssel ----

export interface Role {
  id: number;
  name: string;
  description: string;
}

export function readRoles(raw: unknown): Role[] {
  return list(raw).map((r) => ({ id: numberOf(r.id), name: text(r.name), description: text(r.description) }));
}

export interface LoginUser {
  id: number;
  username: string;
  email: string;
  firstName: string;
  lastName: string;
  isSystem: boolean;
  active: boolean;
  totpEnabled: boolean;
  mustChangePassword: boolean;
  lastLoginAt: string | null;
  roles: string[];
}

export function readUsers(raw: unknown): LoginUser[] {
  return list(raw).map((r) => ({
    id: numberOf(r.id),
    username: text(r.username),
    email: text(r.email),
    firstName: text(r.first_name),
    lastName: text(r.last_name),
    isSystem: r.is_system === true,
    active: r.active !== false,
    totpEnabled: r.totp_enabled === true,
    mustChangePassword: r.must_change_password === true,
    lastLoginAt: textOrNull(r.last_login_at),
    roles: list(r.roles).map((role) => text(role.name)),
  }));
}

export interface NewUserBody {
  username: string;
  email: string;
  first_name: string;
  last_name: string;
  password: string;
  roles: string[];
}

export interface ProfileBody {
  email: string;
  first_name: string;
  last_name: string;
  current_password?: string;
}

export interface ActivationLink {
  token: string;
  expiresAt: string | null;
  mailed: boolean;
  mailError: string;
}

export function readActivationLink(raw: unknown): ActivationLink {
  const r = isRecord(raw) ? raw : {};
  return { token: text(r.token), expiresAt: textOrNull(r.expires_at), mailed: r.mailed === true, mailError: text(r.mail_error) };
}

export type KeyScope = 'read' | 'readwrite' | 'mcp';

export interface ApiKey {
  id: number;
  name: string;
  prefix: string;
  scope: KeyScope;
  /** Der Benutzer, in dessen Rechten der Schlüssel handelt. */
  userId: number;
  lastUsedAt: string | null;
  expiresAt: string | null;
  revoked: boolean;
  createdAt: string | null;
}

function readKey(r: Raw): ApiKey {
  const scope: KeyScope = r.scope === 'read' || r.scope === 'mcp' ? r.scope : 'readwrite';
  return {
    id: numberOf(r.id),
    name: text(r.name),
    prefix: text(r.prefix),
    scope,
    userId: numberOf(r.user_id),
    lastUsedAt: textOrNull(r.last_used_at),
    expiresAt: textOrNull(r.expires_at),
    revoked: r.revoked === true,
    createdAt: textOrNull(r.created_at),
  };
}

export function readApiKeys(raw: unknown): ApiKey[] {
  return list(raw).map(readKey);
}

export interface CreatedKey {
  /** Der Klartext; nur in dieser Antwort enthalten. */
  secret: string;
  key: ApiKey;
}

export function readCreatedKey(raw: unknown): CreatedKey {
  const r = isRecord(raw) ? raw : {};
  return { secret: text(r.key), key: readKey(isRecord(r.api_key) ? r.api_key : {}) };
}

// ---- ändernde Aufrufe ----

const userUrl = (id: number) => `${USERS_URL}/${id}`;

function refreshGlobal(): void {
  store.invalidate(GLOBAL_URL);
}

export const settingsApi = {
  async patchGlobal(body: Record<string, unknown>): Promise<GlobalSettings> {
    const updated = readGlobal(await api.patch<unknown>(GLOBAL_URL, body));
    refreshGlobal();
    return updated;
  },

  async setMailChannel(enabled: boolean): Promise<void> {
    await api.put(MAIL_CHANNEL_URL, { enabled });
    store.invalidate(MAIL_CHANNEL_URL);
  },

  /** Schickt eine Testnachricht mit den gespeicherten Mail-Einstellungen an die Admin-Empfänger. */
  testMail: () => api.post('/api/v1/settings/global/test-mail'),

  async setMcp(body: { enabled: boolean; bind_host: string; port: number }): Promise<void> {
    await api.put('/api/v1/settings/mcp', body);
    refreshGlobal();
  },

  async activateSubscription(subscriptionKey: string, serviceUrl: string): Promise<void> {
    await api.post(`${SUBSCRIPTION_URL}/activate`, { subscription_key: subscriptionKey, service_url: serviceUrl });
    store.invalidate(SUBSCRIPTION_URL);
  },

  async verifySubscription(): Promise<Subscription> {
    const status = readSubscription(await api.post<unknown>(`${SUBSCRIPTION_URL}/verify`));
    store.invalidate(SUBSCRIPTION_URL);
    return status;
  },

  async removeSubscription(): Promise<void> {
    await api.delete(SUBSCRIPTION_URL);
    store.invalidate(SUBSCRIPTION_URL);
  },

  /** Stellt den Paketkanal des LCM-Hosts um; die Antwort ist der Job, der das erledigt. */
  async switchAptChannel(channel: AptChannel): Promise<{ jobId: string }> {
    const job = await api.post<unknown>(`${SUBSCRIPTION_URL}/apt`, { channel });
    return { jobId: isRecord(job) ? text(job.id) : '' };
  },

  async createBackup(passphrase: string): Promise<void> {
    await api.post(`${BACKUPS_URL}/trigger-now`, passphrase ? { passphrase } : undefined);
    store.invalidate(BACKUPS_URL);
  },

  async configureBackups(body: BackupScheduleBody): Promise<void> {
    await api.patch(`${BACKUPS_URL}/settings`, body);
    refreshGlobal();
  },

  /** Die Sicherungsdatei; per fetch, damit die Sitzung mitgeht (ein Link trüge sie nicht). */
  downloadBackup(name: string): Promise<Blob> {
    return api.download(`${BACKUPS_URL}/${encodeURIComponent(name)}/download`);
  },

  async deleteBackup(name: string): Promise<void> {
    await api.delete(`${BACKUPS_URL}/${encodeURIComponent(name)}`);
    store.invalidate(BACKUPS_URL);
  },

  async generateRecipient(): Promise<BackupRecipientPair> {
    const raw = await api.post<unknown>(`${BACKUPS_URL}/recipients/generate`);
    const r = isRecord(raw) ? raw : {};
    return { publicKey: text(r.public_key), privateKey: text(r.private_key) };
  },

  /** Bereitet die Wiederherstellung aus einem Backup der Liste vor. */
  async restoreBackup(name: string, passphrase: string): Promise<StagedRestore> {
    return readStagedRestore(await api.post<unknown>(`${BACKUPS_URL}/${encodeURIComponent(name)}/restore`, { passphrase }));
  },

  /** Bereitet die Wiederherstellung aus einer hochgeladenen Datei vor. */
  async restoreUpload(file: File, passphrase: string): Promise<StagedRestore> {
    const form = new FormData();
    form.append('archive', file);
    form.append('passphrase', passphrase);
    return readStagedRestore(await api.post<unknown>(`${BACKUPS_URL}/restore-upload`, form));
  },

  /** Startet das Spiegeln der lokalen Frühwarn-Daten; die Antwort nennt den Job. */
  async mirrorAdvisories(): Promise<{ jobId: string }> {
    const raw = await api.post<unknown>('/api/v1/security/advisories/mirror', {});
    return { jobId: isRecord(raw) ? text(raw.job_id) : '' };
  },

  async createUser(body: NewUserBody): Promise<void> {
    await api.post(USERS_URL, body);
    store.invalidate(USERS_URL);
  },

  async updateProfile(id: number, body: ProfileBody): Promise<void> {
    await api.patch(`${userUrl(id)}/profile`, body);
    store.invalidate(USERS_URL);
  },

  async updateRoles(id: number, roles: string[]): Promise<void> {
    await api.put(`${userUrl(id)}/roles`, { roles });
    store.invalidate(USERS_URL);
  },

  /** Setzt das Passwort eines anderen Benutzers; dessen Sitzungen enden. */
  async resetPassword(id: number, password: string): Promise<void> {
    await api.post(`${userUrl(id)}/reset-password`, { password });
    store.invalidate(USERS_URL);
  },

  async setActive(id: number, active: boolean): Promise<void> {
    await api.patch(`${userUrl(id)}/active`, { active });
    store.invalidate(USERS_URL);
  },

  async deleteUser(id: number): Promise<void> {
    await api.delete(userUrl(id));
    store.invalidate(USERS_URL);
  },

  async createActivationLink(userId: number, sendEmail: boolean): Promise<ActivationLink> {
    const raw = await api.post<unknown>(`${USERS_URL}/activation-links/generate`, { user_id: userId, ttl_hours: 0, send_email: sendEmail });
    return readActivationLink(raw);
  },

  async createApiKey(name: string, scope: KeyScope, expiresInDays: number | null): Promise<CreatedKey> {
    const body: Record<string, unknown> = { name, scope };
    if (expiresInDays !== null) body.expires_in_days = expiresInDays;
    const created = readCreatedKey(await api.post<unknown>(API_KEYS_URL, body));
    store.invalidate(API_KEYS_URL);
    return created;
  },

  async revokeApiKey(id: number): Promise<void> {
    await api.delete(`${API_KEYS_URL}/${id}`);
    store.invalidate(API_KEYS_URL);
  },
};
