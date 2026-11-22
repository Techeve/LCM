import { describe, expect, it } from 'vitest';
import { readAcmeState, readNetworkState, readTlsStatus } from './endpoints';

describe('Zertifikatsstand', () => {
  it('liest Zertifikate, Endpunkte und den gemeinsamen Port', () => {
    const status = readTlsStatus({
      certificates: [
        { id: 'upload-ab12cd34', source: 'upload', names: ['lcm.example.com'], issuer: 'R11', not_after: '2027-02-01T00:00:00Z', expired: false, fingerprint: 'ab', warnings: ['kette unvollständig: x'] },
        { id: 'lcm-ca', source: 'lcm-ca', names: ['localhost'], issuer: 'LCM CA', not_after: '2027-03-01T00:00:00Z', expired: false, fingerprint: 'cd' },
      ],
      endpoints: [
        { name: 'ui', url: 'https://lcm.example.com', host: 'lcm.example.com', cert: { id: 'upload-ab12cd34', source: 'upload', names: [], issuer: '', not_after: '', fingerprint: '' } },
        { name: 'agent' },
        { name: 'unbekannt', url: 'x' },
      ],
      shared_port: { address: ':443', listening: true, routes: [{ service: 'ui', host: 'lcm.example.com' }, { service: 'agent', host: '', shadowed_by: 'ui' }, { service: 'x' }] },
    });
    expect(status.certificates[0]).toMatchObject({ id: 'upload-ab12cd34', source: 'upload', warnings: ['kette unvollständig: x'] });
    expect(status.certificates[1]).toMatchObject({ source: 'lcm-ca', warnings: [] });
    expect(status.endpoints.map((e) => e.name)).toEqual(['ui', 'agent']);
    expect(status.endpoints[0]?.cert?.id).toBe('upload-ab12cd34');
    expect(status.endpoints[1]).toMatchObject({ url: '', host: '', cert: null });
    expect(status.sharedPort).toMatchObject({ address: ':443', listening: true });
    expect(status.sharedPort?.routes).toHaveLength(2);
    expect(status.sharedPort?.routes[1]?.shadowedBy).toBe('ui');
  });

  it('hält leere und kaputte Antworten aus', () => {
    expect(readTlsStatus(null)).toEqual({ certificates: [], endpoints: [], sharedPort: null });
    expect(readTlsStatus({ certificates: 'x', endpoints: [1] }).certificates).toEqual([]);
    expect(readTlsStatus({ certificates: [{ source: 'seltsam' }] }).certificates[0]?.source).toBe('lcm-ca');
  });
});

describe('Netzbereiche', () => {
  it('liest je Endpunkt die Liste samt eigener Adresse', () => {
    const state = readNetworkState({
      client_address: '10.1.2.3',
      endpoints: [
        { endpoint: 'ui', allowed: [] },
        { endpoint: 'mcp', allowed: ['127.0.0.1/32'] },
        { endpoint: 'nope', allowed: [] },
      ],
    });
    expect(state.clientAddress).toBe('10.1.2.3');
    expect(state.endpoints).toEqual([
      { endpoint: 'ui', allowed: [] },
      { endpoint: 'mcp', allowed: ['127.0.0.1/32'] },
    ]);
  });
});

describe('ACME-Zustand', () => {
  it('liest Einstellungen, Listener und Zertifikate mit den Vorgaben für Fehlendes', () => {
    const state = readAcmeState({
      enabled: true,
      directory_url: 'https://acme-v02.api.letsencrypt.org/directory',
      names: ['lcm.example.com'],
      mode: 'per_name',
      terms_accepted: true,
      http_listen: ':80',
      listener_running: false,
      listener_error: 'bind: permission denied',
      last_run_at: '2026-11-21T04:00:00Z',
      certificates: [{ id: 'a', names: ['lcm.example.com'], issuer: 'R11', not_after: '2027-02-01T00:00:00Z', renew_at: '2027-01-01T00:00:00Z', fingerprint: 'ab' }],
    });
    expect(state).toMatchObject({ enabled: true, mode: 'per_name', challenge: 'http-01', termsAccepted: true, listenerError: 'bind: permission denied', lastRunAt: '2026-11-21T04:00:00Z', lastError: '' });
    expect(state.certificates[0]).toMatchObject({ renewAt: '2027-01-01T00:00:00Z' });
    expect(readAcmeState(null)).toMatchObject({ enabled: false, mode: 'single', lastRunAt: null, certificates: [] });
  });

  it('liest den Hinweis zum Umstieg auf die Namen aus den Adressen, null ohne ihn', () => {
    const state = readAcmeState({ names_changed_notice: { old: ['alt.example.com'], new: ['lcm.example.com', 'agent.example.com'], was_per_name: true } });
    expect(state.namesChangedNotice).toEqual({ old: ['alt.example.com'], new: ['lcm.example.com', 'agent.example.com'], wasPerName: true });
    expect(readAcmeState({ names_changed_notice: null }).namesChangedNotice).toBeNull();
    expect(readAcmeState({}).namesChangedNotice).toBeNull();
  });

  it('liest die Angaben zu DNS-01: Werte mit ihren Typen, gesetzte Geheimnisse und die Felder je Anbieter', () => {
    const state = readAcmeState({
      challenge: 'dns-01',
      dns_provider: 'inwx',
      dns_values: { username: 'tony', sandbox: true, polling_interval: 5, kaputt: { x: 1 } },
      dns_secrets_set: { password: true, totp_secret: false },
      dns_check_nameservers: ['1.1.1.1'],
      dns_providers: [{ id: 'inwx', fields: [{ name: 'username', type: 'text', required: true }, { name: 'mode', type: 'select', options: ['standard', 'raw'], default: 'standard' }, { name: 'x', type: 'neu' }] }],
    });
    expect(state).toMatchObject({ challenge: 'dns-01', dnsProvider: 'inwx', dnsCheckNameservers: ['1.1.1.1'], dnsSecretsSet: { password: true, totp_secret: false } });
    expect(state.dnsValues).toEqual({ username: 'tony', sandbox: true, polling_interval: 5 });
    expect(state.dnsProviders[0]?.fields).toEqual([
      { name: 'username', type: 'text', required: true, options: [], default: '' },
      { name: 'mode', type: 'select', required: false, options: ['standard', 'raw'], default: 'standard' },
      { name: 'x', type: 'text', required: false, options: [], default: '' },
    ]);
    expect(readAcmeState({ challenge: 'unbekannt' }).challenge).toBe('http-01');
  });
});
