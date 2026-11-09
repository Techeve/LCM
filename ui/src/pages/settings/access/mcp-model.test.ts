import { describe, expect, it } from 'vitest';
import { clientConfig, curlExample, isLoopbackHost, mcpUrl } from './mcp-model';

describe('MCP-Bind-Adresse', () => {
  it('lässt nur lokale Adressen zu, wie der Server', () => {
    for (const host of ['127.0.0.1', '127.1.2.3', 'localhost', 'LOCALHOST', '::1', '[::1]', ' 127.0.0.1 ']) expect(isLoopbackHost(host)).toBe(true);
    for (const host of ['0.0.0.0', '192.168.1.5', '128.0.0.1', '127.0.0.256', 'lcm.example.com', '::', '']) expect(isLoopbackHost(host)).toBe(false);
  });
});

describe('Anbindungsbeispiel', () => {
  it('baut die Adresse mit eckigen Klammern für IPv6 und Vorgaben für leere Felder', () => {
    expect(mcpUrl('127.0.0.1', '9330')).toBe('http://127.0.0.1:9330/mcp');
    expect(mcpUrl('::1', '9400')).toBe('http://[::1]:9400/mcp');
    expect(mcpUrl('[::1]', '9400')).toBe('http://[::1]:9400/mcp');
    expect(mcpUrl('', '')).toBe('http://127.0.0.1:9330/mcp');
  });

  it('setzt Adresse und Platzhalter in Konfiguration und curl-Aufruf', () => {
    const config = JSON.parse(clientConfig('http://127.0.0.1:9330/mcp', 'KEY'));
    expect(config.mcpServers.lcm).toEqual({ type: 'http', url: 'http://127.0.0.1:9330/mcp', headers: { Authorization: 'Bearer KEY' } });
    const curl = curlExample('http://127.0.0.1:9330/mcp', 'KEY');
    expect(curl).toContain('curl -s http://127.0.0.1:9330/mcp');
    expect(curl).toContain('Bearer KEY');
    expect(curl).toContain('"name":"list_servers"');
  });
});
