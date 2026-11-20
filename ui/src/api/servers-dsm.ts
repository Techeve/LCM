// Sicherheitseinstellungen von DSM auf den Sollwert setzen, nur auf Klick: welche Prüfungen LCM setzen kann und was der
// Bestätigungsdialog dazu nennt. Gestartet über `dsmApi` als Job. Dazu die Wörter für die Liste der Firewall-Regeln.

import { api } from '../lib/api';
import { SERVERS_URL, type DsmCheck, type DsmFirewallRule, type StartedJob } from './servers';

/**
 * Was LCM auf den Sollwert setzt, wie domain.DSMSettable und in der Reihenfolge der DSM-Systemsteuerung. Die Firewall
 * fehlt bewusst: Mit unpassenden Regeln eingeschaltet sperrt sie LCM und den Admin aus.
 */
export const SETTABLE_KEYS: readonly string[] = ['skipIpCheck', 'csrf', 'csp', 'iframe', 'sessionClear', 'autoBlock', 'dos', 'tlsProfile', 'spectre'];

export function isSettable(key: string): boolean {
  return SETTABLE_KEYS.includes(key);
}

/** Die Abweichungen, die LCM beheben kann. */
export function settableDeviations(checks: readonly DsmCheck[]): DsmCheck[] {
  return checks.filter((check) => check.state === 'deviation' && isSettable(check.key));
}

/** Der Sollwert je Prüfung in Worten (als Wörterbuch, damit die Prüfung der Sprachdateien die Schlüssel findet). */
const TARGET_KEYS: Record<string, string> = {
  skipIpCheck: 'serverSecurity.device.target.off',
  csrf: 'serverSecurity.device.target.on',
  csp: 'serverSecurity.device.target.on',
  iframe: 'serverSecurity.device.target.on',
  sessionClear: 'serverSecurity.device.target.on',
  autoBlock: 'serverSecurity.device.target.autoBlock',
  dos: 'serverSecurity.device.target.on',
  tlsProfile: 'serverSecurity.device.target.tlsProfile',
  spectre: 'serverSecurity.device.target.on',
};

export function targetKey(key: string): string {
  return TARGET_KEYS[key] ?? 'serverSecurity.device.target.on';
}

/** Zusätzliche Warnungen im Dialog: wo das Setzen Folgen hat, die man vorher kennen sollte. */
const HINT_KEYS: Record<string, string> = {
  dos: 'serverSecurity.device.apply.hint.dos',
  tlsProfile: 'serverSecurity.device.apply.hint.tlsProfile',
  spectre: 'serverSecurity.device.apply.hint.spectre',
};

/** Die Warnungen für die gewählten Prüfungen, in der Reihenfolge der Systemsteuerung. */
export function applyHints(keys: readonly string[]): string[] {
  return SETTABLE_KEYS.filter((key) => keys.includes(key) && HINT_KEYS[key]).map((key) => HINT_KEYS[key]);
}

/** Was eine Firewall-Regel oder die Vorgabe eines Abschnitts tut. DSM schreibt verweigernd als drop oder deny. */
export type FirewallPolicy = 'allow' | 'deny' | 'none';

export function firewallPolicy(policy: string): FirewallPolicy {
  const p = policy.toLowerCase();
  if (p.includes('deny') || p.includes('drop')) return 'deny';
  return p === '' || p === 'none' ? 'none' : 'allow';
}

const isAll = (value: string) => value === '' || value.toLowerCase() === 'all';

/** Die Ports einer Regel, wie DSM sie nennt; leer heißt alle. */
export function firewallPorts(rule: DsmFirewallRule): string {
  return isAll(rule.ports) ? '' : rule.ports.split(',').join(', ');
}

/** Das Protokoll einer Regel; leer heißt alle. */
export function firewallProtocol(rule: DsmFirewallRule): string {
  return isAll(rule.protocol) ? '' : rule.protocol.toUpperCase();
}

/** Die Quelle einer Regel: die Adresse, sonst die Gruppe; leer heißt alle. */
export function firewallSource(rule: DsmFirewallRule): string {
  if (!isAll(rule.source_ip)) return rule.source_ip;
  return isAll(rule.source_ip_group) ? '' : rule.source_ip_group;
}

export const dsmApplyUrl = (id: number) => `${SERVERS_URL}/${id}/dsm/apply`;

function started(raw: unknown): StartedJob {
  const r = typeof raw === 'object' && raw !== null ? (raw as Record<string, unknown>) : {};
  const text = (value: unknown) => (typeof value === 'string' ? value : '');
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

export const dsmApi = {
  apply: async (id: number, keys: readonly string[]) => started(await api.post<unknown>(dsmApplyUrl(id), { keys })),
};
