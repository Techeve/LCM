// Server: Liste, Detail, Betriebsfähigkeit, Pakete, Befehlsvorschau und alle ändernden Aufrufe.
// Lesen läuft über den Datenspeicher (`LiveData`/`store.subscribe`), Ändern über `serversApi`;
// danach werden die abonnierten Server-Adressen von den Ereignissen oder `store.invalidate` neu geladen.

import type { StatusLevel } from '../components/shared';
import { api } from '../lib/api';
import { store } from '../lib/store';
import type { Insight } from './dashboard';

export const SERVERS_URL = '/api/v1/servers';
export const GROUPS_URL = '/api/v1/server-groups';
export const PAGE_SIZE = 100;

export type ServerStatus = 'excellent' | 'green' | 'yellow' | 'red';
export const SERVER_STATUSES: readonly ServerStatus[] = ['excellent', 'green', 'yellow', 'red'];

export type ServerSort = 'name' | 'status' | 'last_seen' | 'os' | 'kernel' | 'updates';
export const SERVER_SORTS: readonly ServerSort[] = ['name', 'status', 'last_seen', 'os', 'kernel', 'updates'];
export const DEFAULT_SERVER_SORT = 'name';

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function numberOr(value: unknown, fallback: number): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : fallback;
}

export interface DsmCheck {
  key: string;
  state: 'ok' | 'deviation' | 'unknown';
  value: string;
  params: Record<string, string>;
}

function normaliseDsmChecks(value: unknown): DsmCheck[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((raw: unknown) => {
    if (!raw || typeof raw !== 'object') return [];
    const r = raw as Record<string, unknown>;
    const state = r.state === 'ok' || r.state === 'deviation' ? r.state : 'unknown';
    const params = r.params && typeof r.params === 'object' ? (r.params as Record<string, string>) : {};
    return [{ key: text(r.key), state, value: text(r.value), params }];
  });
}

/** Eine Regel der DSM-Firewall, wie DSM sie liefert. Ports sind Dienstnamen von DSM oder Nummern, "all" heißt alle. */
export interface DsmFirewallRule {
  enabled: boolean;
  policy: string;
  protocol: string;
  ports: string;
  port_group: string;
  source_ip: string;
  source_ip_group: string;
  name: string;
}

/** Die Regeln einer Schnittstelle ("global" = alle). `policy` gilt, wenn keine Regel trifft ("none" = nicht festgelegt). */
export interface DsmFirewallSection {
  name: string;
  policy: string;
  rules: DsmFirewallRule[];
}

/** Das aktive Firewall-Profil von DSM mit seinen Regeln. */
export interface DsmFirewall {
  profile: string;
  sections: DsmFirewallSection[];
}

function normaliseDsmFirewallRule(r: Raw): DsmFirewallRule {
  return {
    enabled: r.enabled !== false,
    policy: text(r.policy),
    protocol: text(r.protocol),
    ports: text(r.ports),
    port_group: text(r.port_group),
    source_ip: text(r.source_ip),
    source_ip_group: text(r.source_ip_group),
    name: text(r.name),
  };
}

/** Das gelesene Profil; `null`, solange LCM es nicht gelesen hat. */
export function normaliseDsmFirewall(value: unknown): DsmFirewall | null {
  if (!isRecord(value) || text(value.profile) === '') return null;
  const sections = Array.isArray(value.sections) ? value.sections.filter(isRecord) : [];
  return {
    profile: text(value.profile),
    sections: sections.map((s) => ({
      name: text(s.name),
      policy: text(s.policy),
      rules: Array.isArray(s.rules) ? s.rules.filter(isRecord).map(normaliseDsmFirewallRule) : [],
    })),
  };
}

function textOrNull(value: unknown): string | null {
  return typeof value === 'string' && value !== '' ? value : null;
}

export interface ServerGroupRef {
  id: number;
  name: string;
}

export interface ServerItem {
  id: number;
  name: string;
  host: string;
  ssh_port: number;
  service_user: string;
  hostname: string;
  os_id: string;
  os_name: string;
  os_version: string;
  kernel_version: string;
  transport: string;
  agent_connected: boolean;
  last_seen_at: string | null;
  last_error: string;
  reachable: boolean;
  status: ServerStatus;
  insights: Insight[];
  incapable: boolean;
  updates: number;
  reboot_required: boolean;
  kernel_reboot_pending: boolean;
  disk_total_mb: number;
  disk_used_mb: number;
  mem_total_mb: number;
  mem_used_mb: number;
  groups: ServerGroupRef[];
  restricted_sudo: boolean;
  maintenance: boolean;
  has_snap: boolean;
  has_docker: boolean;
  proxmox_type: string;
  is_demo: boolean;
  last_cve_scan_at: string | null;
  cve_scan_error: string;
  update_check_error: string;
  deep_scan_warnings: number;
  /** Der Server, auf dem LCM selbst läuft (Regel im Backend: Loopback und Port 22). */
  is_lcm_host: boolean;
  failed_checks: number;
  agent_version: string;
  console_disabled: boolean;
  user_sync_disabled: boolean;
  // Hardware und Netz
  virtualization: string;
  hardware_model: string;
  cpu_model: string;
  cpu_cores: number;
  package_manager: string;
  ip_addresses: string;
  dns_current: string;
  dns_test_status: string;
  dns_test_at: string | null;
  dns_test_detail: string;
  timezone: string;
  ntp_service: string;
  ntp_synchronized: boolean;
  ntp_servers: string;
  clock_offset_seconds: number;
  time_checked_at: string | null;
  // System und Schutz
  os_support: OsSupport;
  proxmox_version: string;
  ssh_hardened: boolean;
  kex_algorithm: string;
  firewall_active: boolean;
  firewall_tool: string;
  firewall_allowed_ports: string;
  fail2ban_installed: boolean;
  fail2ban_active: boolean;
  crowdsec_installed: boolean;
  crowdsec_active: boolean;
  host_key_fingerprint: string;
  // Geräte über eine API (RouterOS, Synology DSM)
  routerboard_model: string;
  routeros_channel: string;
  routeros_latest_version: string;
  routeros_update_available: boolean;
  dsm_model: string;
  /** Firewall und Sicherheitseinstellungen von DSM, je Prüfung der Stand; leer = nie gelesen. */
  dsm_checks: DsmCheck[];
  /** Regeln des aktiven Firewall-Profils; `null` = nicht gelesen. */
  dsm_firewall: DsmFirewall | null;
  dsm_port: number;
  dsm_latest_version: string;
  dsm_update_available: boolean;
  dsm_security_risks: number;
  dsm_security_summary: string;
  dsm_cert_fingerprint: string;
}

/** Support-Stand des Betriebssystems (`os_support` der Serverliste). */
export interface OsSupport {
  known: boolean;
  is_lts: boolean;
  supported: boolean;
  eol: string;
  eol_soon: boolean;
  up_to_date: boolean;
  recommended: string;
  summary: string;
  summary_key: string;
  summary_params: Record<string, string>;
}

function normaliseOsSupport(raw: unknown): OsSupport {
  const r = isRecord(raw) ? raw : {};
  const params = isRecord(r.summary_params) ? Object.fromEntries(Object.entries(r.summary_params).map(([k, v]) => [k, String(v)])) : {};
  return {
    known: r.known === true,
    is_lts: r.is_lts === true,
    supported: r.supported === true,
    eol: text(r.eol),
    eol_soon: r.eol_soon === true,
    up_to_date: r.up_to_date === true,
    recommended: text(r.recommended),
    summary: text(r.summary),
    summary_key: text(r.summary_key),
    summary_params: params,
  };
}

export interface ServerPage {
  items: ServerItem[];
  total: number;
}

function status(value: unknown): ServerStatus {
  return (SERVER_STATUSES as readonly unknown[]).includes(value) ? (value as ServerStatus) : 'green';
}

function normaliseInsight(raw: unknown): Insight | null {
  if (!isRecord(raw)) return null;
  const params = isRecord(raw.params) ? Object.fromEntries(Object.entries(raw.params).map(([k, v]) => [k, String(v)])) : undefined;
  return { severity: text(raw.severity) || 'info', message: text(raw.message), key: text(raw.key) || undefined, params };
}

function normaliseGroup(raw: unknown): ServerGroupRef | null {
  return isRecord(raw) && typeof raw.id === 'number' ? { id: raw.id, name: text(raw.name) } : null;
}

export function normaliseServer(raw: unknown): ServerItem {
  const r = isRecord(raw) ? raw : {};
  return {
    id: numberOr(r.id, 0),
    name: text(r.name),
    host: text(r.host),
    ssh_port: numberOr(r.ssh_port, 22),
    service_user: text(r.service_user),
    hostname: text(r.hostname),
    os_id: text(r.os_id),
    os_name: text(r.os_name),
    os_version: text(r.os_version),
    kernel_version: text(r.kernel_version),
    transport: text(r.transport) || 'ssh',
    agent_connected: r.agent_connected === true,
    last_seen_at: textOrNull(r.last_seen_at),
    last_error: text(r.last_error),
    reachable: r.reachable !== false,
    status: status(r.status),
    insights: (Array.isArray(r.insights) ? r.insights : []).map(normaliseInsight).filter((i): i is Insight => i !== null),
    incapable: r.incapable === true,
    updates: numberOr(r.updates, 0),
    reboot_required: r.reboot_required === true,
    kernel_reboot_pending: r.kernel_reboot_pending === true,
    disk_total_mb: numberOr(r.disk_total_mb, 0),
    disk_used_mb: numberOr(r.disk_used_mb, 0),
    mem_total_mb: numberOr(r.mem_total_mb, 0),
    mem_used_mb: numberOr(r.mem_used_mb, 0),
    groups: (Array.isArray(r.groups) ? r.groups : []).map(normaliseGroup).filter((g): g is ServerGroupRef => g !== null),
    restricted_sudo: r.restricted_sudo === true,
    maintenance: r.maintenance === true,
    has_snap: r.has_snap === true,
    has_docker: r.has_docker === true,
    proxmox_type: text(r.proxmox_type),
    is_demo: r.is_demo === true,
    last_cve_scan_at: textOrNull(r.last_cve_scan_at),
    cve_scan_error: text(r.cve_scan_error),
    update_check_error: text(r.update_check_error),
    deep_scan_warnings: numberOr(r.deep_scan_warnings, 0),
    is_lcm_host: r.is_lcm_host === true,
    failed_checks: numberOr(r.failed_checks, 0),
    agent_version: text(r.agent_version),
    console_disabled: r.console_disabled === true,
    user_sync_disabled: r.user_sync_disabled === true,
    virtualization: text(r.virtualization),
    hardware_model: text(r.hardware_model),
    cpu_model: text(r.cpu_model),
    cpu_cores: numberOr(r.cpu_cores, 0),
    package_manager: text(r.package_manager),
    ip_addresses: text(r.ip_addresses),
    dns_current: text(r.dns_current),
    dns_test_status: text(r.dns_test_status),
    dns_test_at: textOrNull(r.dns_test_at),
    dns_test_detail: text(r.dns_test_detail),
    timezone: text(r.timezone),
    ntp_service: text(r.ntp_service),
    ntp_synchronized: r.ntp_synchronized === true,
    ntp_servers: text(r.ntp_servers),
    clock_offset_seconds: numberOr(r.clock_offset_seconds, 0),
    time_checked_at: textOrNull(r.time_checked_at),
    os_support: normaliseOsSupport(r.os_support),
    proxmox_version: text(r.proxmox_version),
    ssh_hardened: r.ssh_hardened === true,
    kex_algorithm: text(r.kex_algorithm),
    firewall_active: r.firewall_active === true,
    firewall_tool: text(r.firewall_tool),
    firewall_allowed_ports: text(r.firewall_allowed_ports),
    fail2ban_installed: r.fail2ban_installed === true,
    fail2ban_active: r.fail2ban_active === true,
    crowdsec_installed: r.crowdsec_installed === true,
    crowdsec_active: r.crowdsec_active === true,
    host_key_fingerprint: text(r.host_key_fingerprint),
    routerboard_model: text(r.routerboard_model),
    routeros_channel: text(r.routeros_channel),
    routeros_latest_version: text(r.routeros_latest_version),
    routeros_update_available: r.routeros_update_available === true,
    dsm_model: text(r.dsm_model),
    dsm_checks: normaliseDsmChecks(r.dsm_checks),
    dsm_firewall: normaliseDsmFirewall(r.dsm_firewall),
    dsm_port: numberOr(r.dsm_port, 0),
    dsm_latest_version: text(r.dsm_latest_version),
    dsm_update_available: r.dsm_update_available === true,
    dsm_security_risks: numberOr(r.dsm_security_risks, 0),
    dsm_security_summary: text(r.dsm_security_summary),
    dsm_cert_fingerprint: text(r.dsm_cert_fingerprint),
  };
}

/** Antwort von `GET /servers`; ältere Form (nacktes Array) wird mitgenommen. */
export function normaliseServers(raw: unknown): ServerPage {
  const items = Array.isArray(raw) ? raw : isRecord(raw) && Array.isArray(raw.items) ? raw.items : [];
  const total = isRecord(raw) ? numberOr(raw.total, items.length) : items.length;
  return { items: items.map(normaliseServer), total };
}

// ---- Adressen ----

export interface ServersQuery {
  q: string;
  status: string;
  os: string;
  kernel: string;
  group: string;
  incapable: boolean;
  sort: string;
  page: number;
}

/** Adresse der Serverliste; leere Filter bleiben weg. */
export function serversUrl(query: ServersQuery): string {
  const params = new URLSearchParams();
  if (query.q) params.set('q', query.q);
  if (query.status) params.set('status', query.status);
  if (query.os) params.set('os', query.os);
  if (query.kernel) params.set('kernel', query.kernel);
  if (query.group) params.set('group_id', query.group);
  if (query.incapable) params.set('incapable', '1');
  params.set('sort', query.sort);
  params.set('limit', String(PAGE_SIZE));
  params.set('offset', String((Math.max(1, query.page) - 1) * PAGE_SIZE));
  return `${SERVERS_URL}?${params}`;
}

/** Der eine Server samt Ampel und Befunden (die Detailroute liefert beides nicht). */
export const serverSummaryUrl = (id: number) => `${SERVERS_URL}?ids=${id}&limit=1`;
export const serverUrl = (id: number) => `${SERVERS_URL}/${id}`;
export const capabilitiesUrl = (id: number) => `${SERVERS_URL}/${id}/capabilities`;
export const packagesUrl = (id: number) => `${SERVERS_URL}/${id}/packages`;
export const activeJobUrl = (id: number) => `${SERVERS_URL}/${id}/active-job`;
export const repositoriesUrl = (id: number) => `${SERVERS_URL}/${id}/repositories`;

// ---- Betriebsfähigkeit (Release A) ----

export type CapabilityCategory = 'connection' | 'packages' | 'rules' | 'cve_scan';
export const CAPABILITY_CATEGORIES: readonly CapabilityCategory[] = ['connection', 'packages', 'rules', 'cve_scan'];

export interface Capability {
  category: CapabilityCategory;
  /** Zustand der Aufzeichnung: ok, warning oder failing. */
  state: string;
  /** Zustand, der tatsächlich gilt (zusätzlich z. B. veraltet). */
  effective: string;
  failing_since: string | null;
  consecutive_failures: number;
  last_success_at: string | null;
  last_error_class: string;
  last_error_message: string;
  /** Zahlen und Namen zur Ursache (`pid`, `process`, `path` …), damit der Text sie nennen kann. */
  last_error_params: Record<string, string>;
  last_job_id: string;
  last_job_name: string;
  warning_class: string;
  warning_message: string;
  stale: string;
  /** Reparaturart (`dpkg_configure`, `fix_deps`) oder leer. */
  repair: string;
  retryable: boolean;
}

function textParams(raw: unknown): Record<string, string> {
  if (!isRecord(raw)) return {};
  return Object.fromEntries(Object.entries(raw).map(([name, value]) => [name, String(value)]));
}

export function normaliseCapability(raw: unknown): Capability | null {
  if (!isRecord(raw)) return null;
  const category = raw.category;
  if (!(CAPABILITY_CATEGORIES as readonly unknown[]).includes(category)) return null;
  return {
    category: category as CapabilityCategory,
    state: text(raw.state) || 'ok',
    effective: text(raw.effective),
    failing_since: textOrNull(raw.failing_since),
    consecutive_failures: numberOr(raw.consecutive_failures, 0),
    last_success_at: textOrNull(raw.last_success_at),
    last_error_class: text(raw.last_error_class),
    last_error_message: text(raw.last_error_message),
    last_error_params: textParams(raw.last_error_params),
    last_job_id: text(raw.last_job_id),
    last_job_name: text(raw.last_job_name),
    warning_class: text(raw.warning_class),
    warning_message: text(raw.warning_message),
    stale: text(raw.stale),
    repair: text(raw.repair),
    retryable: raw.retryable === true,
  };
}

export function normaliseCapabilities(raw: unknown): Capability[] {
  const rows = Array.isArray(raw) ? raw.map(normaliseCapability).filter((c): c is Capability => c !== null) : [];
  return CAPABILITY_CATEGORIES.flatMap((category) => rows.filter((row) => row.category === category));
}

/**
 * Ampelstufe einer Kategorie. `effective` ist leer, solange nichts fehlschlägt, sonst `warning` oder `critical`.
 * Ohne Fehlschlag zählen eine Warnung der Kategorie und ein Stillstand der Paketliste (`stale`).
 */
export type CapabilityLevel = Extract<StatusLevel, 'ok' | 'warn' | 'crit'>;

export function capabilityLevel(capability: Capability): CapabilityLevel {
  if (capability.effective === 'critical') return 'crit';
  if (capability.effective === 'warning') return 'warn';
  if (capability.stale === 'critical') return 'crit';
  if (capability.stale === 'warning' || capability.warning_class !== '') return 'warn';
  return 'ok';
}

// ---- Pakete ----

export interface ServerPackage {
  name: string;
  version: string;
  candidate: string;
  security: boolean;
  held_reason: string;
}

export function normalisePackages(raw: unknown): ServerPackage[] {
  const rows = Array.isArray(raw) ? raw : [];
  return rows.filter(isRecord).map((r) => ({
    name: text(r.name),
    version: text(r.version),
    candidate: text(r.candidate_version),
    security: r.security === true,
    held_reason: text(r.held_reason),
  }));
}

export const pendingOf = (packages: ServerPackage[]) => packages.filter((p) => p.candidate !== '');

// ---- Repositories ----

export interface Repository {
  id: number;
  line: string;
  insecure: boolean;
}

export function normaliseRepositories(raw: unknown): Repository[] {
  const rows = Array.isArray(raw) ? raw : [];
  return rows.filter(isRecord).map((r) => ({ id: numberOr(r.id, 0), line: text(r.line), insecure: r.insecure === true }));
}

// ---- Befehlsvorschau ----

export interface PreviewCommand {
  run: string;
  as: string;
}

export interface PreviewNote {
  code: string;
  params: Record<string, string | number>;
}

export interface ActionPreviewData {
  action: string;
  commands: PreviewCommand[];
  packages: Array<{ name: string; from: string; to: string }>;
  services: string[];
  reboot: 'no' | 'maybe' | 'yes';
  reversible: boolean;
  duration: { min: number; max: number } | null;
  notes: PreviewNote[];
}

export function normalisePreview(raw: unknown): ActionPreviewData {
  const r = isRecord(raw) ? raw : {};
  const commands = (Array.isArray(r.commands) ? r.commands : []).filter(isRecord).map((c) => ({ run: text(c.run), as: text(c.as) }));
  const packages = (Array.isArray(r.packages) ? r.packages : []).filter(isRecord).map((p) => ({ name: text(p.name), from: text(p.from), to: text(p.to) }));
  const notes = (Array.isArray(r.notes) ? r.notes : []).filter(isRecord).map((n) => ({
    code: text(n.code),
    params: isRecord(n.params) ? (n.params as Record<string, string | number>) : {},
  }));
  const duration = isRecord(r.duration) ? { min: numberOr(r.duration.min_seconds, 0), max: numberOr(r.duration.max_seconds, 0) } : null;
  const reboot = r.reboot === 'yes' || r.reboot === 'maybe' ? r.reboot : 'no';
  const services = (Array.isArray(r.services) ? r.services : []).filter((s): s is string => typeof s === 'string');
  return { action: text(r.action), commands, packages, services, reboot, reversible: r.reversible === true, duration, notes };
}

export type PreviewAction =
  | 'refresh'
  | 'upgrade-all'
  | 'upgrade-security'
  | 'update'
  | 'remove'
  | 'autoremove'
  | 'repair'
  | 'kernels-cleanup'
  | 'reboot'
  | 'retry'
  | 'snaps-refresh-all'
  | 'snaps-refresh'
  | 'snaps-remove';

export interface PreviewParams {
  package?: string[];
  version?: string;
  kind?: string;
  category?: string;
}

export function previewUrl(serverId: number, action: PreviewAction, params: PreviewParams = {}): string {
  const query = new URLSearchParams();
  if (params.package?.length) query.set('package', params.package.join(','));
  if (params.version) query.set('version', params.version);
  if (params.kind) query.set('kind', params.kind);
  if (params.category) query.set('category', params.category);
  const suffix = query.toString();
  return `${SERVERS_URL}/${serverId}/actions/${action}/preview${suffix ? `?${suffix}` : ''}`;
}

export async function fetchPreview(serverId: number, action: PreviewAction, params: PreviewParams = {}): Promise<ActionPreviewData> {
  return normalisePreview(await api.get(previewUrl(serverId, action, params)));
}

// ---- Aktiver Job ----

export interface ActiveJob {
  id: string;
  name: string;
  type: string;
  status: string;
}

export function normaliseActiveJob(raw: unknown): ActiveJob | null {
  const job = isRecord(raw) ? raw.job : null;
  if (!isRecord(job)) return null;
  return { id: text(job.id), name: text(job.name), type: text(job.type), status: text(job.status) };
}

// ---- Ändernde Aufrufe ----

export interface StartedJob {
  status: string;
  job_id: string;
  job_name: string;
}

function started(raw: unknown): StartedJob {
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

const post = async (url: string, body?: unknown) => started(await api.post<unknown>(url, body));

export const serversApi = {
  refreshPackages: (id: number) => post(`${SERVERS_URL}/${id}/packages/refresh`),
  upgradeAll: (id: number) => post(`${SERVERS_URL}/${id}/packages/upgrade-all`),
  upgradeSecurity: (id: number) => post(`${SERVERS_URL}/${id}/packages/upgrade-security`),
  updatePackages: (id: number, names: string[], version = '') => post(`${SERVERS_URL}/${id}/packages/update`, { names, version }),
  removePackages: (id: number, names: string[]) => post(`${SERVERS_URL}/${id}/packages/remove`, { names }),
  autoremove: (id: number) => post(`${SERVERS_URL}/${id}/packages/autoremove`),
  repair: (id: number, action: string) => post(`${SERVERS_URL}/${id}/packages/repair`, { action }),
  cleanupKernels: (id: number) => post(`${SERVERS_URL}/${id}/kernels/cleanup`),
  reboot: (id: number) => post(`${SERVERS_URL}/${id}/reboot`),
  retry: (id: number, category: string) => post(`${SERVERS_URL}/${id}/capabilities/${encodeURIComponent(category)}/retry`),
  refreshAll: (id: number) => post(`${SERVERS_URL}/${id}/refresh-all`),
  snapsRefreshAll: (id: number) => post(`${SERVERS_URL}/${id}/snaps/refresh-all`),
  snapsRefresh: (id: number, names: string[]) => post(`${SERVERS_URL}/${id}/snaps/refresh`, { names }),
  snapsRemove: (id: number, names: string[]) => post(`${SERVERS_URL}/${id}/snaps/remove`, { names }),
  scanVulnerabilities: (id: number) => api.post(`${SERVERS_URL}/${id}/vulnerabilities/scan`),
  deepScan: (id: number) => api.post(`${SERVERS_URL}/${id}/deep-scan`),
  secureRepositories: (id: number) => api.post(`${SERVERS_URL}/${id}/repositories/secure`),
  restrictSudo: (id: number) => api.post(`${SERVERS_URL}/${id}/restrict-sudo`),
  decommission: (id: number, purge: boolean) => api.post(`${SERVERS_URL}/${id}/decommission`, { purge }),
  updateSettings: (id: number, patch: Record<string, unknown>) => api.patch(`${SERVERS_URL}/${id}/settings`, patch),
  terminalTicket: (id: number) => api.post<{ ticket: string }>(`${SERVERS_URL}/${id}/terminal/ticket`, {}),
};

/** Nach einer Änderung: alles zum Server und die Listen neu laden (die Ereignisse tun dasselbe, das hier ist die Sicherung). */
export function refreshServer(id: number): void {
  store.invalidate(`${SERVERS_URL}/${id}`);
  store.invalidate(SERVERS_URL, { withChildren: false });
  store.invalidate('/api/v1/jobs', { withChildren: false });
}

// ---- Tabs des Serverdetails ----

export type ServerTab =
  | 'overview'
  | 'repositories'
  | 'packages'
  | 'snaps'
  | 'containers'
  | 'apps'
  | 'users'
  | 'security'
  | 'deep-scan'
  | 'storage'
  | 'jobs'
  | 'ssh';

export const SERVER_TABS: readonly ServerTab[] = [
  'overview',
  'repositories',
  'packages',
  'snaps',
  'containers',
  'apps',
  'users',
  'security',
  'deep-scan',
  'storage',
  'jobs',
  'ssh',
];

/** Reine API-Geräte (RouterOS, Synology DSM) haben keine Shell und keine Paketverwaltung. */
/** VirtualDSM: Synologys DSM als virtuelle Maschine - CPU und Platten sind virtuell. */
export function isVirtualDsm(server: Pick<ServerItem, 'os_id' | 'dsm_model'>): boolean {
  return server.os_id === 'dsm' && /virtualdsm/i.test(server.dsm_model);
}

/** Adresse der DSM-Oberfläche: HTTPS auf dem DSM-Port (Standard 5001), IPv6 in eckigen Klammern. */
export function dsmUrl(server: Pick<ServerItem, 'host' | 'dsm_port'>): string {
  const host = server.host.includes(':') && !server.host.startsWith('[') ? `[${server.host}]` : server.host;
  return `https://${host}:${server.dsm_port || 5001}/`;
}

export function isApiDevice(server: Pick<ServerItem, 'os_id'>): boolean {
  return server.os_id === 'routeros' || server.os_id === 'dsm';
}

/** Eine Konsole gibt es nur über SSH und nur, solange sie am Server nicht abgeschaltet ist. */
export function hasConsole(server: Pick<ServerItem, 'os_id' | 'transport' | 'console_disabled'>): boolean {
  return !isApiDevice(server) && server.transport !== 'agent' && !server.console_disabled;
}

/** Die Reiter, die zu diesem Server und zu den Rechten des Benutzers passen (wie in der alten Oberfläche). */
export function visibleTabs(server: Pick<ServerItem, 'os_id' | 'has_snap' | 'has_docker'>, can: (permission: string) => boolean = () => true): ServerTab[] {
  const device = isApiDevice(server);
  return SERVER_TABS.filter((tab) => {
    if (tab === 'jobs') return can('jobs:read');
    if (tab === 'repositories' || tab === 'users' || tab === 'deep-scan') return !device;
    // DSM-Pakete stehen unter Pakete; einen eigenen App-Bestand gibt es dort nicht.
    if (tab === 'apps') return server.os_id !== 'dsm';
    if (tab === 'snaps') return server.has_snap;
    if (tab === 'containers') return server.has_docker;
    return true;
  });
}

export function parseTab(value: string | undefined): ServerTab {
  return (SERVER_TABS as readonly string[]).includes(value ?? '') ? (value as ServerTab) : 'overview';
}

export function serverPath(id: number, tab: ServerTab | '' = ''): string {
  return tab && tab !== 'overview' ? `/servers/${id}/${tab}` : `/servers/${id}`;
}
