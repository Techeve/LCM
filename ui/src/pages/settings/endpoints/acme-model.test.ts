import { describe, expect, it } from 'vitest';
import type { AcmeState } from '../../../api/endpoints';
import { LETS_ENCRYPT, LETS_ENCRYPT_STAGING, LETS_ENCRYPT_TERMS, bodyOf, caOf, chainFileName, coversNames, directoryHost, directoryOf, draftOf, isDirty, listenerNeedsCapability, needsTerms, problemOf, statusOf, termsUrl } from './acme-model';

const NAMES = ['lcm.example.com'];

function state(over: Partial<AcmeState> = {}): AcmeState {
  return {
    enabled: false,
    directoryUrl: LETS_ENCRYPT,
    email: '',
    names: [],
    mode: 'single',
    challenge: 'http-01',
    termsAccepted: false,
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
    dnsProviders: [],
    ...over,
  };
}

describe('Zertifizierungsstelle', () => {
  it('ordnet die Verzeichnisadresse zu', () => {
    expect(caOf(LETS_ENCRYPT)).toBe('letsencrypt');
    expect(caOf('')).toBe('letsencrypt');
    expect(caOf(LETS_ENCRYPT_STAGING)).toBe('staging');
    expect(caOf('https://ca.example.com/dir')).toBe('custom');
    expect(directoryOf({ ca: 'staging', customUrl: '' })).toBe(LETS_ENCRYPT_STAGING);
    expect(directoryOf({ ca: 'custom', customUrl: ' https://ca.example.com/dir ' })).toBe('https://ca.example.com/dir');
    expect(directoryHost(LETS_ENCRYPT)).toBe('acme-v02.api.letsencrypt.org');
  });
});

describe('Entwurf', () => {
  it('erkennt Änderungen; die Namen gehören nicht zum Entwurf', () => {
    const saved = state({ enabled: true, names: ['lcm.example.com'], email: 'a@example.com' });
    const draft = draftOf(saved);
    expect(draft).not.toHaveProperty('names');
    expect(draft).not.toHaveProperty('mode');
    expect(isDirty(draft, saved)).toBe(false);
    expect(isDirty({ ...draft, email: ' a@example.com ' }, saved)).toBe(false);
    expect(isDirty({ ...draft, ca: 'staging' }, saved)).toBe(true);
    expect(isDirty({ ...draft, enabled: false }, saved)).toBe(true);
  });

  it('schickt die Namen der Adressen mit, immer als ein Zertifikat für alle', () => {
    const draft = { ...draftOf(state()), enabled: true, acceptTerms: true };
    const names = ['lcm.example.com', 'agent.example.com'];
    expect(bodyOf(draft, state(), names)).toEqual({
      enabled: true,
      directory_url: LETS_ENCRYPT,
      email: '',
      names,
      mode: 'single',
      challenge: 'http-01',
      accept_terms: true,
    });
    expect(bodyOf(draft, state({ mode: 'per_name' }), names).mode).toBe('single');
  });
});

describe('Nutzungsbedingungen', () => {
  it('verlangen die Zustimmung beim Einschalten, bis sie für diese CA erteilt ist', () => {
    const accepted = state({ termsAccepted: true });
    const draft = { ...draftOf(accepted), enabled: true };
    expect(needsTerms(draft, accepted)).toBe(false);
    expect(needsTerms({ ...draft, ca: 'staging' }, accepted)).toBe(true);
    expect(needsTerms(draft, state())).toBe(true);
    expect(needsTerms({ ...draft, enabled: false }, state())).toBe(false);
  });
});

describe('Prüfung vor dem Speichern', () => {
  const accepted = state({ termsAccepted: true });
  const base = { ...draftOf(accepted), enabled: true };

  it('nennt das erste Problem wie der Server', () => {
    expect(problemOf(base, accepted, NAMES)).toBeNull();
    expect(problemOf(base, accepted, [])).toBe('namesRequired');
    expect(problemOf({ ...base, ca: 'custom', customUrl: ' ' }, accepted, NAMES)).toBe('directoryRequired');
    expect(problemOf(base, state(), NAMES)).toBe('termsRequired');
    expect(problemOf({ ...base, acceptTerms: true }, state(), NAMES)).toBeNull();
  });

  it('lässt ein Ausschalten ohne Namen zu', () => {
    expect(problemOf({ ...base, enabled: false }, state(), [])).toBeNull();
  });
});

describe('Dateiname der Zertifikatskette', () => {
  it('nimmt den ersten Namen und räumt unpassende Zeichen auf', () => {
    expect(chainFileName(['lcm.example.com', 'agent.example.com'])).toBe('lcm.example.com-chain.pem');
    expect(chainFileName(['*.example.com'])).toBe('wildcard.example.com-chain.pem');
    expect(chainFileName(['a/b c.example.com'])).toBe('a_b_c.example.com-chain.pem');
    expect(chainFileName([])).toBe('certificate-chain.pem');
  });
});

describe('Zustand', () => {
  it('zeigt einen Status je Zustand', () => {
    expect(statusOf(state()).level).toBe('unknown');
    expect(statusOf(state({ enabled: true, running: true })).level).toBe('running');
    expect(statusOf(state({ enabled: true, lastError: 'x' })).level).toBe('crit');
    expect(statusOf(state({ enabled: true })).level).toBe('warn');
    const cert = { id: 'a', names: ['x'], issuer: 'i', notAfter: '', fingerprint: '', renewAt: '' };
    expect(statusOf(state({ enabled: true, certificates: [cert] })).level).toBe('great');
  });

  it('weist bei einem Bindefehler unter Port 1024 auf die Capability hin', () => {
    expect(listenerNeedsCapability(state({ enabled: true, listenerError: 'permission denied', httpListen: ':80' }))).toBe(true);
    expect(listenerNeedsCapability(state({ enabled: true, listenerError: 'in use', httpListen: ':8080' }))).toBe(false);
    expect(listenerNeedsCapability(state({ enabled: true, httpListen: ':80' }))).toBe(false);
    expect(listenerNeedsCapability(state({ listenerError: 'x', httpListen: ':80' }))).toBe(false);
  });
});

describe('Nutzungsbedingungen', () => {
  it('verlinkt die von Let\'s Encrypt für Produktion und Staging, bei eigener CA nichts', () => {
    expect(termsUrl({ ca: 'letsencrypt' })).toBe(LETS_ENCRYPT_TERMS);
    expect(termsUrl({ ca: 'staging' })).toBe(LETS_ENCRYPT_TERMS);
    expect(termsUrl({ ca: 'custom' })).toBeNull();
  });
});

describe('Abdeckung der Namen', () => {
  const cert = (names: string[], notAfter: string) => ({ id: '1', names, issuer: 'R11', notAfter, fingerprint: '', renewAt: '' });
  const now = Date.parse('2026-11-22T12:00:00Z');

  it('gilt, wenn ein gültiges Zertifikat alle Namen trägt, auch mit mehr Namen', () => {
    expect(coversNames({ certificates: [cert(['a.example.com', 'b.example.com'], '2027-01-01T00:00:00Z')] }, ['a.example.com'], now)).toBe(true);
  });

  it('gilt nicht bei fehlendem Namen, abgelaufenem Zertifikat oder ohne Namen', () => {
    expect(coversNames({ certificates: [cert(['a.example.com'], '2027-01-01T00:00:00Z')] }, ['a.example.com', 'b.example.com'], now)).toBe(false);
    expect(coversNames({ certificates: [cert(['a.example.com'], '2026-11-01T00:00:00Z')] }, ['a.example.com'], now)).toBe(false);
    expect(coversNames({ certificates: [] }, ['a.example.com'], now)).toBe(false);
    expect(coversNames({ certificates: [cert(['a.example.com'], '2027-01-01T00:00:00Z')] }, [], now)).toBe(false);
  });
});
