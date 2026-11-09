// MCP-Schnittstelle: Prüfung der Bind-Adresse und das Anbindungsbeispiel.

/** Wie der Server: nur `localhost` oder eine Loopback-Adresse (127.0.0.0/8, ::1), gern in eckigen Klammern. */
export function isLoopbackHost(host: string): boolean {
  const bare = host.trim().replace(/^\[|\]$/g, '').trim();
  if (bare.toLowerCase() === 'localhost') return true;
  if (bare === '::1' || /^0*:(0*:){0,6}0*1$/.test(bare) || bare === '0:0:0:0:0:0:0:1') return true;
  const parts = bare.split('.');
  return parts.length === 4 && parts[0] === '127' && parts.every((part) => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

/** Adresse des MCP-Endpunkts, wie ein Client auf dem LCM-Host sie aufruft. IPv6 steht in eckigen Klammern. */
export function mcpUrl(host: string, port: string): string {
  const bare = host.trim().replace(/^\[|\]$/g, '') || '127.0.0.1';
  const shown = bare.includes(':') ? `[${bare}]` : bare;
  return `http://${shown}:${port.trim() || '9330'}/mcp`;
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
