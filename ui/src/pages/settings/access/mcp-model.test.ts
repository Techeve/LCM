import { describe, expect, it } from 'vitest';
import { readGlobal } from '../../../api/settings';
import { clientConfig, curlExample, mcpNeedsDomain, mcpUrl } from './mcp-model';

describe('MCP braucht eine Domain', () => {
  it('nur in Modus C ohne Namen für MCP', () => {
    expect(mcpNeedsDomain(readGlobal({ address_mode: 'shared_port', public_mcp_url: '' }))).toBe(true);
    expect(mcpNeedsDomain(readGlobal({ address_mode: 'shared_port' }))).toBe(true);
    expect(mcpNeedsDomain(readGlobal({ address_mode: 'shared_port', public_mcp_url: 'https://mcp.example.com' }))).toBe(false);
    expect(mcpNeedsDomain(readGlobal({ address_mode: 'separate', public_mcp_url: '' }))).toBe(false);
    expect(mcpNeedsDomain(readGlobal({ address_mode: 'one_name' }))).toBe(false);
  });
});

describe('Anbindungsbeispiel', () => {
  it('hängt an die wirksame Adresse den Pfad /mcp', () => {
    expect(mcpUrl('https://mcp.example.com', '9330')).toBe('https://mcp.example.com/mcp');
    expect(mcpUrl('https://lcm.example.com:9330/', '9330')).toBe('https://lcm.example.com:9330/mcp');
  });

  it('fällt ohne wirksame Adresse auf den LCM-Host zurück, immer mit https', () => {
    expect(mcpUrl('', '9400')).toBe('https://127.0.0.1:9400/mcp');
    expect(mcpUrl('', '')).toBe('https://127.0.0.1:9330/mcp');
  });

  it('setzt Adresse und Platzhalter in Konfiguration und curl-Aufruf', () => {
    const config = JSON.parse(clientConfig('https://127.0.0.1:9330/mcp', 'KEY'));
    expect(config.mcpServers.lcm).toEqual({ type: 'http', url: 'https://127.0.0.1:9330/mcp', headers: { Authorization: 'Bearer KEY' } });
    const curl = curlExample('https://127.0.0.1:9330/mcp', 'KEY');
    expect(curl).toContain('curl -s https://127.0.0.1:9330/mcp');
    expect(curl).toContain('Bearer KEY');
    expect(curl).toContain('"name":"list_servers"');
  });
});
