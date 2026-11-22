import { describe, expect, it } from 'vitest';
import type { AcmeState, DnsProvider } from '../../../api/endpoints';
import { bodyOf, draftOf, isDirty, problemOf } from './acme-model';
import { dnsBodyOf, dnsChanged, dnsDraftOf, fieldKey, missingDnsField, parseNameservers, runsSlowly, secretStored, switchProvider } from './acme-dns-model';

const NAMES = ['lcm.example.com'];
const none = { required: false, options: [], default: '' };

const providers: DnsProvider[] = [
  { id: 'cloudflare', fields: [{ ...none, name: 'api_token', type: 'secret', required: true }, { ...none, name: 'propagation_timeout', type: 'number' }] },
  {
    id: 'rfc2136',
    fields: [
      { ...none, name: 'nameserver', type: 'text', required: true },
      { ...none, name: 'tsig_algorithm', type: 'select', options: ['hmac-sha256', 'hmac-sha512'], default: 'hmac-sha256' },
      { ...none, name: 'tsig_secret', type: 'secret' },
    ],
  },
  { id: 'inwx', fields: [{ ...none, name: 'username', type: 'text', required: true }, { ...none, name: 'sandbox', type: 'bool' }] },
];

function state(over: Partial<AcmeState> = {}): AcmeState {
  return {
    enabled: false,
    directoryUrl: 'https://acme-v02.api.letsencrypt.org/directory',
    email: '',
    names: [],
    mode: 'single',
    challenge: 'dns-01',
    termsAccepted: true,
    httpListen: ':80',
    namesChangedNotice: null,
    listenerRunning: false,
    listenerError: '',
    running: false,
    lastRunAt: null,
    lastError: '',
    certificates: [],
    dnsProvider: '',
    dnsValues: {},
    dnsSecretsSet: {},
    dnsCheckNameservers: [],
    dnsProviders: providers,
    ...over,
  };
}

const cloudflareSaved = state({ dnsProvider: 'cloudflare', dnsSecretsSet: { api_token: true }, dnsValues: { propagation_timeout: 90 } });

describe('Anbieterwerte im Entwurf', () => {
  it('übernimmt gespeicherte Werte als Text, Geheimnisse bleiben leer', () => {
    const dns = dnsDraftOf(state({ dnsProvider: 'inwx', dnsValues: { username: 'tony', sandbox: true, polling_interval: 5 }, dnsCheckNameservers: ['1.1.1.1', 'ns.example.com:53'] }));
    expect(dns.values).toEqual({ username: 'tony', sandbox: 'true', polling_interval: '5' });
    expect(dns.secrets).toEqual({});
    expect(dns.nameservers).toBe('1.1.1.1\nns.example.com:53');
  });

  it('liest Prüf-Nameserver je Zeile ohne Doppelte', () => {
    expect(parseNameservers('1.1.1.1\n 9.9.9.9, 1.1.1.1\n')).toEqual(['1.1.1.1', '9.9.9.9']);
  });

  it('verwirft beim Anbieterwechsel die Eingaben des alten Anbieters', () => {
    const typed = { ...dnsDraftOf(cloudflareSaved), values: { propagation_timeout: '30' }, secrets: { api_token: 'neu' }, nameservers: '1.1.1.1' };
    const other = switchProvider(typed, cloudflareSaved, 'rfc2136');
    expect(other).toEqual({ provider: 'rfc2136', values: {}, secrets: {}, nameservers: '1.1.1.1' });
  });

  it('stellt beim Zurückwechseln zum gespeicherten Anbieter dessen Werte wieder her', () => {
    const typed = { ...switchProvider(dnsDraftOf(cloudflareSaved), cloudflareSaved, 'rfc2136'), values: { nameserver: 'ns.example.com' } };
    const back = switchProvider(typed, cloudflareSaved, 'cloudflare');
    expect(back.provider).toBe('cloudflare');
    expect(back.values).toEqual({ propagation_timeout: '90' });
  });
});

describe('Geheimfelder', () => {
  it('gelten nur beim selben Anbieter als gespeichert', () => {
    const dns = dnsDraftOf(cloudflareSaved);
    expect(secretStored(dns, cloudflareSaved, 'api_token')).toBe(true);
    expect(secretStored({ ...dns, provider: 'rfc2136' }, cloudflareSaved, 'api_token')).toBe(false);
  });

  it('bleiben ohne Eingabe aus dem Rumpf, ein neuer Wert geht mit, ein leerer löscht', () => {
    const dns = dnsDraftOf(cloudflareSaved);
    expect(dnsBodyOf(dns, cloudflareSaved).dns_values).toEqual({ propagation_timeout: 90 });
    expect(dnsBodyOf({ ...dns, secrets: { api_token: ' neu ' } }, cloudflareSaved).dns_values).toEqual({ api_token: 'neu', propagation_timeout: 90 });
    expect(dnsBodyOf({ ...dns, secrets: { api_token: '' } }, cloudflareSaved).dns_values).toEqual({ api_token: '', propagation_timeout: 90 });
    expect(dnsBodyOf({ ...dns, secrets: { api_token: undefined } }, cloudflareSaved).dns_values).toEqual({ propagation_timeout: 90 });
  });

  it('zählen als Pflichtangabe, wenn gespeichert oder eingegeben, nicht nach dem Löschen', () => {
    const dns = dnsDraftOf(cloudflareSaved);
    expect(missingDnsField(dns, cloudflareSaved)).toBeNull();
    expect(missingDnsField({ ...dns, secrets: { api_token: '' } }, cloudflareSaved)).toBe('api_token');
    expect(missingDnsField({ ...dns, provider: 'cloudflare', values: {} }, state({ dnsProvider: 'cloudflare' }))).toBe('api_token');
    expect(missingDnsField({ ...dns, secrets: { api_token: 'x' } }, state({ dnsProvider: 'cloudflare' }))).toBeNull();
  });
});

describe('Rumpf der Anfrage', () => {
  it('schickt Zahlen als Zahl, Schalter als Wahrheitswert und lässt Leeres weg', () => {
    const saved = state();
    const dns = { ...switchProvider(dnsDraftOf(saved), saved, 'inwx'), values: { username: ' tony ', sandbox: 'true', unknown: 'x' } };
    expect(dnsBodyOf(dns, saved)).toEqual({ dns_provider: 'inwx', dns_values: { username: 'tony', sandbox: true }, dns_check_nameservers: [] });
    const timeout = { ...switchProvider(dnsDraftOf(saved), saved, 'cloudflare'), values: { propagation_timeout: '120' }, secrets: { api_token: 'abc' } };
    expect(dnsBodyOf(timeout, saved).dns_values).toEqual({ api_token: 'abc', propagation_timeout: 120 });
  });

  it('lässt eine nicht gewählte Auswahl leer, damit der Server seinen Standard nimmt', () => {
    const saved = state();
    const dns = { ...switchProvider(dnsDraftOf(saved), saved, 'rfc2136'), values: { nameserver: 'ns.example.com' } };
    expect(dnsBodyOf(dns, saved).dns_values).toEqual({ nameserver: 'ns.example.com' });
    expect(dnsBodyOf({ ...dns, values: { ...dns.values, tsig_algorithm: 'hmac-sha512' } }, saved).dns_values).toEqual({ nameserver: 'ns.example.com', tsig_algorithm: 'hmac-sha512' });
  });

  it('nimmt DNS-Angaben nur mit DNS-01 in den Rumpf', () => {
    const saved = state({ challenge: 'http-01' });
    const draft = draftOf(saved);
    expect(bodyOf(draft, saved, NAMES).challenge).toBe('http-01');
    expect(bodyOf(draft, saved, NAMES)).not.toHaveProperty('dns_provider');
    const dnsDraft = { ...draft, challenge: 'dns-01' as const, dns: { ...draft.dns, provider: 'cloudflare', secrets: { api_token: 'abc' } } };
    expect(bodyOf(dnsDraft, saved, NAMES)).toMatchObject({ challenge: 'dns-01', dns_provider: 'cloudflare', dns_values: { api_token: 'abc' } });
  });
});

describe('Änderungen und Pflichtangaben', () => {
  it('erkennt eine Änderung der Anbieterwerte nur bei DNS-01', () => {
    const draft = draftOf(cloudflareSaved);
    expect(isDirty(draft, cloudflareSaved)).toBe(false);
    expect(isDirty({ ...draft, dns: { ...draft.dns, secrets: { api_token: 'neu' } } }, cloudflareSaved)).toBe(true);
    expect(isDirty({ ...draft, dns: { ...draft.dns, secrets: { api_token: undefined } } }, cloudflareSaved)).toBe(false);
    expect(isDirty({ ...draft, challenge: 'http-01' }, cloudflareSaved)).toBe(true);
    const http = state({ challenge: 'http-01' });
    expect(isDirty({ ...draftOf(http), dns: { ...dnsDraftOf(http), nameservers: '1.1.1.1' } }, http)).toBe(false);
    expect(dnsChanged({ ...dnsDraftOf(cloudflareSaved), nameservers: '1.1.1.1' }, cloudflareSaved)).toBe(true);
  });

  it('verlangt beim Einschalten von DNS-01 Anbieter und Pflichtfelder, nicht beim Speichern im ausgeschalteten Zustand', () => {
    const saved = state();
    const base = draftOf(saved);
    expect(problemOf(base, saved, NAMES)).toBeNull();
    expect(problemOf({ ...base, enabled: true }, saved, NAMES)).toBe('providerRequired');
    const chosen = { ...base, enabled: true, dns: switchProvider(base.dns, saved, 'cloudflare') };
    expect(problemOf(chosen, saved, NAMES)).toBe('fieldMissing');
    expect(problemOf({ ...chosen, dns: { ...chosen.dns, secrets: { api_token: 'abc' } } }, saved, NAMES)).toBeNull();
  });

  it('lehnt mehr als drei Prüf-Nameserver ab', () => {
    const saved = state();
    const draft = { ...draftOf(saved), dns: { ...dnsDraftOf(saved), nameservers: '1.1.1.1\n8.8.8.8\n9.9.9.9\n4.4.4.4' } };
    expect(problemOf(draft, saved, NAMES)).toBe('nameserversTooMany');
    expect(problemOf({ ...draft, challenge: 'http-01' }, saved, NAMES)).toBeNull();
  });
});

describe('Beschriftungen', () => {
  it('nutzt für gemeinsame Felder einen Schlüssel, sonst einen je Anbieter', () => {
    expect(fieldKey('cloudflare', 'propagation_timeout')).toBe('settings.acme.dns.field.common.propagation_timeout');
    expect(fieldKey('ionos', 'api_key')).toBe('settings.acme.dns.field.ionos.api_key');
  });

  it('meldet langsame Läufe bei mehreren Namen mit RFC2136 oder Skript', () => {
    expect(runsSlowly('rfc2136', 2)).toBe(true);
    expect(runsSlowly('exec', 2)).toBe(true);
    expect(runsSlowly('rfc2136', 1)).toBe(false);
    expect(runsSlowly('cloudflare', 3)).toBe(false);
  });
});
