// Einstellungen „Pakete & Quellen“, „Schutz“, „Automatisierung“ und „Meldungen“: Adressen, Typen,
// Normalisierung der Antworten und die ändernden Aufrufe. Gelesen wird über den Datenspeicher
// (`LiveData`/`store.subscribe`), geändert über die Funktionen unten; danach werden die betroffenen
// Adressen neu geladen, auch die Lese-Adressen anderer Seiten (Auswahl in Regeln, Firewall …).

import { api } from '../lib/api';
import { readMailSecurity, type MailSecurity } from '../lib/mail-security';
import { store } from '../lib/store';

const V1 = '/api/v1';

export const KNOWN_REPOS_URL = `${V1}/settings/known-repos`;
export const APT_CACHES_URL = `${V1}/settings/apt-caches`;
export const APT_CACHES_READ_URL = `${V1}/apt-caches`;
export const aptCacheOverviewUrl = (cacheId: number) => `${V1}/settings/apt-cache/overview${cacheId ? `?cache_id=${cacheId}` : ''}`;
export const APPS_URL = `${V1}/apps`;
export const CUSTOM_ACTIONS_URL = `${V1}/custom-actions`;
export const LAPIS_URL = `${V1}/settings/crowdsec/lapis`;
export const LAPIS_READ_URL = `${V1}/crowdsec-lapis`;
export const GLOBAL_SETTINGS_URL = `${V1}/settings/global`;
export const ALLOWLISTS_URL = `${V1}/settings/ip-allowlists`;
export const ALLOWLISTS_READ_URL = `${V1}/ip-allowlists`;
export const SCHEDULES_OVERVIEW_URL = `${V1}/system/schedules/overview`;
export const CHANNELS_URL = `${V1}/notification-channels`;
export const ALERT_RULES_URL = `${V1}/alert-rules`;
export const ALERT_EVENTS_URL = `${V1}/alert-events`;
export const LOGS_URL = `${V1}/system/logs`;
export const LOG_LEVEL_URL = `${V1}/system/logs/level`;
export const SERVER_GROUPS_URL = `${V1}/server-groups`;
/** Alle sichtbaren Server (Höchstgrenze des Backends); für „angebundene Server“. */
export const ALL_SERVERS_URL = `${V1}/servers?limit=1000`;

export const alertEventsUrl = (page: number, pageSize: number) => `${ALERT_EVENTS_URL}?limit=${pageSize}&offset=${(page - 1) * pageSize}`;

export function logsUrl(level: string, query: string, lines = 300): string {
  const params = new URLSearchParams({ lines: String(lines) });
  if (level) params.set('level', level);
  if (query) params.set('q', query);
  return `${LOGS_URL}?${params}`;
}

export function logStreamUrl(level: string, query: string): string {
  const params = new URLSearchParams();
  if (level) params.set('level', level);
  if (query) params.set('q', query);
  const search = params.toString();
  return `${LOGS_URL}/stream${search ? `?${search}` : ''}`;
}

// ---- Normalisierung ----------------------------------------------------------------------------

type Raw = Record<string, unknown>;

function record(value: unknown): Raw {
  return value && typeof value === 'object' ? (value as Raw) : {};
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const count = (value: unknown, fallback = 0): number => (typeof value === 'number' && Number.isFinite(value) ? value : fallback);
const flag = (value: unknown): boolean => value === true;
const idOrNull = (value: unknown): number | null => (typeof value === 'number' && value > 0 ? value : null);

/** Listen kommen als Liste oder als `{ items, total }`. */
function items(raw: unknown): Raw[] {
  const list = Array.isArray(raw) ? raw : record(raw).items;
  return Array.isArray(list) ? list.map(record) : [];
}

export interface KnownRepo {
  id: number;
  key: string;
  name: string;
  description: string;
  key_url: string;
  package_manager: string;
  line: string;
}

export function normaliseKnownRepos(raw: unknown): KnownRepo[] {
  return items(raw).map((r) => ({
    id: count(r.id),
    key: text(r.key),
    name: text(r.name),
    description: text(r.description),
    key_url: text(r.key_url),
    package_manager: text(r.package_manager) || 'apt',
    line: text(r.line),
  }));
}

export interface AptCache {
  id: number;
  name: string;
  url: string;
}

export function normaliseAptCaches(raw: unknown): AptCache[] {
  return items(raw).map((c) => ({ id: count(c.id), name: text(c.name), url: text(c.url) }));
}

/** Ergebnis einer Erreichbarkeitsprüfung (APT-Cache und CrowdSec-LAPI haben dieselbe Form). */
export interface ProbeStatus {
  reachable: boolean;
  running: boolean;
  message: string;
}

export function normaliseProbe(raw: unknown): ProbeStatus {
  const s = record(raw);
  return { reachable: flag(s.reachable), running: flag(s.running), message: text(s.message) };
}

export interface AptCacheStats {
  data_fetched: string;
  data_fetched_recent: string;
  data_served: string;
  data_served_recent: string;
}

export interface AptCacheOverview {
  url: string;
  stats: AptCacheStats | null;
  managed: boolean;
  server_id: number | null;
  permanent_cache: boolean;
  disk_usage: string;
}

export function normaliseOverview(raw: unknown): AptCacheOverview {
  const o = record(raw);
  const stats = o.stats ? record(o.stats) : null;
  return {
    url: text(o.url),
    stats: stats
      ? {
          data_fetched: text(stats.data_fetched),
          data_fetched_recent: text(stats.data_fetched_recent),
          data_served: text(stats.data_served),
          data_served_recent: text(stats.data_served_recent),
        }
      : null,
    managed: flag(o.managed),
    server_id: idOrNull(o.server_id),
    permanent_cache: flag(o.permanent_cache),
    disk_usage: text(o.disk_usage),
  };
}

/** Was die Seiten von einem Server wissen müssen: Anbindung an APT-Cache und CrowdSec. */
export interface ServerLink {
  id: number;
  name: string;
  host: string;
  apt_proxy_active: boolean;
  apt_proxy_url: string;
  crowdsec_installed: boolean;
  crowdsec_active: boolean;
  crowdsec_lapi_mode: string;
  crowdsec_lapi_url: string;
}

export function normaliseServerLinks(raw: unknown): ServerLink[] {
  return items(raw).map((s) => ({
    id: count(s.id),
    name: text(s.name),
    host: text(s.host),
    apt_proxy_active: flag(s.apt_proxy_active),
    apt_proxy_url: text(s.apt_proxy_url),
    crowdsec_installed: flag(s.crowdsec_installed),
    crowdsec_active: flag(s.crowdsec_active),
    crowdsec_lapi_mode: text(s.crowdsec_lapi_mode),
    crowdsec_lapi_url: text(s.crowdsec_lapi_url),
  }));
}

export interface AppEntry {
  id: number;
  slug: string;
  name: string;
  description: string;
  name_en: string;
  description_en: string;
  builtin: boolean;
  enabled: boolean;
  markers: string;
  version_command: string;
  version_pattern: string;
  compare: string;
  latest_source: string;
  latest_pattern: string;
  latest_version: string;
  latest_error: string;
  backup_action_id: number | null;
  update_action_id: number | null;
}

export function normaliseApps(raw: unknown): AppEntry[] {
  return items(raw).map((a) => ({
    id: count(a.id),
    slug: text(a.slug),
    name: text(a.name),
    description: text(a.description),
    name_en: text(a.name_en),
    description_en: text(a.description_en),
    builtin: flag(a.builtin),
    enabled: a.enabled !== false,
    markers: text(a.markers),
    version_command: text(a.version_command),
    version_pattern: text(a.version_pattern),
    compare: text(a.compare) || 'semver',
    latest_source: text(a.latest_source),
    latest_pattern: text(a.latest_pattern),
    latest_version: text(a.latest_version),
    latest_error: text(a.latest_error),
    backup_action_id: idOrNull(a.backup_action_id),
    update_action_id: idOrNull(a.update_action_id),
  }));
}

export type AppBody = Omit<AppEntry, 'id' | 'builtin' | 'latest_version' | 'latest_error'>;

export interface CustomAction {
  id: number;
  name: string;
  description: string;
  commands: string;
}

export function normaliseCustomActions(raw: unknown): CustomAction[] {
  return items(raw).map((a) => ({ id: count(a.id), name: text(a.name), description: text(a.description), commands: text(a.commands) }));
}

export interface Lapi {
  id: number;
  name: string;
  url: string;
  login: string;
}

export function normaliseLapis(raw: unknown): Lapi[] {
  return items(raw).map((l) => ({ id: count(l.id), name: text(l.name), url: text(l.url), login: text(l.login) }));
}

export function consoleConfigured(raw: unknown): boolean {
  return flag(record(raw).crowdsec_console_configured);
}

export interface Allowlist {
  id: number;
  name: string;
  description: string;
  entries: string;
}

export function normaliseAllowlists(raw: unknown): Allowlist[] {
  return items(raw).map((a) => ({ id: count(a.id), name: text(a.name), description: text(a.description), entries: text(a.entries) }));
}

export interface ScheduleRule {
  id: number;
  name: string;
  type: string;
  enabled: boolean;
}

export interface ScheduleEntry {
  /** `schedule` (Gruppe) oder die Art des System-Zeitplans (`backup`, `cleanup`, `cve-scan` …). */
  kind: string;
  schedule_id: number | null;
  name: string;
  type: string;
  cron_expr: string;
  group_id: number | null;
  next_run: string;
  enabled: boolean;
  rules: ScheduleRule[];
}

export function normaliseSchedules(raw: unknown): ScheduleEntry[] {
  return items(raw).map((s) => ({
    kind: text(s.kind),
    schedule_id: idOrNull(s.schedule_id),
    name: text(s.name),
    type: text(s.type),
    cron_expr: text(s.cron_expr),
    group_id: idOrNull(s.group_id),
    next_run: text(s.next_run),
    enabled: flag(s.enabled),
    rules: items(s.rules).map((r) => ({ id: count(r.id), name: text(r.name), type: text(r.type), enabled: r.enabled !== false })),
  }));
}

export interface GroupOption {
  id: number;
  name: string;
}

export function normaliseGroupOptions(raw: unknown): GroupOption[] {
  return items(raw).map((g) => ({ id: count(g.id), name: text(g.name) }));
}

export interface EmailConfig {
  host: string;
  port: number;
  username: string;
  from: string;
  recipients: string[];
  security: MailSecurity;
}

export interface Channel {
  id: number;
  name: string;
  type: string;
  enabled: boolean;
  /** Die nicht geheime Konfiguration; das Geheimnis (Passwort, Webhook-URL) liefert das Backend nie. */
  email: EmailConfig;
  format: string;
}

function parseConfig(raw: unknown): Raw {
  if (typeof raw !== 'string' || raw === '') return record(raw);
  try {
    return record(JSON.parse(raw));
  } catch {
    return {};
  }
}

export function normaliseChannels(raw: unknown): Channel[] {
  return items(raw).map((c) => {
    const config = parseConfig(c.config);
    const recipients = Array.isArray(config.recipients) ? config.recipients.filter((r): r is string => typeof r === 'string') : [];
    return {
      id: count(c.id),
      name: text(c.name),
      type: text(c.type) || 'email',
      enabled: flag(c.enabled),
      email: {
        host: text(config.host),
        port: count(config.port, 587),
        username: text(config.username),
        from: text(config.from),
        recipients,
        security: readMailSecurity(config.security, config.use_tls),
      },
      format: text(config.format) || 'teams',
    };
  });
}

export interface ChannelBody {
  name: string;
  type: string;
  enabled: boolean;
  config: Partial<EmailConfig> | { format: string };
  secret?: string;
}

export interface AlertRule {
  id: number;
  name: string;
  type: string;
  enabled: boolean;
  group_ids: number[];
  group_names: string[];
  channel_id: number | null;
  severity: string;
  threshold_percent: number;
  forecast_days: number;
  max_outdated: number;
  min_severity: string;
  heartbeat_hours: number;
  cooldown_minutes: number;
}

export function normaliseAlertRules(raw: unknown): AlertRule[] {
  return items(raw).map((r) => {
    const groups = items(r.groups);
    return {
      id: count(r.id),
      name: text(r.name),
      type: text(r.type),
      enabled: flag(r.enabled),
      group_ids: groups.map((g) => count(g.id)),
      group_names: groups.map((g) => text(g.name)),
      channel_id: idOrNull(r.channel_id),
      severity: text(r.severity) || 'warning',
      threshold_percent: count(r.threshold_percent),
      forecast_days: count(r.forecast_days),
      max_outdated: count(r.max_outdated),
      min_severity: text(r.min_severity),
      heartbeat_hours: count(r.heartbeat_hours),
      cooldown_minutes: count(r.cooldown_minutes),
    };
  });
}

export type AlertRuleBody = Omit<AlertRule, 'id' | 'group_names'>;

export interface AlertEvent {
  id: string;
  created_at: string;
  rule_name: string;
  server_id: number | null;
  server_name: string;
  type: string;
  severity: string;
  description: string;
  notified: boolean;
  notify_error: string;
}

export interface AlertEventPage {
  items: AlertEvent[];
  total: number;
}

export function normaliseAlertEvents(raw: unknown): AlertEventPage {
  const list = items(raw).map((e) => ({
    id: text(e.id),
    created_at: text(e.created_at),
    rule_name: text(e.rule_name),
    server_id: idOrNull(e.server_id),
    server_name: text(e.server_name),
    type: text(e.type),
    severity: text(e.severity),
    description: text(e.description),
    notified: flag(e.notified),
    notify_error: text(e.notify_error),
  }));
  return { items: list, total: count(record(raw).total, list.length) };
}

export interface LogEntry {
  time: string;
  level: string;
  msg: string;
  raw: string;
}

export function normaliseLogEntry(raw: unknown): LogEntry {
  const e = record(raw);
  return { time: text(e.time), level: text(e.level), msg: text(e.msg), raw: text(e.raw) };
}

export function normaliseLogs(raw: unknown): LogEntry[] {
  return items(record(raw).entries).map(normaliseLogEntry);
}

export interface LogLevel {
  level: string;
  debug_until: string;
}

export function normaliseLogLevel(raw: unknown): LogLevel {
  const l = record(raw);
  return { level: text(l.level) || 'info', debug_until: text(l.debug_until) };
}

// ---- Ändernde Aufrufe ----------------------------------------------------------------------------

/** Lädt die Adressen neu, die eine Änderung betrifft. */
function changed(...urls: string[]): void {
  urls.forEach((url) => store.invalidate(url));
}

/** POST mit ID = Bearbeiten, ohne = Anlegen (Muster der „save“-Endpunkte unter /settings). */
async function saveTo<T>(url: string, id: number | null, body: object): Promise<T> {
  return api.post<T>(url, id ? { ...body, id } : body);
}

export interface JobStart {
  job_id: string;
}

export const settingsMoreApi = {
  async saveKnownRepo(id: number | null, body: Omit<KnownRepo, 'id'>): Promise<void> {
    await saveTo(KNOWN_REPOS_URL, id, body);
    changed(KNOWN_REPOS_URL, `${V1}/servers/known-repos`);
  },
  async deleteKnownRepo(id: number): Promise<void> {
    await api.delete(`${KNOWN_REPOS_URL}/${id}`);
    changed(KNOWN_REPOS_URL, `${V1}/servers/known-repos`);
  },

  async saveAptCache(id: number | null, body: Omit<AptCache, 'id'>): Promise<void> {
    await saveTo(APT_CACHES_URL, id, body);
    changed(APT_CACHES_URL, APT_CACHES_READ_URL, `${V1}/settings/apt-cache/overview`);
  },
  async deleteAptCache(id: number): Promise<void> {
    await api.delete(`${APT_CACHES_URL}/${id}`);
    changed(APT_CACHES_URL, APT_CACHES_READ_URL, `${V1}/settings/apt-cache/overview`);
  },
  checkAptCache: async (id: number): Promise<ProbeStatus> => normaliseProbe(await api.get(`${APT_CACHES_URL}/${id}/status`)),
  restartAptCacher: (serverId: number) => api.post<JobStart>(`${V1}/servers/${serverId}/apt-cache/restart`),
  setPermanentCache: (serverId: number, enabled: boolean) => api.post<JobStart>(`${V1}/servers/${serverId}/apt-cache/permanent-cache`, { enabled }),
  async disconnectAptProxy(serverId: number): Promise<string> {
    const result = await api.post<{ output?: string }>(`${V1}/servers/${serverId}/apt-proxy`, { enable: false });
    changed(`${V1}/servers`);
    return result?.output ?? '';
  },

  async saveApp(id: number | null, body: AppBody): Promise<void> {
    if (id) await api.put(`${APPS_URL}/${id}`, body);
    else await api.post(APPS_URL, body);
    changed(APPS_URL);
  },
  async deleteApp(id: number): Promise<void> {
    await api.delete(`${APPS_URL}/${id}`);
    changed(APPS_URL);
  },

  async saveCustomAction(id: number | null, body: Omit<CustomAction, 'id'>): Promise<void> {
    if (id) await api.patch(`${CUSTOM_ACTIONS_URL}/${id}`, body);
    else await api.post(CUSTOM_ACTIONS_URL, body);
    changed(CUSTOM_ACTIONS_URL);
  },
  async deleteCustomAction(id: number): Promise<void> {
    await api.delete(`${CUSTOM_ACTIONS_URL}/${id}`);
    changed(CUSTOM_ACTIONS_URL);
  },

  async saveLapi(id: number | null, body: Omit<Lapi, 'id'> & { password: string }): Promise<void> {
    await saveTo(LAPIS_URL, id, body);
    changed(LAPIS_URL, LAPIS_READ_URL);
  },
  async deleteLapi(id: number): Promise<void> {
    await api.delete(`${LAPIS_URL}/${id}`);
    changed(LAPIS_URL, LAPIS_READ_URL);
  },
  checkLapi: async (id: number): Promise<ProbeStatus> => normaliseProbe(await api.get(`${LAPIS_URL}/${id}/status`)),
  async saveConsoleKey(key: string): Promise<void> {
    await api.patch(GLOBAL_SETTINGS_URL, { crowdsec_console_key: key });
    changed(GLOBAL_SETTINGS_URL);
  },

  async saveAllowlist(id: number | null, body: Omit<Allowlist, 'id'>): Promise<void> {
    await saveTo(ALLOWLISTS_URL, id, body);
    changed(ALLOWLISTS_URL, ALLOWLISTS_READ_URL);
  },
  async deleteAllowlist(id: number): Promise<void> {
    await api.delete(`${ALLOWLISTS_URL}/${id}`);
    changed(ALLOWLISTS_URL, ALLOWLISTS_READ_URL);
  },

  /** Gruppen-Zeitpläne über ihre ID, System-Zeitpläne über ihre Art. */
  async triggerSchedule(entry: ScheduleEntry): Promise<void> {
    if (entry.kind === 'schedule' && entry.schedule_id) await api.post(`${V1}/system/schedules/${entry.schedule_id}/trigger-now`);
    else await api.post(`${V1}/system/schedules/kind/${encodeURIComponent(entry.kind)}/trigger-now`);
  },

  async saveChannel(id: number | null, body: ChannelBody): Promise<void> {
    if (id) await api.patch(`${CHANNELS_URL}/${id}`, body);
    else await api.post(CHANNELS_URL, body);
    changed(CHANNELS_URL);
  },
  async deleteChannel(id: number): Promise<void> {
    await api.delete(`${CHANNELS_URL}/${id}`);
    changed(CHANNELS_URL);
  },
  testChannel: (id: number) => api.post(`${CHANNELS_URL}/${id}/test`),

  async saveAlertRule(id: number | null, body: AlertRuleBody): Promise<void> {
    if (id) await api.patch(`${ALERT_RULES_URL}/${id}`, body);
    else await api.post(ALERT_RULES_URL, body);
    changed(ALERT_RULES_URL);
  },
  async deleteAlertRule(id: number): Promise<void> {
    await api.delete(`${ALERT_RULES_URL}/${id}`);
    changed(ALERT_RULES_URL);
  },
  async evaluateAlerts(): Promise<void> {
    await api.post(`${V1}/alerts/evaluate`);
    changed(ALERT_EVENTS_URL);
  },

  async setDebug(debug: boolean): Promise<LogLevel> {
    const level = normaliseLogLevel(await api.post(LOG_LEVEL_URL, { debug }));
    changed(LOG_LEVEL_URL);
    return level;
  },
};
