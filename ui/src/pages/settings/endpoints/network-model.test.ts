import { describe, expect, it } from 'vitest';
import { ApiError } from '../../../lib/api';
import { MAX_NETWORK_ENTRIES, formatNetworks, isNetworkEntry, networkFailure, networkProblem, parseNetworks, sameNetworks } from './network-model';

describe('Netzliste', () => {
  it('trennt nach Zeilen, Kommas und Leerzeichen und entfernt Doppelte', () => {
    expect(parseNetworks('10.0.0.0/8\n 192.168.1.5, 10.0.0.0/8  fe80::/10\n\n')).toEqual(['10.0.0.0/8', '192.168.1.5', 'fe80::/10']);
    expect(parseNetworks('')).toEqual([]);
    expect(formatNetworks(['10.0.0.0/8', 'private'])).toBe('10.0.0.0/8\nprivate');
  });

  it('erkennt Adressen, Netze und die Schlüsselwörter wie der Server', () => {
    for (const entry of ['10.0.0.1', '10.0.0.0/8', '10.0.0.5/24', '::1', '2001:db8::/32', 'localhost', 'PRIVATE', 'loopback']) expect(isNetworkEntry(entry)).toBe(true);
    for (const entry of ['10.0.0.0/33', '2001:db8::/129', '10.0.0.1/', '10.0.0.1/8/8', 'example.com', '300.1.1.1', '10.0.0.0/08', 'all']) expect(isNetworkEntry(entry)).toBe(false);
  });

  it('nennt das Problem der Eingabe', () => {
    expect(networkProblem(['10.0.0.0/8', 'private'])).toBeNull();
    expect(networkProblem(['10.0.0.0/8', 'kaputt'])).toEqual({ kind: 'invalid', entries: ['kaputt'] });
    const many = Array.from({ length: MAX_NETWORK_ENTRIES + 1 }, (_, i) => `10.0.${Math.floor(i / 256)}.${i % 256}`);
    expect(networkProblem(many)).toEqual({ kind: 'tooMany' });
    expect(networkProblem(many.slice(0, MAX_NETWORK_ENTRIES))).toBeNull();
  });

  it('vergleicht Listen in ihrer Reihenfolge', () => {
    expect(sameNetworks(['a', 'b'], ['a', 'b'])).toBe(true);
    expect(sameNetworks(['a', 'b'], ['b', 'a'])).toBe(false);
    expect(sameNetworks([], ['a'])).toBe(false);
  });
});

describe('Fehler beim Speichern der Netzbereiche', () => {
  it('erkennt das Aussperren und nennt die eigene Adresse', () => {
    const error = new ApiError(409, 'endpoint.would_lock_out', { address: '10.1.2.3' }, 'x');
    expect(networkFailure(error)).toEqual({ kind: 'lockout', address: '10.1.2.3' });
  });

  it('liest die betroffenen Agenten für die Rückfrage', () => {
    const agents = [{ id: 'a1', name: 'web01', address: '203.0.113.9' }, { id: 'a2', name: 'db01' }];
    const error = new ApiError(409, 'endpoint.agents_outside', { count: 2, agents }, 'x');
    expect(networkFailure(error)).toEqual({
      kind: 'agentsOutside',
      agents: [
        { id: 'a1', name: 'web01', address: '203.0.113.9' },
        { id: 'a2', name: 'db01', address: '' },
      ],
    });
  });

  it('behandelt alles andere als sonstigen Fehler', () => {
    expect(networkFailure(new ApiError(409, 'endpoint.networks_locked', { config_key: 'allowed_ips' }, 'x'))).toEqual({ kind: 'other' });
    expect(networkFailure(new ApiError(422, 'endpoint.invalid_networks', { detail: 'x' }, 'x'))).toEqual({ kind: 'other' });
    expect(networkFailure(new Error('x'))).toEqual({ kind: 'other' });
  });
});
