// Gruppen, Zeitpläne und Regeln: Typen, Normalisierung der Antworten und alle ändernden Aufrufe.
// Lesen läuft über den Datenspeicher (`store.subscribe(url, …)`), Ändern über `groupsApi`; danach
// werden alle abonnierten Gruppen-Adressen neu geladen.

import { api } from '../lib/api';
import { store } from '../lib/store';

export const GROUPS_URL = '/api/v1/server-groups';
export const ALL_RULES_URL = '/api/v1/rules';
export const SERVERS_URL = '/api/v1/servers';
export const SERVER_STATUSES_URL = '/api/v1/servers/statuses';
export const USERS_URL = '/api/v1/users';
export const CUSTOM_ACTIONS_URL = '/api/v1/custom-actions';
export const IP_ALLOWLISTS_URL = '/api/v1/ip-allowlists';
export const APT_CACHES_URL = '/api/v1/apt-caches';

export const DEFAULT_PRIORITY = 100;
export const MAX_PRIORITY = 9999;
export const MAX_SPREAD_MINUTES = 1439;

export const scheduleUrl = (groupId: number) => `${GROUPS_URL}/${groupId}/schedules`;
export const groupUrl = (groupId: number) => `${GROUPS_URL}/${groupId}`;

export interface GroupServer {
  id: number;
  name: string;
  host: string;
  os_id: string;
  os_name: string;
  proxmox_type: string;
  hardware_model: string;
}

export interface GroupManager {
  id: number;
  username: string;
  email: string;
  first_name: string;
  last_name: string;
}

export interface Rule {
  id: number;
  group_id: number;
  group_name: string;
  schedule_id: number | null;
  /** Läuft der Zeitplan einer geplanten Regel? Nur `/api/v1/rules` liefert es; ohne Angabe gilt: ja. */
  schedule_enabled: boolean;
  enforce: boolean;
  position: number;
  name: string;
  type: string;
  command: string;
  enabled: boolean;
  is_system: boolean;
}

export interface Schedule {
  id: number;
  group_id: number;
  name: string;
  cron_expr: string;
  enabled: boolean;
  spread_minutes: number;
  is_system: boolean;
  rules: Rule[];
}

export interface Group {
  id: number;
  name: string;
  description: string;
  is_system: boolean;
  priority: number;
  acl_servers: number;
  acl_capable: number;
  servers: GroupServer[];
  managers: GroupManager[];
  /** Regeln der Gruppe; nur im Detail vorhanden. */
  rules: Rule[];
  server_count: number;
  /** `null`, solange unbekannt: die Liste nennt Verwalter und Zeitpläne (noch) nicht. */
  manager_count: number | null;
  schedule_count: number | null;
  /** Nur die Listenantwort mit Mitgliedern kennt die Server-IDs; sonst leer. */
  has_members: boolean;
  created_at: string;
  updated_at: string;
}

export interface ServerOption {
  id: number;
  name: string;
  host: string;
  os_id: string;
}

export interface UserOption {
  id: number;
  username: string;
  first_name: string;
  last_name: string;
}

export interface CustomAction {
  id: number;
  name: string;
}

export interface IpAllowlist {
  id: number;
  name: string;
}

export interface AptCache {
  id: number;
  name: string;
  url: string;
}

type Raw = Record<string, unknown>;

function record(value: unknown): Raw {
  return value && typeof value === 'object' ? (value as Raw) : {};
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const count = (value: unknown, fallback = 0): number => (typeof value === 'number' && Number.isFinite(value) ? value : fallback);
const list = (value: unknown): Raw[] => (Array.isArray(value) ? value.map(record) : []);

/** Die Listen-Endpunkte liefern eine Liste oder `{ items, total }`; beides wird hier zur Liste. */
export function itemsOf(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw;
  const items = record(raw).items;
  return Array.isArray(items) ? items : [];
}

export function normalizeRule(raw: unknown): Rule {
  const r = record(raw);
  return {
    id: count(r.id),
    group_id: count(r.group_id),
    group_name: text(r.group_name),
    schedule_id: typeof r.schedule_id === 'number' ? r.schedule_id : null,
    schedule_enabled: r.schedule_enabled !== false,
    enforce: r.enforce === true,
    position: count(r.position),
    name: text(r.name),
    type: text(r.type),
    command: text(r.command),
    enabled: r.enabled !== false,
    is_system: r.is_system === true,
  };
}

export function normalizeRules(raw: unknown): Rule[] {
  return itemsOf(raw).map(normalizeRule);
}

export function normalizeSchedule(raw: unknown): Schedule {
  const s = record(raw);
  return {
    id: count(s.id),
    group_id: count(s.group_id),
    name: text(s.name),
    cron_expr: text(s.cron_expr),
    enabled: s.enabled !== false,
    spread_minutes: count(s.spread_minutes),
    is_system: s.is_system === true,
    rules: list(s.rules).map(normalizeRule),
  };
}

export function normalizeSchedules(raw: unknown): Schedule[] {
  return itemsOf(raw).map(normalizeSchedule);
}

function normalizeServer(raw: Raw): GroupServer {
  return {
    id: count(raw.id),
    name: text(raw.name),
    host: text(raw.host),
    os_id: text(raw.os_id),
    os_name: text(raw.os_name),
    proxmox_type: text(raw.proxmox_type),
    hardware_model: text(raw.hardware_model),
  };
}

function normalizeManager(raw: Raw): GroupManager {
  return {
    id: count(raw.id),
    username: text(raw.username),
    email: text(raw.email),
    first_name: text(raw.first_name),
    last_name: text(raw.last_name),
  };
}

/** Eine Gruppe aus Listen- oder Detailantwort. Zähler, die der Server selbst nennt, gehen vor den Listen. */
export function normalizeGroup(raw: unknown): Group {
  const g = record(raw);
  const servers = list(g.servers).map(normalizeServer);
  const managers = list(g.managers).map(normalizeManager);
  const hasMembers = Array.isArray(g.servers);
  const serverCount = typeof g.server_count === 'number' ? g.server_count : servers.length;
  const managerCount = typeof g.manager_count === 'number' ? g.manager_count : Array.isArray(g.managers) ? managers.length : null;
  return {
    id: count(g.id),
    name: text(g.name),
    description: text(g.description),
    is_system: g.is_system === true,
    priority: count(g.priority, DEFAULT_PRIORITY),
    acl_servers: count(g.acl_servers, serverCount),
    acl_capable: count(g.acl_capable),
    servers,
    managers,
    rules: list(g.rules).map(normalizeRule),
    server_count: serverCount,
    manager_count: managerCount,
    schedule_count: typeof g.schedule_count === 'number' ? g.schedule_count : null,
    has_members: hasMembers,
    created_at: text(g.created_at),
    updated_at: text(g.updated_at),
  };
}

export function normalizeGroups(raw: unknown): Group[] {
  return itemsOf(raw).map(normalizeGroup);
}

export function normalizeServerOptions(raw: unknown): ServerOption[] {
  return itemsOf(raw)
    .map(record)
    .map((s) => ({ id: count(s.id), name: text(s.name), host: text(s.host), os_id: text(s.os_id) }));
}

export function normalizeUserOptions(raw: unknown): UserOption[] {
  return itemsOf(raw)
    .map(record)
    .filter((u) => u.active !== false && u.is_system !== true)
    .map((u) => ({ id: count(u.id), username: text(u.username), first_name: text(u.first_name), last_name: text(u.last_name) }));
}

export function normalizeCustomActions(raw: unknown): CustomAction[] {
  return itemsOf(raw)
    .map(record)
    .map((a) => ({ id: count(a.id), name: text(a.name) }));
}

export function normalizeAllowlists(raw: unknown): IpAllowlist[] {
  return itemsOf(raw)
    .map(record)
    .map((a) => ({ id: count(a.id), name: text(a.name) }));
}

export function normalizeAptCaches(raw: unknown): AptCache[] {
  return itemsOf(raw)
    .map(record)
    .map((c) => ({ id: count(c.id), name: text(c.name), url: text(c.url) }));
}

/** „admin (Administrator)“, wenn der Benutzer einen Namen hat. */
export function userLabel(user: { username: string; first_name: string; last_name: string }): string {
  const name = [user.first_name, user.last_name].filter(Boolean).join(' ');
  return name ? `${user.username} (${name})` : user.username;
}

/** Sortiert die Regeln eines Auslösers in Ausführungsreihenfolge (Position, dann Alter). */
export function inOrder(rules: Rule[]): Rule[] {
  return [...rules].sort((a, b) => a.position - b.position || a.id - b.id);
}

export interface GroupBody {
  name: string;
  description: string;
  priority: number;
}

export interface ScheduleBody {
  name: string;
  cron_expr: string;
  spread_minutes: number;
}

export interface RuleBody {
  name: string;
  type: string;
  command: string;
  /** Zeitplan-ID oder `'enforce'` für eine Grundsatz-Regel. */
  trigger: number | 'enforce';
}

/** Nach jeder Änderung: abonnierte Gruppen-, Zeitplan- und Regel-Adressen neu laden. */
function changed(): void {
  store.invalidate(GROUPS_URL);
  store.invalidate(ALL_RULES_URL);
}

/**
 * Nach einem Job: die Liste und die offene Gruppe neu laden. Die Details der übrigen Gruppen, aus denen die Liste ihre
 * Zahlen holt, ändert ein Job nicht; sie mitzuladen hieße zwei Abrufe je Gruppe bei jedem Job im Cluster.
 */
export function refreshAfterJob(openGroupId: number | null): void {
  store.invalidate(GROUPS_URL, { withChildren: false });
  if (openGroupId !== null) store.invalidate(groupUrl(openGroupId));
}

async function after<T>(request: Promise<T>): Promise<T> {
  const result = await request;
  changed();
  return result;
}

/** Ändernde Aufrufe. Sie laden nach Erfolg die betroffenen Ansichten neu. */
export const groupsApi = {
  create: (body: GroupBody) => after(api.post<{ id: number }>(`${GROUPS_URL}/create`, body)),
  update: (id: number, body: GroupBody) => after(api.patch(`${groupUrl(id)}/settings`, body)),
  disband: (id: number) => after(api.post(`${groupUrl(id)}/disband`, {})),
  assignServer: (id: number, serverId: number) => after(api.post(`${groupUrl(id)}/assign-server`, { server_id: serverId })),
  removeServer: (id: number, serverId: number) => after(api.post(`${groupUrl(id)}/remove-server`, { server_id: serverId })),
  assignManager: (id: number, userId: number) => after(api.post(`${groupUrl(id)}/assign-manager`, { user_id: userId })),
  removeManager: (id: number, userId: number) => after(api.post(`${groupUrl(id)}/remove-manager`, { user_id: userId })),

  defineSchedule: (groupId: number, body: ScheduleBody) => after(api.post(`${groupUrl(groupId)}/schedules/define`, body)),
  updateSchedule: (id: number, body: ScheduleBody) => after(api.patch(`/api/v1/schedules/${id}/update`, body)),
  removeSchedule: (id: number) => after(api.delete(`/api/v1/schedules/${id}/remove`)),
  setScheduleEnabled: (id: number, enabled: boolean) => after(api.post(`/api/v1/schedules/${id}/${enabled ? 'enable' : 'disable'}`)),
  /** Löst den Lauf aus (Antwort 202). Der Lauf selbst erscheint unter „Jobs“. */
  triggerSchedule: (id: number) => after(api.post(`/api/v1/schedules/${id}/trigger-now`)),

  defineRule: (groupId: number, body: RuleBody) => {
    const { trigger, ...rest } = body;
    const target = trigger === 'enforce' ? { enforce: true } : { schedule_id: trigger };
    return after(api.post(`${groupUrl(groupId)}/rules/define`, { ...rest, ...target }));
  },
  updateRule: (id: number, body: Pick<RuleBody, 'name' | 'command'>) => after(api.patch(`/api/v1/rules/${id}/update`, body)),
  removeRule: (id: number) => after(api.delete(`/api/v1/rules/${id}/remove`)),
  setRuleEnabled: (id: number, enabled: boolean) => after(api.post(`/api/v1/rules/${id}/${enabled ? 'enable' : 'disable'}`)),
  triggerRule: (id: number) => after(api.post(`/api/v1/rules/${id}/trigger-now`)),
  /** Neue Reihenfolge der Regeln EINES Auslösers, die erste läuft zuerst. */
  reorderRules: (groupId: number, ruleIds: number[]) => after(api.put(`${groupUrl(groupId)}/rules/order`, { rule_ids: ruleIds })),
};
