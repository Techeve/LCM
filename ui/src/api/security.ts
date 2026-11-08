// Sicherheit: CVE-Funde (Trivy), Sammel-Update, Scanner-Datenbank, Frühwarnung (OSV) und Zwischenspeicher.
// Lesen läuft über den Datenspeicher (`LiveData`), Ändern über `securityApi`.

import { api } from '../lib/api';
import { store } from '../lib/store';
import { serversApi, SERVERS_URL, type StartedJob } from './servers';

export const SECURITY_URL = '/api/v1/security';
export const VULNS_URL = `${SECURITY_URL}/vulnerabilities`;
export const BULK_URL = `${SECURITY_URL}/update-all`;
export const SCANNER_URL = `${SECURITY_URL}/scanner`;
export const ADVISORIES_URL = `${SECURITY_URL}/advisories`;
export const CACHES_URL = `${SECURITY_URL}/caches`;

export const CVE_PAGE_SIZE = 50;
export const ADVISORY_PAGE_SIZE = 50;

export const SEVERITIES = ['critical', 'high', 'medium', 'low'] as const;
export type Severity = (typeof SEVERITIES)[number] | 'unknown';
export type SeveritySummary = Record<Exclude<Severity, 'unknown'>, number>;

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function textOrNull(value: unknown): string | null {
  return typeof value === 'string' && value !== '' && !value.startsWith('0001-') ? value : null;
}

/** Zahl, auch wenn das Backend sie als Text liefert (64-Bit-Zähler). */
function numberOr(value: unknown, fallback: number): number {
  const n = typeof value === 'string' ? Number(value) : value;
  return typeof n === 'number' && Number.isFinite(n) ? n : fallback;
}

function list(raw: unknown): unknown[] {
  return Array.isArray(raw) ? raw : isRecord(raw) && Array.isArray(raw.items) ? raw.items : [];
}

export function severityOf(value: unknown): Severity {
  return (SEVERITIES as readonly unknown[]).includes(value) ? (value as Severity) : 'unknown';
}

function normaliseSummary(raw: unknown): SeveritySummary {
  const r = isRecord(raw) ? raw : {};
  return { critical: numberOr(r.critical, 0), high: numberOr(r.high, 0), medium: numberOr(r.medium, 0), low: numberOr(r.low, 0) };
}

// ---- CVE-Funde ----

export type CveSource = '' | 'os' | 'docker';
export const CVE_SOURCES: readonly CveSource[] = ['', 'os', 'docker'];

export interface CveQuery {
  source: CveSource;
  page: number;
}

/** Adresse der CVE-Liste; die Quelle bleibt weg, wenn alle gezeigt werden. */
export function vulnerabilitiesUrl(query: CveQuery): string {
  const params = new URLSearchParams({ page: String(query.page), page_size: String(CVE_PAGE_SIZE) });
  if (query.source) params.set('source', query.source);
  return `${VULNS_URL}?${params}`;
}

export interface CveItem {
  id: string;
  serverId: number;
  serverName: string;
  cve: string;
  pkg: string;
  installed: string;
  fixed: string;
  severity: Severity;
  title: string;
  url: string;
  source: 'os' | 'docker';
  imageRef: string;
}

export interface CvePage {
  items: CveItem[];
  total: number;
  page: number;
  summary: SeveritySummary;
}

export function normaliseCve(raw: unknown): CveItem {
  const r = isRecord(raw) ? raw : {};
  const serverId = numberOr(r.server_id, 0);
  const cve = text(r.cve_id);
  const pkg = text(r.package_name);
  const imageRef = text(r.image_ref);
  return {
    id: [serverId, cve, pkg, imageRef].join('|'),
    serverId,
    serverName: text(r.server_name),
    cve,
    pkg,
    installed: text(r.installed_version),
    fixed: text(r.fixed_version),
    severity: severityOf(r.severity),
    title: text(r.title),
    url: text(r.primary_url),
    source: r.source === 'docker' ? 'docker' : 'os',
    imageRef,
  };
}

export function normaliseCvePage(raw: unknown): CvePage {
  const r = isRecord(raw) ? raw : {};
  const items = list(raw).map(normaliseCve);
  return { items, total: numberOr(r.total, items.length), page: numberOr(r.page, 1), summary: normaliseSummary(r.summary) };
}

// ---- Sammel-Update ----

/**
 * Stand des Sammel-Updates. Wer die betroffenen Server nicht alle sehen darf, bekommt nur `running`;
 * `detailed` ist dann falsch und die Zähler sind leer.
 */
export interface BulkStatus {
  running: boolean;
  total: number;
  completed: number;
  failed: number;
  current: string;
  actor: string;
  startedAt: string | null;
  finishedAt: string | null;
  lastError: string;
  detailed: boolean;
}

export function normaliseBulk(raw: unknown): BulkStatus {
  const r = isRecord(raw) ? raw : {};
  const total = numberOr(r.total, 0);
  const running = r.running === true;
  return {
    running,
    total,
    completed: numberOr(r.completed, 0),
    failed: numberOr(r.failed, 0),
    current: text(r.current),
    actor: text(r.actor),
    startedAt: textOrNull(r.started_at),
    finishedAt: textOrNull(r.finished_at),
    lastError: text(r.last_error),
    detailed: total > 0 || textOrNull(r.finished_at) !== null,
  };
}

/** Anteil der abgearbeiteten Server in Prozent; ohne bekannte Gesamtzahl 0. */
export function bulkPercent(status: BulkStatus): number {
  if (status.total <= 0) return 0;
  return Math.min(100, Math.round(((status.completed + status.failed) / status.total) * 100));
}

// ---- Scanner-Datenbank ----

export type DbFreshness = 'fresh' | 'stale' | 'critical' | 'unknown';

export interface ScannerStatus {
  available: boolean;
  version: string;
  /** Neuere Trivy-Version, die apt auf dem LCM-Host anbietet; leer, wenn aktuell. */
  updateVersion: string;
  updatedAt: string | null;
  nextUpdate: string | null;
  freshness: DbFreshness;
  ageHours: number;
  error: string;
  sandboxed: boolean;
  sandboxBackend: string;
  sandboxNote: string;
}

export function normaliseScanner(raw: unknown): ScannerStatus {
  const r = isRecord(raw) ? raw : {};
  const freshness = r.freshness === 'fresh' || r.freshness === 'stale' || r.freshness === 'critical' ? r.freshness : 'unknown';
  return {
    available: r.available === true,
    version: text(r.version),
    updateVersion: text(r.update_version),
    updatedAt: textOrNull(r.updated_at),
    nextUpdate: textOrNull(r.next_update),
    freshness,
    ageHours: numberOr(r.age_hours, 0),
    error: text(r.error),
    sandboxed: r.sandboxed === true,
    sandboxBackend: text(r.sandbox_backend),
    sandboxNote: text(r.sandbox_note),
  };
}

// ---- Frühwarnung ----

export interface AdvisoryQuery {
  minSeverity: Exclude<Severity, 'unknown'> | '';
  resolved: boolean;
  page: number;
}

export function advisoriesUrl(query: AdvisoryQuery): string {
  const params = new URLSearchParams({ page: String(query.page), page_size: String(ADVISORY_PAGE_SIZE) });
  if (query.resolved) params.set('resolved', '1');
  if (query.minSeverity) params.set('min_severity', query.minSeverity);
  return `${ADVISORIES_URL}?${params}`;
}

export interface AdvisoryItem {
  id: string;
  serverId: number;
  serverName: string;
  advisoryId: string;
  kind: string;
  malware: boolean;
  pkg: string;
  installed: string;
  fixed: string;
  severity: Severity;
  title: string;
  url: string;
  exploited: boolean;
  firstSeenAt: string | null;
  resolvedAt: string | null;
  acknowledgedBy: string;
}

export interface AdvisoryStatus {
  enabled: boolean;
  localCopy: boolean;
  lastPollAt: string | null;
  mirroredAt: string | null;
}

export interface AdvisoryPage {
  items: AdvisoryItem[];
  total: number;
  page: number;
  summary: SeveritySummary;
  status: AdvisoryStatus;
}

export function normaliseAdvisory(raw: unknown): AdvisoryItem {
  const r = isRecord(raw) ? raw : {};
  const kind = text(r.kind);
  return {
    id: text(r.id),
    serverId: numberOr(r.server_id, 0),
    serverName: text(r.server_name),
    advisoryId: text(r.advisory_id),
    kind,
    malware: kind === 'malware',
    pkg: text(r.package_name),
    installed: text(r.installed_version),
    fixed: text(r.fixed_version),
    severity: severityOf(r.severity),
    title: text(r.title),
    url: text(r.url),
    exploited: r.exploited === true,
    firstSeenAt: textOrNull(r.first_seen_at),
    resolvedAt: textOrNull(r.resolved_at),
    acknowledgedBy: text(r.acknowledged_by),
  };
}

export function normaliseAdvisoryPage(raw: unknown): AdvisoryPage {
  const r = isRecord(raw) ? raw : {};
  const status = isRecord(r.status) ? r.status : {};
  const items = list(raw).map(normaliseAdvisory);
  return {
    items,
    total: numberOr(r.total, items.length),
    page: numberOr(r.page, 1),
    summary: normaliseSummary(r.summary),
    status: {
      enabled: status.enabled === true,
      localCopy: status.local_copy === true,
      lastPollAt: textOrNull(status.last_poll_at),
      mirroredAt: textOrNull(status.mirrored_at),
    },
  };
}

// ---- Zwischenspeicher ----

export interface CacheReport {
  scan: { hits: number; misses: number; entries: number; limit: number; stamp: string | null };
  advisory: { hits: number; misses: number; runs: number; sinceAt: string | null };
  snapshot: { entries: number; fresh: number; details: number };
  ttlMinutes: number;
}

export function normaliseCaches(raw: unknown): CacheReport {
  const r = isRecord(raw) ? raw : {};
  const scan = isRecord(r.scan) ? r.scan : {};
  const advisory = isRecord(r.advisory) ? r.advisory : {};
  const snapshot = isRecord(r.snapshot) ? r.snapshot : {};
  return {
    scan: { hits: numberOr(scan.hits, 0), misses: numberOr(scan.misses, 0), entries: numberOr(scan.entries, 0), limit: numberOr(scan.limit, 0), stamp: textOrNull(scan.stamp) },
    advisory: { hits: numberOr(advisory.hits, 0), misses: numberOr(advisory.misses, 0), runs: numberOr(advisory.runs, 0), sinceAt: textOrNull(advisory.since_at) },
    snapshot: { entries: numberOr(snapshot.entries, 0), fresh: numberOr(snapshot.fresh, 0), details: numberOr(snapshot.details, 0) },
    ttlMinutes: numberOr(r.ttl_minutes, 0),
  };
}

/** Trefferquote in Prozent; `null`, solange nichts gemessen wurde (null ist nicht null Prozent). */
export function hitRate(hits: number, misses: number): number | null {
  const total = hits + misses;
  return total === 0 ? null : Math.round((hits / total) * 100);
}

// ---- Ändernde Aufrufe ----

function started(raw: unknown): StartedJob {
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

export const securityApi = {
  /** Startet das Sammel-Update; läuft schon eines (409), liefert der Aufruf den Fehler. */
  startBulk: async () => {
    await api.post(BULK_URL, {});
    store.invalidate(BULK_URL);
  },
  updateScannerDb: async () => started(await api.post<unknown>(`${SCANNER_URL}/update-db`, {})),
  pollAdvisories: async () => started(await api.post<unknown>(`${ADVISORIES_URL}/poll`, {})),
  acknowledge: async (id: string) => {
    await api.post(`${ADVISORIES_URL}/${encodeURIComponent(id)}/acknowledge`, {});
    store.invalidate(ADVISORIES_URL);
  },
  /** Das eine Paket auf die behebende Version heben. */
  updatePackage: (serverId: number, name: string) => serversApi.updatePackages(serverId, [name]),
  /** Installierte Version einfrieren, damit ein Update nicht auf eine belastete Version springt. */
  freezePackage: async (serverId: number, name: string, note: string) => {
    await api.post(`${SERVERS_URL}/${serverId}/packages/pins`, { name, hold: true, note });
  },
};
