// Regeln des Firewall-Editors eines Servers: Startzustand aus dem Server, Vorschläge aus den lauschenden Diensten,
// Schutz vor dem Aussperren und die Nutzlast für `POST /servers/:id/firewall`. Die Zeilen selbst teilt sich der Editor
// mit den Firewall-Regeln der Gruppen (`groups/rule-model`).

import type { FirewallRulePayload, ListeningPort, ServerSecurity, SshSources } from '../../api/servers-security';
import { splitList } from '../../api/servers-security';
import { emptyFirewallRow, firewallErrors, firewallRules, hasRowErrors, parseFirewall, protoSuffix, type FirewallRow, type FirewallRowErrors } from '../groups/rule-model';

/** Die SSH-Freigabe im Editor: Allowlists und eigene Adressen als Freitext. */
export interface SshSourcesForm {
  allowlist_ids: number[];
  source_ips: string;
}

/** Maßgeblich sind die gespeicherten Regeln; fehlen sie, zählt die alte Portliste (nur TCP). */
export function initialRows(security: Pick<ServerSecurity, 'firewallRules' | 'firewallPorts'>): FirewallRow[] {
  const rows = parseFirewall(security.firewallRules);
  return rows.length > 0 || !security.firewallPorts ? rows : parseFirewall(security.firewallPorts);
}

export function initialSshSources(sources: SshSources): SshSourcesForm {
  return { allowlist_ids: [...sources.allowlist_ids], source_ips: sources.source_ips.join(', ') };
}

const isWildcard = (bind: string) => bind === '0.0.0.0' || bind === '::';

/**
 * Lauschende Dienste, die noch keine Regel haben. SSH ist immer frei, Docker-Ports umgehen die Firewall ohnehin;
 * die Sockets eines Dienstes auf 0.0.0.0 und :: ergeben einen Vorschlag.
 */
export function listeningSuggestions(listening: readonly ListeningPort[], rows: readonly FirewallRow[], sshPort: number, dockerPorts: readonly number[]): ListeningPort[] {
  const seen = new Set<string>();
  const docker = new Set(dockerPorts);
  return listening.filter((l) => {
    if (l.port === sshPort || docker.has(l.port)) return false;
    if (rows.some((row) => Number(row.port) === l.port && (row.proto === 'both' || row.proto === l.proto))) return false;
    const key = `${l.port}/${l.proto}/${isWildcard(l.bind) ? '*' : l.bind}`;
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

/** Eine Regel aus einem Vorschlag: der Dienstname als Bemerkung, die Adressfamilie nach der Bindung. */
export function ruleFromListening(l: ListeningPort): FirewallRow {
  const version = isWildcard(l.bind) ? 'any' : l.ip_version === 'v4' || l.ip_version === 'v6' ? l.ip_version : 'any';
  return { ...emptyFirewallRow(), port: String(l.port), proto: l.proto === 'udp' ? 'udp' : 'tcp', ip_version: version, comment: l.process };
}

/** Fehler der eigenen Adressen der SSH-Freigabe, geprüft wie die Quellen jeder anderen Regel. */
export function sshSourceErrors(form: Pick<SshSourcesForm, 'source_ips'>): FirewallRowErrors {
  return firewallErrors([{ ...emptyFirewallRow(), port: '22', source_ips: form.source_ips }])[0] as FirewallRowErrors;
}

export function sshSourcesInvalid(form: SshSourcesForm): boolean {
  return sshSourceErrors(form).sources;
}

/** SSH auf eigene Adressen beschränkt, ohne die Adresse, über die LCM den Server erreicht. Allowlists kennt nur der Server. */
export function sshLocksOut(form: SshSourcesForm, lcmSourceIp: string): boolean {
  const ips = splitList(form.source_ips);
  if (ips.length === 0 || form.allowlist_ids.length > 0) return false;
  const ip = lcmSourceIp.trim();
  return ip !== '' && !ips.includes(ip);
}

export function withLcmAddress(form: SshSourcesForm, lcmSourceIp: string): SshSourcesForm {
  const ips = splitList(form.source_ips);
  const ip = lcmSourceIp.trim();
  return ip && !ips.includes(ip) ? { ...form, source_ips: [...ips, ip].join(', ') } : form;
}

export function hasEditorErrors(rows: readonly FirewallRow[], ssh: SshSourcesForm): boolean {
  return sshSourcesInvalid(ssh) || firewallErrors([...rows]).some(hasRowErrors);
}

export function rulesPayload(rows: readonly FirewallRow[]): FirewallRulePayload[] {
  return firewallRules(rows);
}

export function sshPayload(form: SshSourcesForm): SshSources {
  return { allowlist_ids: [...form.allowlist_ids], source_ips: splitList(form.source_ips) };
}

/** Kurzfassung der offenen Ports für die Bestätigung: „22/tcp, 80/tcp, 443/udp“. */
export function openPortsSummary(rows: readonly FirewallRow[], sshPort: number): string {
  return [`${sshPort}/tcp`, ...rows.map((row) => `${row.port}/${protoSuffix(row.proto)}`)].join(', ');
}
