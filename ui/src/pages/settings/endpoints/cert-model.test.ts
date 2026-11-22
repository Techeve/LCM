import { describe, expect, it } from 'vitest';
import type { Certificate, EndpointCertificate, TlsStatus } from '../../../api/endpoints';
import { EXPIRY_WARN_DAYS, certificateOf, expiryOf, fingerprintText, nameCovers, otherCertificates, servedBy, uploadOutcome, warningKey } from './cert-model';

const NOW = Date.parse('2026-11-21T12:00:00Z');
const DAY = 86_400_000;
const at = (days: number) => new Date(NOW + days * DAY).toISOString();

function cert(id: string, source: Certificate['source'], names: string[], over: Partial<Certificate> = {}): Certificate {
  return { id, source, names, issuer: 'Aussteller', notAfter: at(60), expired: false, fingerprint: 'ab'.repeat(32), warnings: [], ...over };
}

function endpoint(name: EndpointCertificate['name'], host: string, found: Certificate | null): EndpointCertificate {
  return { name, url: host ? `https://${host}` : '', host, cert: found };
}

describe('Ablauf', () => {
  it('warnt ab 21 Tagen und meldet Abgelaufenes', () => {
    expect(expiryOf({ notAfter: at(60), expired: false }, NOW)).toEqual({ state: 'ok', days: 60 });
    expect(expiryOf({ notAfter: at(EXPIRY_WARN_DAYS), expired: false }, NOW)).toEqual({ state: 'ok', days: EXPIRY_WARN_DAYS });
    expect(expiryOf({ notAfter: at(EXPIRY_WARN_DAYS - 0.5), expired: false }, NOW)).toEqual({ state: 'soon', days: EXPIRY_WARN_DAYS - 1 });
    expect(expiryOf({ notAfter: at(0.5), expired: false }, NOW)).toEqual({ state: 'soon', days: 0 });
  });

  it('nimmt abgelaufen vom Server und aus dem Datum', () => {
    expect(expiryOf({ notAfter: at(60), expired: true }, NOW)).toEqual({ state: 'expired' });
    expect(expiryOf({ notAfter: at(-1), expired: false }, NOW)).toEqual({ state: 'expired' });
    expect(expiryOf({ notAfter: '', expired: false }, NOW)).toEqual({ state: 'expired' });
  });
});

describe('Fingerabdruck', () => {
  it('schreibt ihn wie der Browser und kürzt auf acht Bytes', () => {
    const fingerprint = '0123456789abcdef'.repeat(4);
    expect(fingerprintText(fingerprint, false)).toBe('01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF:01:23:45:67:89:AB:CD:EF');
    expect(fingerprintText(fingerprint, true)).toBe('01:23:45:67:89:AB:CD:EF…');
    expect(fingerprintText('abcd', true)).toBe('AB:CD');
  });
});

describe('Warnungen des Servers', () => {
  it('übersetzt die bekannte und lässt eine unbekannte stehen', () => {
    expect(warningKey('kette unvollständig: das zertifikat des ausstellers fehlt in der datei')).toBe('settings.endpoint.cert.warning.chain');
    expect(warningKey('etwas anderes')).toBeNull();
  });
});

describe('Namen eines Zertifikats', () => {
  it('vergleicht ohne Groß-/Kleinschreibung, ein Platzhalter deckt genau ein Label', () => {
    expect(nameCovers(['LCM.example.com'], 'lcm.example.com')).toBe(true);
    expect(nameCovers(['*.example.com'], 'lcm.example.com')).toBe(true);
    expect(nameCovers(['*.example.com'], 'a.b.example.com')).toBe(false);
    expect(nameCovers(['*.example.com'], 'example.com')).toBe(false);
    expect(nameCovers(['lcm.example.com'], 'agent.example.com')).toBe(false);
    expect(nameCovers(['192.0.2.7'], '192.0.2.7')).toBe(true);
  });
});

describe('Stand der Endpunkte', () => {
  const lcm = cert('lcm-ca', 'lcm-ca', ['localhost']);
  const upload = cert('upload-ab12cd34', 'upload', ['lcm.example.com']);
  const config = cert('config-0', 'config', ['other.example.com']);
  const status: TlsStatus = {
    certificates: [config, upload, lcm],
    endpoints: [endpoint('ui', 'lcm.example.com', upload), endpoint('agent', 'agent.example.com', lcm), endpoint('mcp', '', null)],
    sharedPort: null,
  };

  it('findet das Zertifikat je Endpunkt und wer es ausliefert', () => {
    expect(certificateOf(status, 'ui')?.cert?.id).toBe('upload-ab12cd34');
    expect(servedBy(status, 'upload-ab12cd34')).toEqual(['ui']);
    expect(servedBy(status, 'lcm-ca')).toEqual(['agent']);
  });

  it('zeigt die übrigen Zertifikate ohne das gezeigte und ohne das LCM-Zertifikat', () => {
    expect(otherCertificates(status, 'upload-ab12cd34').map((c) => c.id)).toEqual(['config-0']);
    expect(otherCertificates(status, undefined).map((c) => c.id)).toEqual(['config-0', 'upload-ab12cd34']);
  });

  it('sagt nach einem Upload, was für den Dienst und die Agenten gilt', () => {
    const outcome = uploadOutcome(status, 'upload-ab12cd34', 'ui');
    expect(outcome).toMatchObject({ served: true, covers: true, agentKeepsLcmCertificate: 'agent.example.com' });
  });

  it('erkennt ein Zertifikat, das den Namen nicht nennt, und eines, dem ein anderes vorgeht', () => {
    const elsewhere = cert('upload-00000001', 'upload', ['nope.example.com']);
    const notNamed = { ...status, certificates: [...status.certificates, elsewhere] };
    expect(uploadOutcome(notNamed, 'upload-00000001', 'ui')).toMatchObject({ served: false, covers: false });

    const shadowed: TlsStatus = { ...status, endpoints: [endpoint('ui', 'lcm.example.com', config), status.endpoints[1]!, status.endpoints[2]!] };
    expect(uploadOutcome(shadowed, 'upload-ab12cd34', 'ui')).toMatchObject({ served: false, covers: true });
  });

  it('meldet keine Agent-Warnung, wenn der Agent ein eigenes Zertifikat hat oder keine Adresse', () => {
    const own: TlsStatus = { ...status, endpoints: [status.endpoints[0]!, endpoint('agent', 'agent.example.com', upload), status.endpoints[2]!] };
    expect(uploadOutcome(own, 'upload-ab12cd34', 'ui').agentKeepsLcmCertificate).toBe('');
    const none: TlsStatus = { ...status, endpoints: [status.endpoints[0]!, endpoint('agent', '', null), status.endpoints[2]!] };
    expect(uploadOutcome(none, 'upload-ab12cd34', 'ui').agentKeepsLcmCertificate).toBe('');
  });
});
