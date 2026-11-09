// Serverdetail: was die Übersicht über die Serverliste hinaus braucht (Kernel, LCM-Host, Gruppen,
// seltene Server-Aktionen) und die reinen Regeln, mit denen die Übersicht Rohwerte einordnet.

import { api } from '../lib/api';
import { GROUPS_URL, SERVERS_URL, type ServerItem, type StartedJob } from './servers';

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null;
}

function text(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function list(value: unknown): Raw[] {
  return Array.isArray(value) ? value.filter(isRecord) : [];
}

export const serverStatusUrl = (id: number) => `${SERVERS_URL}/${id}/status`;
export const lcmHostUrl = (id: number) => `${SERVERS_URL}/${id}/lcm-host/status`;
export const CONSOLES_URL = `${SERVERS_URL}/consoles`;

// ---- Kernel und Docker-Ports (`GET /servers/:id/status`) ----

export interface KernelPackage {
  name: string;
  release: string;
  running: boolean;
}

export interface KernelInfo {
  running: string;
  installed: KernelPackage[];
  managed: boolean;
  container: string;
  reboot_pending: boolean;
  removable: string[];
}

export interface PortExposure {
  container: string;
  host_ip: string;
  host_port: string;
  proto: string;
}

export interface ServerStatusDetail {
  kernel: KernelInfo | null;
  exposures: PortExposure[];
}

export function normaliseStatusDetail(raw: unknown): ServerStatusDetail {
  const r = isRecord(raw) ? raw : {};
  const k = isRecord(r.kernel) ? r.kernel : null;
  const kernel = k
    ? {
        running: text(k.running),
        installed: list(k.installed).map((p) => ({ name: text(p.name), release: text(p.release), running: p.running === true })),
        managed: k.managed === true,
        container: text(k.container),
        reboot_pending: k.reboot_pending === true,
        removable: list(k.removable).map((p) => text(p.name)),
      }
    : null;
  const exposures = list(r.docker_port_exposures).map((e) => ({
    container: text(e.container),
    host_ip: text(e.host_ip),
    host_port: text(e.host_port),
    proto: text(e.proto),
  }));
  return { kernel, exposures };
}

/** Ein Port an der Firewall vorbei. `addresses` leer: an keine bestimmte Adresse gebunden, er gilt für jede Schnittstelle. */
export interface ExposedPort {
  port: string;
  addresses: string[];
}

/** Die Ports eines Containers, die an der Firewall vorbei erreichbar sind. */
export interface ExposureGroup {
  container: string;
  ports: ExposedPort[];
}

export interface ExposureSummary {
  /** Verschiedene Ports (Port/Protokoll); IPv4 und IPv6 desselben Ports zählen einmal. */
  count: number;
  /** Davon an keine bestimmte Adresse gebunden – sie gelten für jede Schnittstelle. */
  unbound: number;
  groups: ExposureGroup[];
}

/** Docker meldet „alle Adressen“ als 0.0.0.0 bzw. ::. */
const ANY_ADDRESS = new Set(['', '0.0.0.0', '::', '[::]', '*']);

interface PortBinding {
  any: boolean;
  addresses: Set<string>;
}

/** Fasst die Port-Freigaben zusammen: je Container eine Zeile statt je Port und Adresse. */
export function summariseExposures(exposures: PortExposure[]): ExposureSummary {
  const unbound = new Set<string>();
  const ports = new Set<string>();
  const groups = new Map<string, Map<string, PortBinding>>();
  for (const e of exposures) {
    const port = `${e.host_port}/${e.proto}`;
    const any = ANY_ADDRESS.has(e.host_ip);
    ports.add(port);
    if (any) unbound.add(port);
    const byPort = groups.get(e.container) ?? new Map<string, PortBinding>();
    const binding = byPort.get(port) ?? { any: false, addresses: new Set<string>() };
    if (any) binding.any = true;
    else binding.addresses.add(e.host_ip);
    byPort.set(port, binding);
    groups.set(e.container, byPort);
  }
  return {
    count: ports.size,
    unbound: unbound.size,
    groups: [...groups].map(([container, byPort]) => ({
      container,
      ports: [...byPort].map(([port, b]) => ({ port, addresses: b.any ? [] : [...b.addresses] })),
    })),
  };
}

/** Zustand eines installierten Kernels: läuft, wartet auf Neustart (neuer als der laufende), entfernbar oder Rückfallebene. */
export type KernelState = 'running' | 'awaiting' | 'removable' | 'fallback';

export function kernelStates(kernel: KernelInfo): KernelState[] {
  const runningIndex = kernel.installed.findIndex((k) => k.running);
  return kernel.installed.map((k, i) => {
    if (k.running) return 'running';
    if (runningIndex >= 0 && i < runningIndex) return 'awaiting';
    return kernel.removable.includes(k.name) ? 'removable' : 'fallback';
  });
}

// ---- LCM-Host ----

export interface ScannerInfo {
  available: boolean;
  version: string;
  /** Neuere Trivy-Version aus dem Paketbestand des Hosts; leer, wenn aktuell. */
  update_version: string;
  updated_at: string | null;
  freshness: string;
  sandboxed: boolean;
  sandbox_backend: string;
  sandbox_note: string;
}

export interface LcmHostStatus {
  is_lcm_host: boolean;
  package_manager: string;
  in_container: boolean;
  trivy_installed: boolean;
  sandbox_retrofit: boolean;
  apt_cacher_installed: boolean;
  crowdsec_lapi_installed: boolean;
  scanner: ScannerInfo | null;
}

export function normaliseLcmHost(raw: unknown): LcmHostStatus {
  const r = isRecord(raw) ? raw : {};
  const s = isRecord(r.scanner) ? r.scanner : null;
  return {
    is_lcm_host: r.is_lcm_host === true,
    package_manager: text(r.package_manager),
    in_container: r.in_container === true,
    trivy_installed: r.trivy_installed === true,
    sandbox_retrofit: r.sandbox_retrofit === true,
    apt_cacher_installed: r.apt_cacher_installed === true,
    crowdsec_lapi_installed: r.crowdsec_lapi_installed === true,
    scanner: s
      ? {
          available: s.available === true,
          version: text(s.version),
          update_version: text(s.update_version),
          updated_at: text(s.updated_at) || null,
          freshness: text(s.freshness) || 'unknown',
          sandboxed: s.sandboxed === true,
          sandbox_backend: text(s.sandbox_backend),
          sandbox_note: text(s.sandbox_note),
        }
      : null,
  };
}

/** Was die LCM-Host-Karte zeigt: im Container nur eine Erklärung, ohne apt nur den Hinweis, sonst die Einrichtungen. */
export type LcmHostMode = 'container' | 'not-apt' | 'setup';

export function lcmHostMode(status: LcmHostStatus): LcmHostMode {
  if (status.in_container) return 'container';
  return status.package_manager === 'apt' ? 'setup' : 'not-apt';
}

// ---- Offene Konsolen ----

export interface OpenConsole {
  server_id: number;
  actor: string;
  since: string;
}

export function normaliseConsoles(raw: unknown, serverId: number): OpenConsole[] {
  return list(raw)
    .map((c) => ({ server_id: typeof c.server_id === 'number' ? c.server_id : 0, actor: text(c.actor), since: text(c.since) }))
    .filter((c) => c.server_id === serverId);
}

// ---- Gruppen ----

/** Welche Gruppen beim Speichern hinzukommen und welche wegfallen. */
export function groupChanges(current: readonly number[], chosen: readonly number[]): { add: number[]; remove: number[] } {
  return { add: chosen.filter((id) => !current.includes(id)), remove: current.filter((id) => !chosen.includes(id)) };
}

/** Gruppen nach Namen filtern (Groß-/Kleinschreibung egal). */
export function filterByName<T extends { name: string }>(items: readonly T[], query: string): T[] {
  const q = query.trim().toLowerCase();
  return q ? items.filter((item) => item.name.toLowerCase().includes(q)) : [...items];
}

export const groupsApi = {
  assign: (groupId: number, serverId: number) => api.post(`${GROUPS_URL}/${groupId}/assign-server`, { server_id: serverId }),
  remove: (groupId: number, serverId: number) => api.post(`${GROUPS_URL}/${groupId}/remove-server`, { server_id: serverId }),
};

// ---- Apps: was sich an einer erkannten App auslösen lässt ----

export interface AppActions {
  slug: string;
  can_backup: boolean;
  can_update: boolean;
}

/** Aktionen je App (Schlüssel wie `AppItem.id`: ID, sonst Slug). */
export function normaliseAppActions(raw: unknown): Map<string, AppActions> {
  const r = isRecord(raw) ? raw : {};
  const rows = list(r.detected).map((a) => [
    String(typeof a.id === 'number' && a.id ? a.id : text(a.slug)),
    { slug: text(a.slug), can_backup: a.can_backup === true, can_update: a.can_update === true },
  ] as const);
  return new Map(rows);
}

// ---- Seltene Server-Aktionen ----

function started(raw: unknown): StartedJob {
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

export const detailApi = {
  refreshHardware: async (id: number) => started(await api.post(`${SERVERS_URL}/${id}/refresh-hardware`)),
  hardenSsh: (id: number) => api.post(`${SERVERS_URL}/${id}/harden-ssh`),
  unhardenSsh: (id: number) => api.post(`${SERVERS_URL}/${id}/unharden-ssh`),
  rotateKey: (id: number) => api.post(`${SERVERS_URL}/${id}/rotate-key`),
  syncUsers: (id: number) => api.post(`${SERVERS_URL}/${id}/sync-users`),
  dnsTest: (id: number) => api.post(`${SERVERS_URL}/${id}/dns-test`),
  regenerateAgentToken: async (id: number) => text((await api.post<Raw>(`${SERVERS_URL}/${id}/agent-token`)).token),
  runAppAction: async (id: number, slug: string, action: 'backup' | 'update') =>
    started(await api.post(`${SERVERS_URL}/${id}/apps/${encodeURIComponent(slug)}/${action}`)),
  installTrivy: async (id: number) => started(await api.post(`${SERVERS_URL}/${id}/lcm-host/install-trivy`)),
  installSandbox: async (id: number) => started(await api.post(`${SERVERS_URL}/${id}/lcm-host/install-sandbox`)),
  updateTrivy: async (id: number) => started(await api.post(`${SERVERS_URL}/${id}/lcm-host/update-trivy`)),
  installAptCacher: async (id: number) => started(await api.post(`${SERVERS_URL}/${id}/lcm-host/install-apt-cacher`)),
  installCrowdSecLapi: async (id: number, bouncer: boolean) => started(await api.post(`${SERVERS_URL}/${id}/lcm-host/install-crowdsec-lapi`, { bouncer })),
};

/** Befehle, mit denen man den eingeschränkten Modus auf dem Server selbst aufhebt (LCM darf das bewusst nicht). */
export function unrestrictCommands(serviceUser: string): string {
  const user = serviceUser || 'lcm-svc';
  return [
    `echo '${user} ALL=(ALL) NOPASSWD:ALL' | sudo tee /etc/sudoers.d/${user}`,
    `sudo chmod 440 /etc/sudoers.d/${user}`,
    `sudo rm -rf /home/${user}/.lcm/sudo-bin /usr/local/sbin/lcm-helper`,
  ].join('\n');
}

// ---- Einordnung der Rohwerte für die Übersicht ----

const CONTAINER_VIRT = ['lxc', 'lxc-libvirt', 'openvz', 'docker', 'podman', 'systemd-nspawn', 'rkt', 'wsl', 'proot'];
const PRETTY_VIRT: Record<string, string> = {
  kvm: 'KVM',
  qemu: 'QEMU',
  lxc: 'LXC',
  'lxc-libvirt': 'LXC (libvirt)',
  vmware: 'VMware',
  microsoft: 'Hyper-V',
  oracle: 'VirtualBox',
  xen: 'Xen',
};

/** Plattform aus `systemd-detect-virt`: physisch, virtuelle Maschine oder Container. */
export interface Platform {
  kind: 'bare' | 'vm' | 'container' | 'unknown';
  name: string;
}

export function platformOf(virtualization: string): Platform {
  const v = virtualization.trim().toLowerCase();
  if (!v) return { kind: 'unknown', name: '' };
  if (v === 'none') return { kind: 'bare', name: '' };
  const name = PRETTY_VIRT[v] ?? v.charAt(0).toUpperCase() + v.slice(1);
  return { kind: CONTAINER_VIRT.includes(v) ? 'container' : 'vm', name };
}

const PACKAGE_MANAGERS: Record<string, string> = {
  apt: 'APT (dpkg)',
  dnf: 'DNF (RPM)',
  yum: 'YUM (RPM)',
  zypper: 'Zypper (RPM)',
  pacman: 'pacman',
  apk: 'apk (Alpine)',
};

/** Die Paketverwaltungen des Servers: die des Systems und, falls vorhanden, Snap. */
export function packageManagers(server: Pick<ServerItem, 'package_manager' | 'has_snap'>): string[] {
  const main = server.package_manager ? (PACKAGE_MANAGERS[server.package_manager] ?? server.package_manager) : '';
  return [main, server.has_snap ? 'Snap' : ''].filter(Boolean);
}

/** Kommagetrennte Liste (IP-Adressen, Resolver, Zeitserver) als einzelne Einträge. */
export function splitList(value: string): string[] {
  return value
    .split(/[,\s]+/)
    .map((part) => part.trim())
    .filter(Boolean);
}

/** Ab dieser Abweichung gilt die Uhr als falsch (wie im Backend). */
export const CLOCK_WARN_SECONDS = 30;

export type ClockState = 'unknown' | 'ok' | 'ahead' | 'behind';

export function clockState(server: Pick<ServerItem, 'time_checked_at' | 'clock_offset_seconds'>): ClockState {
  if (!server.time_checked_at) return 'unknown';
  const offset = server.clock_offset_seconds;
  if (Math.abs(offset) < CLOCK_WARN_SECONDS) return 'ok';
  return offset > 0 ? 'ahead' : 'behind';
}

/** Ampelstufe des DNS-Tests: alles aufgelöst, teilweise oder gar nicht. */
export function dnsLevel(status: string): 'ok' | 'warn' | 'crit' | 'unknown' {
  if (status === 'full') return 'ok';
  if (status === 'partial') return 'warn';
  if (status === 'none') return 'crit';
  return 'unknown';
}

/** Support-Stand: unbekannt, abgelaufen, läuft bald aus oder unterstützt. */
export type SupportState = 'unknown' | 'eol' | 'eol-soon' | 'supported';

export function supportState(support: ServerItem['os_support']): SupportState {
  if (!support.known) return 'unknown';
  if (!support.supported) return 'eol';
  return support.eol_soon ? 'eol-soon' : 'supported';
}

/** Post-Quanten-Schlüsselaustausch (ML-KEM, sntrup); die Bewertung selbst trifft der Server. */
export function isPostQuantum(kex: string): boolean {
  return /^(mlkem|sntrup)/.test(kex);
}

/** Der Hostname sagt nur etwas, wenn er sich von Anzeigename und Adresse unterscheidet. */
export function distinctHostname(server: Pick<ServerItem, 'hostname' | 'name' | 'host'>): string {
  return server.hostname && server.hostname !== server.name && server.hostname !== server.host ? server.hostname : '';
}
