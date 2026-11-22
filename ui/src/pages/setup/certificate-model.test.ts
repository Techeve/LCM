import { describe, expect, it } from 'vitest';
import { readGlobal } from '../../api/settings';
import { certificateBody, certificateChoice, certificateRedirect, proxyBody, proxyChanged, proxyDraftOf, proxyDraftProblem, proxyEntries, proxyProblem, proxyProblemText } from './certificate-model';

describe('certificateChoice', () => {
  it('starts with Let\'s Encrypt, or with the proxy when LCM already trusts one', () => {
    expect(certificateChoice({})).toBe('letsencrypt');
    expect(certificateChoice({ trust_proxy_header: false })).toBe('letsencrypt');
    expect(certificateChoice({ trust_proxy_header: true })).toBe('proxy');
  });
});

describe('proxyEntries', () => {
  it('takes lines and commas and drops duplicates', () => {
    expect(proxyEntries('10.0.0.1, 10.0.0.2\n10.0.0.1\n\n192.168.0.0/24')).toEqual(['10.0.0.1', '10.0.0.2', '192.168.0.0/24']);
  });

  it('comes back from the API as one entry per line', () => {
    const settings = readGlobal({ trusted_proxies: ['10.0.0.1', '2001:db8::/32'], trust_proxy_header: true });
    expect(settings.trusted_proxies).toBe('10.0.0.1\n2001:db8::/32');
    expect(proxyEntries(String(settings.trusted_proxies))).toEqual(['10.0.0.1', '2001:db8::/32']);
  });
});

describe('proxyProblem', () => {
  it('accepts addresses and networks', () => {
    expect(proxyProblem(['10.0.0.1', '10.0.0.0/8', '::1', 'fd00::/8'])).toBeNull();
  });

  it('rejects an empty list, because the switch needs a proxy', () => {
    expect(proxyProblem([])).toEqual({ kind: 'empty' });
  });

  it('rejects entries that are no address or network, also the keywords of the access lists', () => {
    expect(proxyProblem(['10.0.0.1', 'proxy.example.com', 'private', '10.0.0.0/33'])).toEqual({ kind: 'invalid', entries: ['proxy.example.com', 'private', '10.0.0.0/33'] });
  });

  it('rejects networks that cover every address', () => {
    expect(proxyProblem(['0.0.0.0/0'])).toEqual({ kind: 'wide', entries: ['0.0.0.0/0'] });
    expect(proxyProblem(['10.0.0.1', '::/0'])).toEqual({ kind: 'wide', entries: ['::/0'] });
  });
});

describe('certificateBody', () => {
  it('turns on the header and sends the list for a proxy', () => {
    expect(certificateBody('proxy', ['10.0.0.1'])).toEqual({ trust_proxy_header: true, trusted_proxies: ['10.0.0.1'] });
  });

  it('sends nothing for Let\'s Encrypt, the card saves itself', () => {
    expect(certificateBody('letsencrypt', ['10.0.0.1'])).toEqual({});
  });
});

describe('certificateRedirect', () => {
  const here = 'https://192.168.1.5:9310/setup';
  const named = { public_base_url: 'https://lcm.example.org' };

  it('leads to the wizard on the new address of the interface when it carries a configured name', () => {
    expect(certificateRedirect(readGlobal({ ...named, effective_urls: { ui: 'https://lcm.example.org' } }), here)).toBe('https://lcm.example.org/setup');
  });

  it('takes the name from public_host in mode B as well', () => {
    expect(certificateRedirect(readGlobal({ public_host: 'lcm.example.org', effective_urls: { ui: 'https://lcm.example.org' } }), here)).toBe('https://lcm.example.org/setup');
  });

  it('prefers ui_url from the answer', () => {
    expect(certificateRedirect(readGlobal({ ...named, ui_url: 'https://lcm.example.org:9400', effective_urls: { ui: 'https://other.example.org' } }), here)).toBe('https://lcm.example.org:9400/setup');
  });

  it('stays without a configured name, even when the address differs from the current one', () => {
    expect(certificateRedirect(readGlobal({ effective_urls: { ui: 'https://lcm.example.org' } }), here)).toBeNull();
    expect(certificateRedirect(readGlobal({ public_base_url: '  ', public_host: '', effective_urls: { ui: 'https://lcm.example.org' } }), here)).toBeNull();
  });

  it('stays when the address is localhost or an IP address, the fallback', () => {
    expect(certificateRedirect(readGlobal({ ...named, effective_urls: { ui: 'https://localhost:9310' } }), here)).toBeNull();
    expect(certificateRedirect(readGlobal({ ...named, effective_urls: { ui: 'https://10.0.0.7:9310' } }), here)).toBeNull();
    expect(certificateRedirect(readGlobal({ ...named, effective_urls: { ui: 'https://[::1]:9310' } }), here)).toBeNull();
  });

  it('stays when the page already runs there', () => {
    expect(certificateRedirect(readGlobal({ ...named, effective_urls: { ui: 'https://lcm.example.org' } }), 'https://lcm.example.org/setup')).toBeNull();
    expect(certificateRedirect(readGlobal({ ...named, effective_urls: { ui: 'https://lcm.example.org/some/path' } }), 'https://lcm.example.org:443/setup')).toBeNull();
  });

  it('stays without an address', () => {
    expect(certificateRedirect({}, here)).toBeNull();
    expect(certificateRedirect({ ...named, 'effective_urls.ui': 'not a url' }, here)).toBeNull();
  });
});

describe('Reverse-Proxy in den Einstellungen', () => {
  const saved = readGlobal({ trust_proxy_header: true, trusted_proxies: ['10.0.0.1'] });

  it('liest Schalter und Liste aus den gespeicherten Einstellungen', () => {
    expect(proxyDraftOf(saved)).toEqual({ trust: true, entries: '10.0.0.1' });
    expect(proxyDraftOf(readGlobal({}))).toEqual({ trust: false, entries: '' });
  });

  it('erkennt Änderungen; bei ausgeschaltetem Schalter zählt die Liste nicht', () => {
    expect(proxyChanged(proxyDraftOf(saved), saved)).toBe(false);
    expect(proxyChanged({ trust: true, entries: '10.0.0.1, 10.0.0.2' }, saved)).toBe(true);
    expect(proxyChanged({ trust: true, entries: '10.0.0.1\n10.0.0.1' }, saved)).toBe(false);
    expect(proxyChanged({ trust: false, entries: '10.0.0.1' }, saved)).toBe(true);
    expect(proxyChanged({ trust: false, entries: 'egal' }, readGlobal({}))).toBe(false);
  });

  it('prüft die Liste wie der Assistent, nur bei eingeschaltetem Schalter', () => {
    expect(proxyDraftProblem({ trust: true, entries: '' })).toEqual({ kind: 'empty' });
    expect(proxyDraftProblem({ trust: true, entries: '0.0.0.0/0' })?.kind).toBe('wide');
    expect(proxyDraftProblem({ trust: true, entries: 'proxy.example.com' })?.kind).toBe('invalid');
    expect(proxyDraftProblem({ trust: true, entries: '10.0.0.1' })).toBeNull();
    expect(proxyDraftProblem({ trust: false, entries: '' })).toBeNull();
  });

  it('schickt aus nur den Schalter, an mit der Liste', () => {
    expect(proxyBody({ trust: false, entries: '10.0.0.1' })).toEqual({ trust_proxy_header: false });
    expect(proxyBody({ trust: true, entries: '10.0.0.1\n192.168.0.0/24' })).toEqual({ trust_proxy_header: true, trusted_proxies: ['10.0.0.1', '192.168.0.0/24'] });
  });

  it('nennt die Einträge im Fehlertext, höchstens drei', () => {
    expect(proxyProblemText({ kind: 'empty' })).toEqual({ key: 'setup.certificate.proxy.empty', list: '' });
    expect(proxyProblemText({ kind: 'invalid', entries: ['a', 'b', 'c', 'd'] })).toEqual({ key: 'setup.certificate.proxy.invalid', list: 'a, b, c …' });
  });
});
