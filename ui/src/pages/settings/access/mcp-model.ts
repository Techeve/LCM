// MCP-Schnittstelle: Adresse des Endpunkts, Anbindungsbeispiel und die Frage, ob MCP eine Domain braucht.

import type { GlobalSettings } from '../../../api/settings';
import { MODE_KEY, URL_KEY, hostOf, readMode } from '../addresses/address-model';

const DEFAULT_PORT = '9330';

/** In Modus C ohne Namen für MCP gibt es keine Adresse, und der Schalter bleibt gesperrt (der Dienst ist aus). */
export function mcpNeedsDomain(settings: GlobalSettings): boolean {
  return readMode(settings[MODE_KEY]) === 'shared_port' && hostOf(String(settings[URL_KEY.mcp] ?? '')) === '';
}

/**
 * Adresse des MCP-Endpunkts: die wirksame MCP-Adresse von LCM (`effective_urls.mcp`) mit dem Pfad `/mcp`. Fehlt sie,
 * gilt der Aufruf auf dem LCM-Host. MCP spricht immer TLS.
 */
export function mcpUrl(effective: string, port: string): string {
  const base = effective.trim().replace(/\/+$/, '');
  return `${base || `https://127.0.0.1:${port.trim() || DEFAULT_PORT}`}/mcp`;
}

/** Konfiguration für Clients wie Claude Code oder Cursor (`mcpServers`). */
export function clientConfig(url: string, keyPlaceholder: string): string {
  return JSON.stringify({ mcpServers: { lcm: { type: 'http', url, headers: { Authorization: `Bearer ${keyPlaceholder}` } } } }, null, 2);
}

/** Ein Probeaufruf mit curl, der die Server auflistet. */
export function curlExample(url: string, keyPlaceholder: string): string {
  const body = JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'list_servers', arguments: {} } });
  return [`curl -s ${url} \\`, `  -H "Authorization: Bearer ${keyPlaceholder}" \\`, `  -H "Content-Type: application/json" \\`, `  -d '${body}'`].join('\n');
}
