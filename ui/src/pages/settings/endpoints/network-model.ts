// Erlaubte Netzbereiche eines Endpunkts: Eingabe, Prüfung und die Fehler, die der Server melden kann.

import type { AgentPeer } from '../../../api/endpoints';
import { ApiError } from '../../../lib/api';
import { isIP, isIPv4, splitList } from '../../../lib/validate';

/** Obergrenze des Servers (netfilter.MaxNetworkEntries). */
export const MAX_NETWORK_ENTRIES = 64;

const KEYWORDS = new Set(['localhost', 'loopback', 'private']);

/** Einträge aus dem Textfeld: getrennt durch Zeilen, Kommas oder Leerzeichen, ohne Doppelte. */
export function parseNetworks(input: string): string[] {
  return [...new Set(splitList(input))];
}

export function formatNetworks(list: string[]): string {
  return list.join('\n');
}

/** Eine Adresse oder ein Netz (CIDR); Host-Bits im Netz fallen beim Server weg. */
export function isAddressOrCidr(entry: string): boolean {
  const [address, prefix, ...rest] = entry.split('/');
  if (rest.length > 0 || !isIP(address!)) return false;
  if (prefix === undefined) return true;
  const width = isIPv4(address!) ? 32 : 128;
  return /^(0|[1-9]\d{0,2})$/.test(prefix) && Number(prefix) <= width;
}

/** Wie der Server: ein Schlüsselwort, eine Adresse oder ein Netz. */
export function isNetworkEntry(entry: string): boolean {
  return KEYWORDS.has(entry.toLowerCase()) || isAddressOrCidr(entry);
}

export type NetworkProblem = { kind: 'invalid'; entries: string[] } | { kind: 'tooMany' };

/** Das Problem der Eingabe, sonst `null`. */
export function networkProblem(entries: string[]): NetworkProblem | null {
  const invalid = entries.filter((entry) => !isNetworkEntry(entry));
  if (invalid.length > 0) return { kind: 'invalid', entries: invalid };
  return entries.length > MAX_NETWORK_ENTRIES ? { kind: 'tooMany' } : null;
}

export function sameNetworks(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((entry, index) => entry === b[index]);
}

/** Wie sich ein abgelehntes Speichern der Netzbereiche zeigt. */
export type NetworkFailure =
  | { kind: 'lockout'; address: string }
  | { kind: 'agentsOutside'; agents: AgentPeer[] }
  | { kind: 'other' };

function peersOf(value: unknown): AgentPeer[] {
  if (!Array.isArray(value)) return [];
  return value.map((item) => {
    const peer = (typeof item === 'object' && item !== null ? item : {}) as Record<string, unknown>;
    return { id: String(peer.id ?? ''), name: String(peer.name ?? ''), address: typeof peer.address === 'string' ? peer.address : '' };
  });
}

export function networkFailure(error: unknown): NetworkFailure {
  if (!(error instanceof ApiError)) return { kind: 'other' };
  if (error.code === 'endpoint.would_lock_out') return { kind: 'lockout', address: String(error.params.address ?? '') };
  if (error.code === 'endpoint.agents_outside') return { kind: 'agentsOutside', agents: peersOf(error.params.agents) };
  return { kind: 'other' };
}
