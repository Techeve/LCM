// Sicherheit eines Servers: Firewall, Sicherheits-Tools (fail2ban, CrowdSec), SSH-2FA, ACL und abgeschottete
// Verzeichnisse sowie die CVE-Funde samt Quelle. Gelesen wird über den Datenspeicher, geändert über `securityApi`.

import { api } from '../lib/api';
import { SERVERS_URL, type StartedJob } from './servers';

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function num(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function list(value: unknown): Raw[] {
  return Array.isArray(value) ? value.filter(isRecord) : [];
}

function ids(value: unknown): number[] {
  return Array.isArray(value) ? value.filter((id): id is number => typeof id === 'number') : [];
}

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === 'string') : [];
}

/** JSON aus einem Textfeld des Servers; kaputtes oder leeres JSON ergibt `fallback`. */
function parseJson(value: unknown, fallback: unknown): unknown {
  const raw = text(value).trim();
  if (!raw) return fallback;
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return fallback;
  }
}

export const serverDetailUrl = (id: number) => `${SERVERS_URL}/${id}`;
export const hardenedPathsUrl = (id: number) => `${SERVERS_URL}/${id}/hardened-paths`;
export const bansUrl = (id: number, tool: SecurityTool) => `${SERVERS_URL}/${id}/security-tool/bans?tool=${tool}`;

export type SecurityTool = 'fail2ban' | 'crowdsec';
export const SECURITY_TOOLS: readonly SecurityTool[] = ['fail2ban', 'crowdsec'];
export const TOOL_NAMES: Record<SecurityTool, string> = { fail2ban: 'fail2ban', crowdsec: 'CrowdSec' };

// ---- Der Server aus `GET /servers/:id` ----

export interface SshSources {
  allowlist_ids: number[];
  source_ips: string[];
}

export interface ListeningPort {
  port: number;
  proto: string;
  bind: string;
  ip_version: string;
  process: string;
}

export interface ServerSecurity {
  name: string;
  sshPort: number;
  restrictedSudo: boolean;
  proxmox: boolean;
  firewallActive: boolean;
  firewallTool: string;
  firewallBackend: string;
  /** Maßgebliche Regeln als JSON-Text (leer: nur die alte Portliste ist bekannt). */
  firewallRules: string;
  firewallPorts: string;
  sshSources: SshSources;
  listening: ListeningPort[];
  lcmSourceIp: string;
  sshHardened: boolean;
  rootLoginDisabled: boolean;
  twoFactor: boolean;
  installed: Record<SecurityTool, boolean>;
  active: Record<SecurityTool, boolean>;
  hasAcl: boolean;
  aclUsable: boolean;
  listeningPackages: string;
}

export function normaliseListening(raw: unknown): ListeningPort[] {
  return list(raw).map((p) => ({ port: num(p.port), proto: text(p.proto) || 'tcp', bind: text(p.bind), ip_version: text(p.ip_version), process: text(p.process) }));
}

export function normaliseServerSecurity(raw: unknown): ServerSecurity {
  const r = isRecord(raw) ? raw : {};
  const sources = parseJson(r.firewall_ssh_sources, {});
  const s = isRecord(sources) ? sources : {};
  return {
    name: text(r.name),
    sshPort: num(r.ssh_port) || 22,
    restrictedSudo: r.restricted_sudo === true,
    proxmox: text(r.proxmox_type) !== '',
    firewallActive: r.firewall_active === true,
    firewallTool: text(r.firewall_tool),
    firewallBackend: text(r.firewall_tool) || text(r.firewall_backend) || 'ufw',
    firewallRules: text(r.firewall_rules),
    firewallPorts: text(r.firewall_allowed_ports),
    sshSources: { allowlist_ids: ids(s.allowlist_ids), source_ips: strings(s.source_ips) },
    listening: normaliseListening(parseJson(r.listening_ports, [])),
    lcmSourceIp: text(r.lcm_source_ip),
    sshHardened: r.ssh_hardened === true,
    rootLoginDisabled: r.ssh_root_login_disabled === true,
    twoFactor: r.ssh_2fa_enabled === true,
    installed: { fail2ban: r.fail2ban_installed === true, crowdsec: r.crowdsec_installed === true },
    active: { fail2ban: r.fail2ban_active === true, crowdsec: r.crowdsec_active === true },
    hasAcl: r.has_acl === true,
    aclUsable: r.acl_usable === true,
    listeningPackages: text(r.listening_packages),
  };
}

/** Von laufenden Containern veröffentlichte Host-Ports („0.0.0.0:8081->80/tcp“ → 8081), aufsteigend. */
export function publishedPorts(containers: readonly { state: string; ports: string }[]): number[] {
  const ports = new Set<number>();
  for (const container of containers) {
    if (container.state !== 'running') continue;
    for (const match of container.ports.matchAll(/:(\d+)->/g)) ports.add(Number(match[1]));
  }
  return [...ports].sort((a, b) => a - b);
}

// ---- CVE-Funde mit Quelle ----

export type VulnSource = '' | 'os' | 'docker';

export interface SecurityVuln {
  id: string;
  cve: string;
  pkg: string;
  installed: string;
  fixed: string;
  severity: string;
  title: string;
  url: string;
  source: 'os' | 'docker';
  imageRef: string;
  unused: boolean;
}

export interface SecurityVulnData {
  items: SecurityVuln[];
  summary: Record<string, number>;
  /** Funde aus Images, die kein Container nutzt: angezeigt, aber nicht mitgezählt. */
  unusedCount: number;
  lastScan: string | null;
  scannerAvailable: boolean;
}

function counts(value: unknown): Record<string, number> {
  return isRecord(value) ? Object.fromEntries(Object.entries(value).map(([k, v]) => [k, num(v)])) : {};
}

export function normaliseSecurityVulns(raw: unknown): SecurityVulnData {
  const r = isRecord(raw) ? raw : {};
  return {
    items: list(r.vulnerabilities).map((v, index) => ({
      id: text(v.id) || String(index),
      cve: text(v.cve_id),
      pkg: text(v.package_name),
      installed: text(v.installed_version),
      fixed: text(v.fixed_version),
      severity: text(v.severity).toLowerCase(),
      title: text(v.title),
      url: text(v.primary_url),
      source: text(v.source) === 'docker' ? 'docker' : 'os',
      imageRef: text(v.image_ref),
      unused: v.image_unused === true,
    })),
    summary: counts(r.summary),
    unusedCount: Object.values(counts(r.unused_summary)).reduce((sum, n) => sum + n, 0),
    lastScan: text(r.last_scan_at) || null,
    scannerAvailable: r.scanner_available !== false,
  };
}

export function parseVulnSource(value: string | null | undefined): VulnSource {
  return value === 'os' || value === 'docker' ? value : '';
}

export function filterVulns(items: readonly SecurityVuln[], source: VulnSource): SecurityVuln[] {
  return source ? items.filter((v) => v.source === source) : [...items];
}

// ---- Sperren, Härtung ----

export interface SecurityBan {
  id: string;
  ip: string;
  scope: string;
  since: string;
  cause: string;
}

export function normaliseBans(raw: unknown): SecurityBan[] {
  const r = isRecord(raw) ? raw : {};
  return list(r.bans).map((b) => ({ id: `${text(b.scope)}|${text(b.ip)}`, ip: text(b.ip), scope: text(b.scope), since: text(b.since), cause: text(b.cause) }));
}

export interface HardenedPath {
  id: number;
  path: string;
  prevMode: string;
  prevGroup: string;
  mode: string;
  group: string;
  unit: string;
}

export function normaliseHardenedPaths(raw: unknown): HardenedPath[] {
  return list(raw).map((p) => ({
    id: num(p.id),
    path: text(p.path),
    prevMode: text(p.prev_mode),
    prevGroup: text(p.prev_group),
    mode: text(p.mode),
    group: text(p.group),
    unit: text(p.unit),
  }));
}

export interface HardenSuggestion {
  path: string;
  mode: string;
  group: string;
  serviceGroup: string;
  kind: 'config' | 'data';
}

export function normaliseSuggestions(raw: unknown): HardenSuggestion[] {
  return list(raw).map((s) => ({
    path: text(s.path),
    mode: text(s.mode),
    group: text(s.group),
    serviceGroup: text(s.service_group),
    kind: text(s.kind) === 'konfig' ? 'config' : 'data',
  }));
}

export interface HardenTarget {
  path: string;
  group: string;
  unit: string;
}

export interface HardenResult {
  path: string;
  ok: boolean;
  message: string;
}

export function normaliseHardenResults(raw: unknown): HardenResult[] {
  return list(raw).map((r) => ({ path: text(r.path), ok: r.ok === true, message: text(r.message) }));
}

// ---- Ändernde Aufrufe ----

export interface FirewallRulePayload {
  port: number;
  proto: 'tcp' | 'udp';
  ip_version: 'any' | 'v4' | 'v6';
  allowlist_ids?: number[];
  source_ips?: string[];
  comment?: string;
}

export type LapiMode = 'local' | 'remote' | 'console';

export interface SecurityToolInstall {
  tool: SecurityTool;
  allowlist_ips: string[];
  allowlist_ids: number[];
  bouncer?: boolean;
  collections?: string[];
  lapi_mode?: LapiMode;
  lapi_id?: number;
}

export type ManageAction = 'start' | 'stop' | 'restart' | 'enable' | 'disable' | 'uninstall' | 'allowlist' | 'unban';

export interface ManageInput {
  tool: SecurityTool;
  action: ManageAction;
  unban_ip?: string;
  allowlist_ips?: string[];
  allowlist_ids?: number[];
}

function started(raw: unknown): StartedJob {
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

const post = async (url: string, body?: unknown) => started(await api.post<unknown>(url, body));

/** Freitext mit Komma oder Leerzeichen als Liste. */
export function splitList(value: string): string[] {
  return value.split(/[\s,]+/).filter(Boolean);
}

export const securityApi = {
  configureFirewall: (id: number, enable: boolean, rules: FirewallRulePayload[], sshSources: SshSources | null) =>
    post(`${SERVERS_URL}/${id}/firewall`, { enable, rules, ssh_sources: sshSources }),
  scanListeningPorts: async (id: number) => normaliseListening(((await api.post<unknown>(`${SERVERS_URL}/${id}/listening-ports/scan`)) as Raw | null)?.listening_ports),
  installTool: (id: number, input: SecurityToolInstall) => post(`${SERVERS_URL}/${id}/security-tool`, input),
  manageTool: (id: number, input: ManageInput) => post(`${SERVERS_URL}/${id}/security-tool/manage`, input),
  configureSsh2fa: (id: number, enable: boolean) => post(`${SERVERS_URL}/${id}/ssh-2fa`, { enable }),
  installAcl: (id: number) => post(`${SERVERS_URL}/${id}/acl/install`),
  hardenSuggestions: async (id: number) => normaliseSuggestions(await api.get<unknown>(`${SERVERS_URL}/${id}/harden-suggestions`)),
  hardenPath: (id: number, target: HardenTarget) => api.post<unknown>(hardenedPathsUrl(id), target),
  hardenPaths: async (id: number, targets: HardenTarget[]) => normaliseHardenResults(await api.post<unknown>(`${hardenedPathsUrl(id)}/bulk`, { targets })),
  restorePath: (id: number, pathId: number) => api.delete(`${hardenedPathsUrl(id)}/${pathId}`),
};
