// Linux-Benutzer, Berechtigungsprofile und Regelbausteine: Typen, Normalisierung der Antworten und alle
// ändernden Aufrufe. Lesen läuft über den Datenspeicher (`store.subscribe(url, …)`), Ändern über die
// `…Api`-Objekte; danach werden die betroffenen abonnierten Adressen neu geladen.

import { api } from '../lib/api';
import { store } from '../lib/store';
import { itemsOf } from './groups';

export const LINUX_USERS_URL = '/api/v1/linux-users';
export const PROFILES_URL = '/api/v1/privilege-profiles';
export const BLOCKS_URL = '/api/v1/profile-blocks';

export const userUrl = (id: number) => `${LINUX_USERS_URL}/${id}`;
export const assignmentsUrl = (id: number) => `${LINUX_USERS_URL}/${id}/group-assignments`;
export const blockUsageUrl = (id: number) => `${BLOCKS_URL}/${id}/usage`;

export const DEFAULT_SHELL = '/bin/bash';
export const FAMILIES = ['all', 'apt', 'dnf', 'zypper', 'pacman', 'apk'] as const;
export const PATH_MODES = ['read', 'readwrite', 'deny'] as const;
export type PathMode = (typeof PATH_MODES)[number];
export type AccountType = 'shell' | 'sftp';

export interface SshKey {
  id: number;
  name: string;
  public_key: string;
  created_at: string;
}

export interface UserServer {
  id: number;
  name: string;
  host: string;
  /** Der Benutzerabgleich ist für diesen Server abgeschaltet: LCM fasst ihn nicht an. */
  user_sync_disabled: boolean;
}

export interface UserGroupRef {
  id: number;
  name: string;
}

export interface LinuxUser {
  id: number;
  username: string;
  full_name: string;
  email: string;
  shell: string;
  active: boolean;
  default_profile_id: number | null;
  has_password: boolean;
  ssh_keys: SshKey[];
  servers: UserServer[];
  groups: UserGroupRef[];
  created_at: string;
  updated_at: string;
}

/** Profil je Gruppen-Zuweisung; `profile_id` null heißt: das Standardprofil des Benutzers gilt. */
export interface GroupAssignment {
  group_id: number;
  profile_id: number | null;
}

export interface SudoRule {
  command: string;
  run_as: string;
  require_password: boolean;
  allow_root_equivalent: boolean;
}

export interface PathRule {
  path: string;
  mode: PathMode;
}

export interface BlockUse {
  block_id: number;
  /** Parameterwerte, je Zeile `name=wert`. */
  values: string;
}

export interface Profile {
  id: number;
  name: string;
  slug: string;
  description: string;
  builtin: boolean;
  account_type: AccountType;
  grants_full_root: boolean;
  sudo_rules: SudoRule[];
  edit_rules: string[];
  path_rules: PathRule[];
  block_uses: BlockUse[];
}

export interface BlockVariant {
  family: string;
  run_as: string;
  sudo_commands: string;
  edit_paths: string;
  path_rules: string;
}

export interface Block {
  id: number;
  name: string;
  slug: string;
  description: string;
  name_en: string;
  description_en: string;
  /** Platzhalter, kommagetrennt (`service,path`). */
  params: string;
  builtin: boolean;
  variants: BlockVariant[];
}

type Raw = Record<string, unknown>;

function record(value: unknown): Raw {
  return value && typeof value === 'object' ? (value as Raw) : {};
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const count = (value: unknown): number => (typeof value === 'number' && Number.isFinite(value) ? value : 0);
const optionalId = (value: unknown): number | null => (typeof value === 'number' && value > 0 ? value : null);
const list = (value: unknown): Raw[] => (Array.isArray(value) ? value.map(record) : []);

function normalizeKey(raw: Raw): SshKey {
  return { id: count(raw.id), name: text(raw.name), public_key: text(raw.public_key), created_at: text(raw.created_at) };
}

function normalizeServer(raw: Raw): UserServer {
  return { id: count(raw.id), name: text(raw.name), host: text(raw.host), user_sync_disabled: raw.user_sync_disabled === true };
}

export function normalizeUser(raw: unknown): LinuxUser {
  const u = record(raw);
  return {
    id: count(u.id),
    username: text(u.username),
    full_name: text(u.full_name),
    email: text(u.email),
    shell: text(u.shell) || DEFAULT_SHELL,
    active: u.active !== false,
    default_profile_id: optionalId(u.default_profile_id),
    has_password: u.has_password === true,
    ssh_keys: list(u.ssh_keys).map(normalizeKey),
    servers: list(u.servers).map(normalizeServer),
    groups: list(u.groups).map((g) => ({ id: count(g.id), name: text(g.name) })),
    created_at: text(u.created_at),
    updated_at: text(u.updated_at),
  };
}

export const normalizeUsers = (raw: unknown): LinuxUser[] => itemsOf(raw).map(normalizeUser);

export function normalizeAssignments(raw: unknown): GroupAssignment[] {
  return itemsOf(raw)
    .map(record)
    .map((a) => ({ group_id: count(a.server_group_id), profile_id: optionalId(a.profile_id) }));
}

function pathMode(value: unknown): PathMode {
  return PATH_MODES.find((mode) => mode === value) ?? 'read';
}

export function normalizeProfile(raw: unknown): Profile {
  const p = record(raw);
  return {
    id: count(p.id),
    name: text(p.name),
    slug: text(p.slug),
    description: text(p.description),
    builtin: p.builtin === true,
    account_type: p.account_type === 'sftp' ? 'sftp' : 'shell',
    grants_full_root: p.grants_full_root === true,
    sudo_rules: list(p.sudo_rules).map((r) => ({
      command: text(r.command),
      run_as: text(r.run_as) || 'root',
      require_password: r.require_password === true,
      allow_root_equivalent: r.allow_root_equivalent === true,
    })),
    edit_rules: list(p.edit_rules).map((r) => text(r.path)),
    path_rules: list(p.path_rules).map((r) => ({ path: text(r.path), mode: pathMode(r.mode) })),
    block_uses: list(p.block_uses).map((u) => ({ block_id: count(u.block_id), values: text(u.values) })),
  };
}

export const normalizeProfiles = (raw: unknown): Profile[] => itemsOf(raw).map(normalizeProfile);

export function normalizeBlock(raw: unknown): Block {
  const b = record(raw);
  return {
    id: count(b.id),
    name: text(b.name),
    slug: text(b.slug),
    description: text(b.description),
    name_en: text(b.name_en),
    description_en: text(b.description_en),
    params: text(b.params),
    builtin: b.builtin === true,
    variants: list(b.variants).map((v) => ({
      family: text(v.family) || 'all',
      run_as: text(v.run_as),
      sudo_commands: text(v.sudo_commands),
      edit_paths: text(v.edit_paths),
      path_rules: text(v.path_rules),
    })),
  };
}

export const normalizeBlocks = (raw: unknown): Block[] => itemsOf(raw).map(normalizeBlock);

/** Namen der Profile, die einen Baustein verwenden (`GET /profile-blocks/:id/usage`). */
export function normalizeUsage(raw: unknown): string[] {
  const names = record(raw).profiles;
  return Array.isArray(names) ? names.filter((name): name is string => typeof name === 'string') : [];
}

export interface UserBody {
  full_name: string;
  email: string;
  shell: string;
  active: boolean;
  default_profile_id: number | null;
}

export interface CreateUserBody extends Omit<UserBody, 'active'> {
  username: string;
}

export interface ProfileBody {
  name: string;
  description: string;
  account_type: AccountType;
  sudo_rules: SudoRule[];
  edit_rules: Array<{ path: string }>;
  path_rules: PathRule[];
  block_uses: BlockUse[];
}

export interface BlockBody {
  name: string;
  description: string;
  name_en: string;
  description_en: string;
  params: string;
  variants: BlockVariant[];
}

export interface ActivationLink {
  token: string;
  expires_at: string;
  mailed: boolean;
  mail_error: string;
}

export interface DeprovisionResult {
  server_id: number;
  server: string;
  ok: boolean;
  message: string;
}

export interface GeneratedKey {
  private_key: string;
}

function changedUsers(): void {
  store.invalidate(LINUX_USERS_URL);
}

function changedProfiles(): void {
  store.invalidate(PROFILES_URL);
  store.invalidate(BLOCKS_URL);
}

async function after<T>(request: Promise<T>, invalidate: () => void): Promise<T> {
  const result = await request;
  invalidate();
  return result;
}

const user = (request: Promise<unknown>) => after(request, changedUsers);
const profile = (request: Promise<unknown>) => after(request, changedProfiles);

/** Ändernde Aufrufe für Benutzer. Sie laden nach Erfolg die Benutzerliste und den Detailstand neu. */
export const linuxUsersApi = {
  create: (body: CreateUserBody) => after(api.post<{ id: number }>(LINUX_USERS_URL, body), changedUsers),
  update: (id: number, body: UserBody) => user(api.patch(userUrl(id), body)),
  remove: (id: number) => user(api.delete(userUrl(id))),
  removeFromServers: (id: number) => after(api.post<{ results?: unknown }>(`${userUrl(id)}/remove-from-servers`, {}), changedUsers),
  addKey: (id: number, name: string, publicKey: string) => user(api.post(`${userUrl(id)}/keys`, { name, public_key: publicKey })),
  /** Der private Schlüssel kommt genau einmal in der Antwort und wird nirgends gespeichert. */
  generateKey: (id: number, name: string) => after(api.post<GeneratedKey>(`${userUrl(id)}/keys/generate`, { name }), changedUsers),
  removeKey: (keyId: number) => user(api.delete(`${LINUX_USERS_URL}/keys/${keyId}`)),
  assignGroup: (id: number, groupId: number, profileId: number | null) =>
    user(api.post(`${userUrl(id)}/assign-group`, { group_id: groupId, profile_id: profileId })),
  setGroupProfile: (id: number, groupId: number, profileId: number | null) =>
    user(api.post(`${userUrl(id)}/group-profile`, { group_id: groupId, profile_id: profileId })),
  removeGroup: (id: number, groupId: number) => user(api.post(`${userUrl(id)}/remove-group`, { group_id: groupId })),
  /** Der Link steht immer in der Antwort; scheitert der Versand, nennt `mail_error` den Grund. */
  generateActivation: (id: number, ttlHours: number, sendEmail: boolean) =>
    api.post<Raw>(`${userUrl(id)}/activation-links/generate`, { ttl_hours: ttlHours, send_email: sendEmail }).then(normalizeActivation),
  /** Gleicht den Benutzerbestand eines Servers ab (`servers:write`). */
  syncServer: (serverId: number) => api.post(`/api/v1/servers/${serverId}/sync-users`, {}),
};

export function normalizeActivation(raw: unknown): ActivationLink {
  const a = record(raw);
  return { token: text(a.token), expires_at: text(a.expires_at), mailed: a.mailed === true, mail_error: text(a.mail_error) };
}

export function normalizeDeprovision(raw: unknown): DeprovisionResult[] {
  return list(record(raw).results).map((r) => ({
    server_id: count(r.server_id),
    server: text(r.server),
    ok: r.ok === true,
    message: text(r.message),
  }));
}

export const profilesApi = {
  create: (body: ProfileBody & { slug: string }) => profile(api.post(PROFILES_URL, body)),
  update: (id: number, body: ProfileBody) => profile(api.patch(`${PROFILES_URL}/${id}`, body)),
  clone: (id: number, slug: string, name: string) => after(api.post<{ id: number }>(`${PROFILES_URL}/${id}/clone`, { slug, name }), changedProfiles),
  remove: (id: number) => profile(api.delete(`${PROFILES_URL}/${id}`)),
};

export const blocksApi = {
  create: (body: BlockBody & { slug: string }) => profile(api.post(BLOCKS_URL, body)),
  update: (id: number, body: BlockBody) => profile(api.patch(`${BLOCKS_URL}/${id}`, body)),
  clone: (id: number, slug: string, name: string) => after(api.post<{ id: number }>(`${BLOCKS_URL}/${id}/clone`, { slug, name }), changedProfiles),
  remove: (id: number) => profile(api.delete(`${BLOCKS_URL}/${id}`)),
  /** Rendert den Baustein für eine Familie, mit denselben Prüfungen wie beim Speichern. */
  preview: (id: number, family: string, values: string) =>
    api.post<{ lines?: unknown }>(`${BLOCKS_URL}/${id}/preview`, { family, values }).then((r) => normalizeLines(r.lines)),
};

function normalizeLines(raw: unknown): string[] {
  return Array.isArray(raw) ? raw.filter((line): line is string => typeof line === 'string') : [];
}
