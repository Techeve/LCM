// Zertifikate der Endpunkte: Ablauf, Kurzform des Fingerabdrucks, Quelle und was ein Upload bewirkt.

import type { Certificate, CertSource, EndpointCertificate, EndpointName, TlsStatus } from '../../../api/endpoints';

/** Ab so vielen Tagen Restlaufzeit weist die Oberfläche auf den Ablauf hin. */
export const EXPIRY_WARN_DAYS = 21;

const DAY_MS = 86_400_000;
const SHORT_FINGERPRINT_BYTES = 8;

export const SOURCE_KEY: Record<CertSource, string> = {
  config: 'settings.endpoint.cert.source.config',
  upload: 'settings.endpoint.cert.source.upload',
  acme: 'settings.endpoint.cert.source.acme',
  'lcm-ca': 'settings.endpoint.cert.source.lcmCa',
};

export type Expiry = { state: 'expired' } | { state: 'soon'; days: number } | { state: 'ok'; days: number };

/** Wie lange das Zertifikat noch gilt; `now` nur für Tests. */
export function expiryOf(cert: Pick<Certificate, 'notAfter' | 'expired'>, now: number = Date.now()): Expiry {
  const end = new Date(cert.notAfter).getTime();
  if (cert.expired || Number.isNaN(end) || end <= now) return { state: 'expired' };
  const days = Math.floor((end - now) / DAY_MS);
  return { state: days < EXPIRY_WARN_DAYS ? 'soon' : 'ok', days };
}

/** `ab12…` als `AB:12:…` wie im Zertifikatsdialog des Browsers; gekürzt auf die ersten Bytes. */
export function fingerprintText(fingerprint: string, short: boolean): string {
  const pairs = fingerprint.toUpperCase().match(/.{1,2}/g) ?? [];
  if (!short || pairs.length <= SHORT_FINGERPRINT_BYTES) return pairs.join(':');
  return `${pairs.slice(0, SHORT_FINGERPRINT_BYTES).join(':')}…`;
}

const CHAIN_WARNING = /^kette unvollständig/;

/** Schlüssel des übersetzten Texts zu einer Warnung des Servers; `null` bei einer unbekannten (dann gilt ihr Text). */
export function warningKey(warning: string): string | null {
  return CHAIN_WARNING.test(warning) ? 'settings.endpoint.cert.warning.chain' : null;
}

export function certificateOf(status: TlsStatus, endpoint: EndpointName): EndpointCertificate | undefined {
  return status.endpoints.find((item) => item.name === endpoint);
}

/** Die Endpunkte, die dieses Zertifikat gerade ausliefern. */
export function servedBy(status: TlsStatus, certId: string): EndpointName[] {
  return status.endpoints.filter((item) => item.cert?.id === certId).map((item) => item.name);
}

/** Die übrigen Zertifikate im Pool, ohne das gezeigte und ohne das LCM-Zertifikat. */
export function otherCertificates(status: TlsStatus, shownId: string | undefined): Certificate[] {
  return status.certificates.filter((cert) => cert.source !== 'lcm-ca' && cert.id !== shownId);
}

/** Wie TLS die Namen eines Zertifikats vergleicht: ohne Groß-/Kleinschreibung, `*` deckt genau ein Label. */
export function nameCovers(names: string[], host: string): boolean {
  const wanted = host.toLowerCase();
  return names.some((raw) => {
    const name = raw.toLowerCase();
    if (!name.startsWith('*.')) return name === wanted;
    const dot = wanted.indexOf('.');
    return dot > 0 && wanted.slice(dot) === name.slice(1);
  });
}

export interface UploadOutcome {
  certificate: Certificate | undefined;
  /** Das neue Zertifikat wird für die Adresse dieses Dienstes ausgeliefert. */
  served: boolean;
  /** Es nennt den Namen dieser Adresse (auch wenn ein Zertifikat mit höherem Rang vorgeht). */
  covers: boolean;
  /** Der Name der Agent-Adresse, für die weiter das LCM-Zertifikat gilt; sonst leer. */
  agentKeepsLcmCertificate: string;
}

/** Was ein Upload für den Dienst im Dialog und für die Agenten bedeutet. */
export function uploadOutcome(status: TlsStatus, uploadId: string, endpoint: EndpointName): UploadOutcome {
  const certificate = status.certificates.find((cert) => cert.id === uploadId);
  const own = certificateOf(status, endpoint);
  const agent = certificateOf(status, 'agent');
  return {
    certificate,
    served: own?.cert?.id === uploadId,
    covers: !!certificate && !!own?.host && nameCovers(certificate.names, own.host),
    agentKeepsLcmCertificate: agent?.host && agent.cert?.source === 'lcm-ca' ? agent.host : '',
  };
}
