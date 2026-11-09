// Zusätze der Serverliste: Auswahl des OS-Filters (`os_names` der Liste), offene Web-Konsolen je Server
// und die Kennzeichen „offline“ und „tolerierter Ausfall“.

import { normaliseServers, type ServerItem, type ServerPage } from './servers';

export interface ServerListPage extends ServerPage {
  /** Betriebssysteme, die im Sichtbereich vorkommen (vor den Textfiltern). */
  osNames: string[];
}

export function normaliseServerList(raw: unknown): ServerListPage {
  const names = typeof raw === 'object' && raw !== null ? (raw as { os_names?: unknown }).os_names : undefined;
  return { ...normaliseServers(raw), osNames: Array.isArray(names) ? names.filter((n): n is string => typeof n === 'string' && n !== '') : [] };
}

/** Wer auf welchem Server gerade eine Web-Konsole offen hat (`GET /servers/consoles`). */
export function consolesByServer(raw: unknown): Map<number, string[]> {
  const byServer = new Map<number, string[]>();
  for (const entry of Array.isArray(raw) ? raw : []) {
    if (typeof entry !== 'object' || entry === null) continue;
    const { server_id: id, actor } = entry as { server_id?: unknown; actor?: unknown };
    if (typeof id !== 'number') continue;
    const actors = byServer.get(id) ?? [];
    const name = typeof actor === 'string' && actor !== '' ? actor : '?';
    if (!actors.includes(name)) actors.push(name);
    byServer.set(id, actors);
  }
  return byServer;
}

/** Ein einzelner Fehlschlag ist Alltag; offline heißt: nicht erreichbar und mindestens zwei Fehlversuche in Folge. */
export const OFFLINE_AFTER_FAILED_CHECKS = 2;

export function isOffline(server: Pick<ServerItem, 'reachable' | 'failed_checks'>): boolean {
  return !server.reachable && server.failed_checks >= OFFLINE_AFTER_FAILED_CHECKS;
}

/** Nicht erreichbar, die Ampel aber nicht rot: Der Ausfall wird toleriert, die Zeile tritt zurück. */
export function isToleratedOutage(server: Pick<ServerItem, 'reachable' | 'status'>): boolean {
  return !server.reachable && server.status !== 'red';
}

/** Host mit SSH-Port; Agent-Server haben keinen Port, sie melden sich selbst. */
export function hostLabel(server: Pick<ServerItem, 'host' | 'ssh_port' | 'transport'>): string {
  if (server.transport === 'agent' || !server.host) return server.host;
  const host = server.host.includes(':') ? `[${server.host}]` : server.host; // IPv6
  return `${host}:${server.ssh_port}`;
}
