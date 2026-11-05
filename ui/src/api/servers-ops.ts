// Betrieb im Serverdetail: Container (Docker), Linux-Konten, Deep-Scan und Speicher, jeweils mit ihren Aktionen.
// Die Antworten werden tolerant gelesen; was die API nicht liefert, bleibt leer und wird nicht erfunden.

import { api } from '../lib/api';
import { SERVERS_URL, type StartedJob } from './servers';

type Raw = Record<string, unknown>;

const isRecord = (value: unknown): value is Raw => typeof value === 'object' && value !== null;
const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const num = (value: unknown): number => (typeof value === 'number' && Number.isFinite(value) ? value : 0);
const list = (value: unknown): Raw[] => (Array.isArray(value) ? value.filter(isRecord) : []);
const date = (value: unknown): string | null => (typeof value === 'string' && value !== '' ? value : null);
const optionalNum = (value: unknown): number | null => (typeof value === 'number' && Number.isFinite(value) ? value : null);

const serverUrl = (id: number) => `${SERVERS_URL}/${id}`;

export const opsDockerUrl = (id: number) => `${serverUrl(id)}/docker`;
export const opsUsersUrl = (id: number) => `${serverUrl(id)}/users`;
export const pendingSyncsUrl = (id: number) => `${serverUrl(id)}/users/pending`;
export const loginsUrl = (id: number, username: string) => `${serverUrl(id)}/users/${encodeURIComponent(username)}/logins`;
export const opsDeepScanUrl = (id: number) => `${serverUrl(id)}/deep-scan`;
export const deepScanReportUrl = (id: number, reportId: string) => `${serverUrl(id)}/deep-scan/reports/${encodeURIComponent(reportId)}`;
export const opsVolumesUrl = (id: number) => `${serverUrl(id)}/volumes`;
export const storageHistoryUrl = (id: number) => `${serverUrl(id)}/storage-history`;

function started(raw: unknown): StartedJob {
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

const job = async (url: string, body?: unknown) => started(await api.post<unknown>(url, body));

/** Die Aktionen dieses Bereichs. Aktionen mit Job liefern dessen Kennung, die übrigen wirken sofort. */
export const opsApi = {
  dockerRefresh: (id: number) => job(`${opsDockerUrl(id)}/refresh`),
  dockerPull: (id: number, image: string) => job(`${opsDockerUrl(id)}/pull`, { image }),
  dockerPullAll: (id: number) => job(`${opsDockerUrl(id)}/pull-all`),
  composeUpdate: (id: number, project: string, service = '') => job(`${opsDockerUrl(id)}/compose-update`, { project, service }),
  dockerRemoveImage: (id: number, image: string) => job(`${opsDockerUrl(id)}/remove-image`, { image }),
  dockerPrune: (id: number) => job(`${opsDockerUrl(id)}/prune`),
  cveRelevance: (id: number, name: string, relevant: boolean) => api.post<unknown>(`${opsDockerUrl(id)}/cve-relevance`, { name, relevant }),
  usersRefresh: (id: number) => api.post<unknown>(`${opsUsersUrl(id)}/refresh`),
  userDisable: (id: number, username: string) => api.post<unknown>(`${opsUsersUrl(id)}/disable`, { username }),
  userEnable: (id: number, username: string) => api.post<unknown>(`${opsUsersUrl(id)}/enable`, { username }),
  userRemove: (id: number, username: string) => api.post<unknown>(`${opsUsersUrl(id)}/remove`, { username }),
  assignUser: (id: number, linuxUserId: number) => api.post<unknown>(`${serverUrl(id)}/assign-user`, { linux_user_id: linuxUserId }),
  installDeepScanTools: (id: number) => job(`${opsDeepScanUrl(id)}/install-tools`),
  setVolumeMonitor: (id: number, body: VolumeMonitorBody) => api.put(`${opsVolumesUrl(id)}/monitor`, body),
};

// ---- Server-Felder dieses Bereichs (aus der Serverliste mit `?ids=`) ----

export interface OpsServerFlags {
  /** Docker-Updates sind abgeschaltet; Pull und Compose-Update lehnt das Backend dann ab. */
  dockerUpdatesDisabled: boolean;
  /** Container, deren Image-Lücken in die Ampel zählen (Namen klein geschrieben). */
  cveRelevant: string[];
  ssh2fa: boolean;
}

/** Komma-Liste der CVE-relevanten Container; das Backend vergleicht ohne Groß/Klein. */
export function parseCveRelevant(value: string): string[] {
  return value
    .split(',')
    .map((name) => name.trim().toLowerCase())
    .filter(Boolean);
}

/** Die Antwort von `GET /servers?ids=<id>&limit=1` (oder ein einzelner Server). */
export function normaliseServerFlags(raw: unknown): OpsServerFlags {
  const r = isRecord(raw) ? raw : {};
  const server = Array.isArray(r.items) ? (list(r.items)[0] ?? {}) : r;
  return {
    dockerUpdatesDisabled: server.docker_updates_disabled === true,
    cveRelevant: parseCveRelevant(text(server.cve_relevant_containers)),
    ssh2fa: server.ssh_2fa_enabled === true,
  };
}

// ---- Docker ----

export type CheckStatus = '' | 'ok' | 'unauthorized' | 'not_found' | 'local' | 'error';

export interface OpsContainer {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports: string;
  project: string;
  service: string;
}

export interface OpsImage {
  id: string;
  ref: string;
  tag: string;
  digest: string;
  size: string;
  inUse: boolean;
  superseded: boolean;
  olderCopies: number;
  updateAvailable: boolean;
  checkStatus: CheckStatus;
  critical: number;
  high: number;
}

export interface OpsDocker {
  hasDocker: boolean;
  hasCompose: boolean;
  containers: OpsContainer[];
  images: OpsImage[];
}

const CHECK_STATUSES: readonly CheckStatus[] = ['ok', 'unauthorized', 'not_found', 'local', 'error'];

function checkStatus(value: unknown): CheckStatus {
  return CHECK_STATUSES.find((status) => status === value) ?? '';
}

export function normaliseOpsDocker(raw: unknown): OpsDocker {
  const r = isRecord(raw) ? raw : {};
  return {
    hasDocker: r.has_docker === true,
    hasCompose: r.has_compose === true,
    containers: list(r.containers).map((c) => ({
      id: text(c.id) || text(c.container_id) || text(c.name),
      name: text(c.name),
      image: text(c.image),
      state: text(c.state),
      status: text(c.status),
      ports: text(c.ports),
      project: text(c.compose_project),
      service: text(c.compose_service),
    })),
    images: list(r.images).map((i) => {
      const tag = text(i.tag);
      return {
        id: text(i.id) || `${text(i.repository)}:${tag}`,
        ref: tag ? `${text(i.repository)}:${tag}` : text(i.repository),
        tag,
        digest: text(i.repo_digest),
        size: text(i.size_text),
        inUse: i.in_use === true,
        superseded: i.superseded === true,
        olderCopies: num(i.older_copies),
        updateAvailable: i.update_available === true,
        checkStatus: checkStatus(i.check_status),
        critical: num(i.critical_vulns),
        high: num(i.high_vulns),
      };
    }),
  };
}

export interface ComposeGroup {
  /** Leer = Container ohne Compose-Projekt. */
  project: string;
  containers: OpsContainer[];
}

/** Container nach Compose-Projekt, Projekte alphabetisch, die ohne Projekt zuletzt. */
export function composeGroups(containers: OpsContainer[]): ComposeGroup[] {
  const groups = new Map<string, OpsContainer[]>();
  for (const container of containers) {
    const bucket = groups.get(container.project) ?? [];
    bucket.push(container);
    groups.set(container.project, bucket);
  }
  return [...groups.entries()]
    .map(([project, items]) => ({ project, containers: items }))
    .sort((a, b) => (a.project === '' ? 1 : b.project === '' ? -1 : a.project.localeCompare(b.project)));
}

/** Digest kompakt: „sha256:ab12cd34ef56…“; ohne Digest (lokal gebaut) ein Strich. */
export function shortDigest(digest: string): string {
  if (!digest) return '–';
  const at = digest.lastIndexOf('@');
  const plain = at >= 0 ? digest.slice(at + 1) : digest;
  const hex = plain.startsWith('sha256:') ? plain.slice(7) : plain;
  return `sha256:${hex.slice(0, 12)}…`;
}

/** Pull ist nur für ein Image mit Tag und Update sinnvoll; löschen nur, was kein Container nutzt. */
export function imageActions(image: OpsImage): { pull: boolean; remove: boolean } {
  return { pull: image.updateAvailable && image.tag !== '', remove: !image.inUse && !image.superseded };
}

/** Gefundene Lücken je Image-Referenz, damit die Containerzeile sie zeigen kann. */
export function vulnsByImage(images: OpsImage[]): Map<string, { critical: number; high: number }> {
  const out = new Map<string, { critical: number; high: number }>();
  for (const image of images) if (image.critical > 0 || image.high > 0) out.set(image.ref, { critical: image.critical, high: image.high });
  return out;
}

// ---- Linux-Konten ----

export interface OpsUser {
  id: string;
  username: string;
  uid: number;
  shell: string;
  passwordStatus: string;
  sshKeys: number;
  twoFactor: boolean;
  lastLogin: string | null;
  loginCount: number;
  loginsFromLcm: boolean;
  disabled: boolean;
  blocked: boolean;
  managed: boolean;
}

export function normaliseOpsUsers(raw: unknown): OpsUser[] {
  return list(raw).map((u) => ({
    id: text(u.username) || String(num(u.id)),
    username: text(u.username),
    uid: num(u.uid),
    shell: text(u.shell),
    passwordStatus: text(u.password_status),
    sshKeys: num(u.ssh_key_count),
    twoFactor: u.two_factor_enrolled === true,
    lastLogin: date(u.last_login_at),
    loginCount: num(u.login_count),
    loginsFromLcm: u.logins_from_lcm === true,
    disabled: u.disabled === true,
    blocked: u.blocked === true,
    managed: u.managed === true,
  }));
}

export type LoginKind = 'password' | 'keyOnly' | 'unknown' | 'none';

/** Wie sich das Konto anmelden kann; ein gesetztes Passwort geht vor Schlüsseln. */
export function loginKind(user: Pick<OpsUser, 'passwordStatus' | 'sshKeys'>): LoginKind {
  if (user.passwordStatus === 'set') return 'password';
  if (user.sshKeys > 0) return 'keyOnly';
  if (user.passwordStatus === 'unknown') return 'unknown';
  return 'none';
}

/** root und der Dienstbenutzer bleiben unangetastet; entfernen nur unverwaltete Konten (ein verteiltes käme mit dem nächsten Abgleich zurück). */
export function userActions(user: OpsUser, serviceUser: string): { toggle: boolean; remove: boolean } {
  const protectedUser = user.username === 'root' || user.username === serviceUser;
  return { toggle: !protectedUser, remove: !protectedUser && !user.managed };
}

export interface LoginItem {
  id: string;
  from: string;
  tty: string;
  startedAt: string | null;
  endedAt: string | null;
  active: boolean;
}

export function normaliseLogins(raw: unknown): LoginItem[] {
  return list(raw).map((l, index) => ({
    id: String(num(l.id) || index),
    from: text(l.from_host),
    tty: text(l.tty),
    startedAt: date(l.started_at),
    endedAt: date(l.ended_at),
    active: l.still_active === true,
  }));
}

/** Dauer einer beendeten Sitzung in Sekunden; laufende und unvollständige haben keine. */
export function loginSeconds(login: LoginItem): number | null {
  if (login.active || !login.startedAt || !login.endedAt) return null;
  const seconds = Math.round((Date.parse(login.endedAt) - Date.parse(login.startedAt)) / 1000);
  return Number.isFinite(seconds) ? Math.max(0, seconds) : null;
}

export interface PendingSync {
  id: string;
  /** Leer = vollständiger Abgleich; gesetzt = dieses Konto ist noch zu entfernen. */
  username: string;
  attempts: number;
  lastError: string;
}

export function normalisePending(raw: unknown): PendingSync[] {
  return list(raw).map((p, index) => ({ id: String(num(p.id) || index), username: text(p.username), attempts: num(p.attempts), lastError: text(p.last_error) }));
}

export interface AssignableUser {
  id: number;
  username: string;
  fullName: string;
}

/** LCM-Benutzer, die sich zuordnen lassen: aktiv und auf dem Server noch nicht vorhanden. */
export function assignableUsers(linuxUsers: { id: number; username: string; full_name: string; active: boolean }[], serverUsers: Pick<OpsUser, 'username'>[]): AssignableUser[] {
  const present = new Set(serverUsers.map((u) => u.username));
  return linuxUsers.filter((u) => u.active && !present.has(u.username)).map((u) => ({ id: u.id, username: u.username, fullName: u.full_name }));
}

// ---- Deep-Scan ----

export interface OpsFinding {
  id: string;
  severity: string;
  category: string;
  title: string;
  detail: string;
  isNew: boolean;
}

export interface KernelVuln {
  id: string;
  cve: string;
  pkg: string;
  fixed: string;
  severity: string;
  url: string;
}

export interface ScanReport {
  id: string;
  createdAt: string | null;
  critical: number;
  warnings: number;
  infos: number;
  newFindings: number;
  resolvedFindings: number;
  resolvedTitles: string[];
  hardeningIndex: number | null;
  kernelRebootPending: boolean;
  tools: string;
}

export interface OpsDeepScan {
  findings: OpsFinding[];
  kernelVulns: KernelVuln[];
  reports: ScanReport[];
  hardeningIndex: number | null;
  scannedAt: string | null;
  error: string;
  kernelRebootPending: boolean;
  scannerAvailable: boolean;
}

function findings(raw: unknown): OpsFinding[] {
  return list(raw).map((f, index) => ({
    id: text(f.id) || String(index),
    severity: text(f.severity).toLowerCase(),
    category: text(f.category),
    title: text(f.title),
    detail: text(f.detail),
    isNew: f.is_new === true,
  }));
}

/** Die behobenen Titel liegen als JSON-Liste am Lauf; Unlesbares heißt: keine. */
export function parseTitles(value: string): string[] {
  if (!value) return [];
  try {
    const parsed: unknown = JSON.parse(value);
    return Array.isArray(parsed) ? parsed.filter((title): title is string => typeof title === 'string') : [];
  } catch {
    return [];
  }
}

function report(r: Raw): ScanReport {
  return {
    id: text(r.id),
    createdAt: date(r.created_at),
    critical: num(r.critical),
    warnings: num(r.warnings),
    infos: num(r.infos),
    newFindings: num(r.new_findings),
    resolvedFindings: num(r.resolved_findings),
    resolvedTitles: parseTitles(text(r.resolved_titles)),
    hardeningIndex: optionalNum(r.hardening_index),
    kernelRebootPending: r.kernel_reboot_pending === true,
    tools: text(r.tools),
  };
}

export function normaliseOpsDeepScan(raw: unknown): OpsDeepScan {
  const r = isRecord(raw) ? raw : {};
  return {
    findings: findings(r.findings),
    kernelVulns: list(r.kernel_vulns).map((v, index) => ({
      id: text(v.id) || String(index),
      cve: text(v.cve_id),
      pkg: text(v.package_name),
      fixed: text(v.fixed_version),
      severity: text(v.severity).toLowerCase(),
      url: text(v.primary_url),
    })),
    reports: list(r.reports).map(report),
    hardeningIndex: optionalNum(r.hardening_index),
    scannedAt: date(r.deep_scan_at),
    error: text(r.deep_scan_error),
    kernelRebootPending: r.kernel_reboot_pending === true,
    scannerAvailable: r.scanner_available !== false,
  };
}

/** Ein einzelner Lauf samt seiner Befunde (`GET …/deep-scan/reports/:id`). */
export function normaliseReportFindings(raw: unknown): OpsFinding[] {
  return findings(isRecord(raw) ? raw.findings : undefined);
}

/** Befunde nach Kategorie; die Reihenfolge des Backends (schwerste zuerst) bleibt in jeder Gruppe. */
export function groupByCategory(items: OpsFinding[]): { category: string; findings: OpsFinding[] }[] {
  const groups = new Map<string, OpsFinding[]>();
  for (const finding of items) groups.set(finding.category, [...(groups.get(finding.category) ?? []), finding]);
  return [...groups.entries()].map(([category, grouped]) => ({ category, findings: grouped }));
}

// ---- Speicher ----

export interface VolumeMonitor {
  warn: number;
  crit: number;
  inodeWarn: number;
}

export interface VolumeMonitorBody {
  mountpoint: string;
  enabled: boolean;
  warn_percent: number;
  crit_percent: number;
  inode_warn_percent: number;
}

export interface OpsVolume {
  id: string;
  mountpoint: string;
  device: string;
  fstype: string;
  totalMb: number;
  usedMb: number;
  percent: number;
  inodesTotal: number;
  inodePercent: number;
  readOnly: boolean;
  isNetwork: boolean;
  monitorable: boolean;
  monitor: VolumeMonitor | null;
}

export interface OpsPool {
  id: string;
  kind: string;
  name: string;
  state: string;
  rawState: string;
  message: string;
  percent: number;
  metaPercent: number;
}

export interface OpsStorage {
  volumes: OpsVolume[];
  pools: OpsPool[];
}

/** Die Warnschwelle des Backends, wenn keine eigene gesetzt ist (`domain.DiskWarningPercent`). */
export const DEFAULT_WARN_PERCENT = 85;

export function normaliseOpsStorage(raw: unknown): OpsStorage {
  const r = isRecord(raw) ? raw : {};
  return {
    volumes: list(r.volumes).map((v) => {
      const monitor = isRecord(v.monitor) ? v.monitor : null;
      return {
        id: text(v.mountpoint) || String(num(v.id)),
        mountpoint: text(v.mountpoint),
        device: text(v.device),
        fstype: text(v.fstype),
        totalMb: num(v.total_mb),
        usedMb: num(v.used_mb),
        percent: num(v.usage_percent),
        inodesTotal: num(v.inodes_total),
        inodePercent: num(v.inode_percent),
        readOnly: v.read_only === true,
        isNetwork: v.is_network === true,
        monitorable: v.monitorable === true,
        monitor: monitor ? { warn: num(monitor.warn_percent), crit: num(monitor.crit_percent), inodeWarn: num(monitor.inode_warn_percent) } : null,
      };
    }),
    pools: list(r.storage_health).map((p) => ({
      id: String(num(p.id) || text(p.name)),
      kind: text(p.kind),
      name: text(p.name),
      state: text(p.state),
      rawState: text(p.raw_state),
      message: text(p.message),
      percent: num(p.usage_percent),
      metaPercent: num(p.meta_percent),
    })),
  };
}

export type MonitorMode = 'root' | 'network' | 'monitored' | 'off';

/** Wie die Überwachung eines Volumes steht: „/“ ist immer überwacht, Netzspeicher nie von hier. */
export function monitorMode(volume: Pick<OpsVolume, 'mountpoint' | 'monitorable' | 'monitor'>): MonitorMode {
  if (volume.mountpoint === '/') return 'root';
  if (!volume.monitorable) return 'network';
  return volume.monitor ? 'monitored' : 'off';
}

/** Ganze Prozent von 1 bis 100; alles andere ist keine Schwelle. */
export function parseThreshold(value: string): number | null {
  if (!/^\s*\d{1,3}\s*$/.test(value)) return null;
  const percent = Number.parseInt(value, 10);
  return percent >= 1 && percent <= 100 ? percent : null;
}

export function monitorBody(volume: Pick<OpsVolume, 'mountpoint' | 'monitor'>, enabled: boolean, warn: number): VolumeMonitorBody {
  return { mountpoint: volume.mountpoint, enabled, warn_percent: warn, crit_percent: volume.monitor?.crit ?? 0, inode_warn_percent: volume.monitor?.inodeWarn ?? 0 };
}

export interface HistoryPoint {
  day: string;
  usedMb: number;
  totalMb: number;
  percent: number;
}

export interface Forecast {
  days: number;
  unlimited: boolean;
  insufficient: boolean;
}

export interface StorageHistoryData {
  points: HistoryPoint[];
  currentPercent: number;
  currentUsedMb: number;
  currentTotalMb: number;
  forecast: Forecast | null;
}

/** Tagesdurchschnitte des Root-Volumes (`GET …/storage-history`), aufsteigend. */
export function normaliseStorageHistory(raw: unknown): StorageHistoryData {
  const r = isRecord(raw) ? raw : {};
  const forecast = isRecord(r.forecast) ? r.forecast : null;
  return {
    points: list(r.history).map((h) => {
      const totalMb = num(h.disk_total_mb);
      const usedMb = num(h.disk_used_mb);
      return { day: text(h.day), usedMb, totalMb, percent: totalMb > 0 ? (usedMb * 100) / totalMb : 0 };
    }),
    currentPercent: num(r.current_percent),
    currentUsedMb: num(r.current_used_mb),
    currentTotalMb: num(r.current_total_mb),
    forecast: forecast ? { days: num(forecast.days_remaining), unlimited: forecast.unlimited === true, insufficient: forecast.insufficient_data === true } : null,
  };
}
