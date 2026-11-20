// Portscan von LCM aus: welche gängigen TCP-Ports ein Server von LCMs Standort aus wirklich anbietet, bewertet gegen den
// LCM-Regelsatz der Firewall. Gelesen über den Datenspeicher, gestartet über `portScanApi`.

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

function strings(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((entry): entry is string => typeof entry === 'string') : [];
}

function numbers(value: unknown): number[] {
  return Array.isArray(value) ? value.filter((entry): entry is number => typeof entry === 'number') : [];
}

export const portScanUrl = (id: number) => `${SERVERS_URL}/${id}/port-scan`;

const LOOPBACK = ['localhost', '127.0.0.1', '::1'];

/** Ob LCM den Server scannen kann - wie im Backend: eine Host-Angabe, die nicht auf LCM selbst zeigt. */
export function canPortScan(server: { host: string; transport: string }): boolean {
  const host = server.host.trim().toLowerCase();
  return server.transport !== 'agent' && host !== '' && !LOOPBACK.includes(host);
}

export type PortSeverity = 'critical' | 'warning' | 'ok';
export type PortReason = 'risky_service' | 'not_in_firewall';

export interface PortScanTarget {
  address: string;
  family: string;
  open: number[];
  closed: number;
  filtered: number;
}

export interface OpenPort {
  port: number;
  service: string;
  families: string[];
  /** `null`: kein Abgleich möglich (DSM, RouterOS). */
  inFirewall: boolean | null;
  risky: boolean;
  severity: PortSeverity;
  reasons: PortReason[];
}

export interface PortScan {
  scannedAt: string | null;
  jobId: string;
  targets: PortScanTarget[];
  ports: OpenPort[];
  firewallCompared: boolean;
  firewallActive: boolean;
  critical: number;
  warnings: number;
}

const SEVERITIES: readonly PortSeverity[] = ['critical', 'warning', 'ok'];
const REASONS: readonly PortReason[] = ['risky_service', 'not_in_firewall'];

const severity = (value: unknown): PortSeverity => (SEVERITIES.includes(value as PortSeverity) ? (value as PortSeverity) : 'ok');

function normaliseTarget(r: Raw): PortScanTarget {
  return { address: text(r.address), family: text(r.family), open: numbers(r.open), closed: num(r.closed), filtered: num(r.filtered) };
}

function normalisePort(r: Raw): OpenPort {
  return {
    port: num(r.port),
    service: text(r.service),
    families: strings(r.families),
    inFirewall: typeof r.in_firewall === 'boolean' ? r.in_firewall : null,
    risky: r.risky === true,
    severity: severity(r.severity),
    reasons: strings(r.reasons).filter((x): x is PortReason => REASONS.includes(x as PortReason)),
  };
}

export function normalisePortScan(raw: unknown): PortScan {
  const r = isRecord(raw) ? raw : {};
  return {
    scannedAt: text(r.scanned_at) || null,
    jobId: text(r.job_id),
    targets: list(r.targets).map(normaliseTarget),
    ports: list(r.open_ports).map(normalisePort),
    firewallCompared: r.firewall_compared === true,
    firewallActive: r.firewall_active === true,
    critical: num(r.critical),
    warnings: num(r.warnings),
  };
}

/** Anzeigename je Dienst (Protokollnamen, nicht übersetzt); unbekannte Dienste zeigen die Kennung. */
const SERVICE_NAMES: Record<string, string> = {
  ftp: 'FTP',
  ssh: 'SSH',
  telnet: 'Telnet',
  http: 'HTTP',
  rpcbind: 'rpcbind',
  netbios: 'NetBIOS',
  ldap: 'LDAP',
  https: 'HTTPS',
  smb: 'SMB',
  ldaps: 'LDAPS',
  mqtt: 'MQTT',
  nfs: 'NFS',
  docker: 'Docker',
  'docker-tls': 'Docker (TLS)',
  mysql: 'MySQL/MariaDB',
  rdp: 'RDP',
  'dsm-http': 'DSM (HTTP)',
  'dsm-https': 'DSM (HTTPS)',
  postgresql: 'PostgreSQL',
  vnc: 'VNC',
  redis: 'Redis',
  kubernetes: 'Kubernetes API',
  proxmox: 'Proxmox VE',
  'http-alt': 'HTTP (8080)',
  winbox: 'Winbox',
  'routeros-api': 'RouterOS API',
  'routeros-api-tls': 'RouterOS API (TLS)',
  'mqtt-tls': 'MQTT (TLS)',
  kubelet: 'Kubelet',
  mongodb: 'MongoDB',
};

export function serviceName(service: string): string {
  return SERVICE_NAMES[service] ?? service;
}

/** Hinweis je Dienst als Übersetzungsschlüssel; die Schlüssel stehen als Wörterbuch hier, damit die Prüfung der Sprachdateien sie findet. */
const HINT_KEYS: Record<string, string> = {
  ftp: 'serverSecurity.portScan.hint.ftp',
  ssh: 'serverSecurity.portScan.hint.ssh',
  telnet: 'serverSecurity.portScan.hint.telnet',
  http: 'serverSecurity.portScan.hint.http',
  rpcbind: 'serverSecurity.portScan.hint.rpcbind',
  netbios: 'serverSecurity.portScan.hint.netbios',
  ldap: 'serverSecurity.portScan.hint.ldap',
  https: 'serverSecurity.portScan.hint.https',
  smb: 'serverSecurity.portScan.hint.smb',
  ldaps: 'serverSecurity.portScan.hint.ldaps',
  mqtt: 'serverSecurity.portScan.hint.mqtt',
  nfs: 'serverSecurity.portScan.hint.nfs',
  docker: 'serverSecurity.portScan.hint.docker',
  'docker-tls': 'serverSecurity.portScan.hint.docker-tls',
  mysql: 'serverSecurity.portScan.hint.database',
  postgresql: 'serverSecurity.portScan.hint.database',
  mongodb: 'serverSecurity.portScan.hint.database',
  redis: 'serverSecurity.portScan.hint.redis',
  rdp: 'serverSecurity.portScan.hint.rdp',
  'dsm-http': 'serverSecurity.portScan.hint.dsm-http',
  'dsm-https': 'serverSecurity.portScan.hint.dsm-https',
  vnc: 'serverSecurity.portScan.hint.vnc',
  kubernetes: 'serverSecurity.portScan.hint.kubernetes',
  kubelet: 'serverSecurity.portScan.hint.kubelet',
  proxmox: 'serverSecurity.portScan.hint.admin',
  winbox: 'serverSecurity.portScan.hint.admin',
  'http-alt': 'serverSecurity.portScan.hint.http-alt',
  'routeros-api': 'serverSecurity.portScan.hint.routeros-api',
  'routeros-api-tls': 'serverSecurity.portScan.hint.routeros-api-tls',
  'mqtt-tls': 'serverSecurity.portScan.hint.mqtt-tls',
};

export function hintKey(service: string): string | null {
  return HINT_KEYS[service] ?? null;
}

const REASON_KEYS: Record<PortReason, string> = {
  risky_service: 'serverSecurity.portScan.reason.risky_service',
  not_in_firewall: 'serverSecurity.portScan.reason.not_in_firewall',
};

export function reasonKey(reason: PortReason): string {
  return REASON_KEYS[reason];
}

const SEVERITY_KEYS: Record<PortSeverity, string> = {
  critical: 'serverSecurity.portScan.severity.critical',
  warning: 'serverSecurity.portScan.severity.warning',
  ok: 'serverSecurity.portScan.severity.ok',
};

export function severityKey(value: PortSeverity): string {
  return SEVERITY_KEYS[value];
}

/** Adressfamilien in Worten. */
export function familyLabel(families: readonly string[]): string {
  return families.map((f) => (f === 'v6' ? 'IPv6' : 'IPv4')).join(', ');
}

/** Adressen, an denen kein einziger Port antwortete - nicht erreichbar oder alles verworfen. */
export function silentTargets(targets: readonly PortScanTarget[]): string[] {
  return targets.filter((t) => t.open.length === 0 && t.closed === 0 && t.filtered > 0).map((t) => t.address);
}

function started(raw: unknown): StartedJob {
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

export const portScanApi = {
  start: async (id: number) => started(await api.post<unknown>(portScanUrl(id))),
};
