// Let's Encrypt / ACME: Entwurf der Einstellungen, Auswahl der CA, Prüfungen und der Zustand als Ampel.

import type { AcmeBody, AcmeChallenge, AcmeState } from '../../../api/endpoints';
import type { StatusLevel } from '../../../components/shared';
import { MAX_NAMESERVERS, dnsBodyOf, dnsChanged, dnsDraftOf, missingDnsField, parseNameservers, type DnsDraft } from './acme-dns-model';

export const LETS_ENCRYPT = 'https://acme-v02.api.letsencrypt.org/directory';
export const LETS_ENCRYPT_STAGING = 'https://acme-staging-v02.api.letsencrypt.org/directory';

/** Die Nutzungsbedingungen von Let's Encrypt (Produktion und Staging). Die API nennt keine Adresse aus dem Verzeichnis der CA. */
export const LETS_ENCRYPT_TERMS = 'https://letsencrypt.org/repository/';

export type AcmeCa = 'letsencrypt' | 'staging' | 'custom';

/**
 * Entwurf der Einstellungen. Die Namen stehen nicht darin: Sie kommen aus den öffentlichen Adressen, und
 * Let's Encrypt stellt immer ein Zertifikat für alle aus.
 */
export interface AcmeDraft {
  enabled: boolean;
  email: string;
  challenge: AcmeChallenge;
  dns: DnsDraft;
  ca: AcmeCa;
  /** Nur bei `ca === 'custom'`. */
  customUrl: string;
  acceptTerms: boolean;
}

export function caOf(directoryUrl: string): AcmeCa {
  if (directoryUrl === '' || directoryUrl === LETS_ENCRYPT) return 'letsencrypt';
  return directoryUrl === LETS_ENCRYPT_STAGING ? 'staging' : 'custom';
}

export function directoryOf(draft: Pick<AcmeDraft, 'ca' | 'customUrl'>): string {
  if (draft.ca === 'letsencrypt') return LETS_ENCRYPT;
  return draft.ca === 'staging' ? LETS_ENCRYPT_STAGING : draft.customUrl.trim();
}

export function draftOf(state: AcmeState): AcmeDraft {
  const ca = caOf(state.directoryUrl);
  return {
    enabled: state.enabled,
    email: state.email,
    challenge: state.challenge,
    dns: dnsDraftOf(state),
    ca,
    customUrl: ca === 'custom' ? state.directoryUrl : '',
    acceptTerms: false,
  };
}

export function isDirty(draft: AcmeDraft, state: AcmeState): boolean {
  const saved = draftOf(state);
  return (
    draft.enabled !== saved.enabled ||
    draft.email.trim() !== saved.email ||
    draft.challenge !== saved.challenge ||
    (draft.challenge === 'dns-01' && dnsChanged(draft.dns, state)) ||
    directoryOf(draft) !== directoryOf(saved)
  );
}

/** Die Nutzungsbedingungen der CA fehlen noch: Beim Einschalten verlangt LCM sie einmal je CA. */
export function needsTerms(draft: AcmeDraft, state: AcmeState): boolean {
  const alreadyAccepted = state.termsAccepted && directoryOf(draft) === state.directoryUrl;
  return draft.enabled && !alreadyAccepted;
}

export type AcmeProblem =
  | 'namesRequired'
  | 'directoryRequired'
  | 'nameserversTooMany'
  | 'providerRequired'
  | 'fieldMissing'
  | 'termsRequired';

/** Das erste Problem der Angaben zu DNS-01: Anbieter und Pflichtfelder gelten erst beim Einschalten. */
function dnsProblemOf(draft: AcmeDraft, state: AcmeState): AcmeProblem | null {
  if (parseNameservers(draft.dns.nameservers).length > MAX_NAMESERVERS) return 'nameserversTooMany';
  if (!draft.enabled) return null;
  if (draft.dns.provider === '') return 'providerRequired';
  return missingDnsField(draft.dns, state) === null ? null : 'fieldMissing';
}

/**
 * Das erste Problem, das LCM beim Speichern ablehnen würde; `null`, wenn die Angaben passen. `names` sind die Namen
 * der gespeicherten Adressen: Ohne sie gibt es nichts auszustellen.
 */
export function problemOf(draft: AcmeDraft, state: AcmeState, names: string[]): AcmeProblem | null {
  if (draft.enabled && names.length === 0) return 'namesRequired';
  if (draft.ca === 'custom' && directoryOf(draft) === '') return 'directoryRequired';
  const dnsProblem = draft.challenge === 'dns-01' ? dnsProblemOf(draft, state) : null;
  if (dnsProblem) return dnsProblem;
  if (needsTerms(draft, state) && !draft.acceptTerms) return 'termsRequired';
  return null;
}

/** Der Rumpf der Anfrage. Die Namen aus den Adressen gehen mit, ein Zertifikat gilt immer für alle (`single`). */
export function bodyOf(draft: AcmeDraft, state: AcmeState, names: string[]): AcmeBody {
  return {
    enabled: draft.enabled,
    directory_url: directoryOf(draft),
    email: draft.email.trim(),
    names,
    mode: 'single',
    challenge: draft.challenge,
    accept_terms: draft.acceptTerms,
    ...(draft.challenge === 'dns-01' ? dnsBodyOf(draft.dns, state) : {}),
  };
}

/** Wo die Nutzungsbedingungen der gewählten CA stehen; `null` bei einer eigenen CA, deren Adresse LCM nicht kennt. */
export function termsUrl(draft: Pick<AcmeDraft, 'ca'>): string | null {
  return draft.ca === 'custom' ? null : LETS_ENCRYPT_TERMS;
}

/** Trägt ein gültiges Zertifikat alle Namen? Dann ist hier nichts mehr zu tun. */
export function coversNames(state: Pick<AcmeState, 'certificates'>, names: string[], now = Date.now()): boolean {
  if (names.length === 0) return false;
  return state.certificates.some((cert) => Date.parse(cert.notAfter) > now && names.every((name) => cert.names.includes(name)));
}

/** Der Host der CA für den Satz zu den Nutzungsbedingungen. */
export function directoryHost(directoryUrl: string): string {
  try {
    return new URL(directoryUrl || LETS_ENCRYPT).host;
  } catch {
    return directoryUrl;
  }
}

export interface AcmeStatus {
  level: StatusLevel;
  /** Schlüssel des Texts neben der Ampel. */
  labelKey: string;
}

/** Ein Status je Karte: Er sagt, ob etwas zu tun ist. */
export function statusOf(state: AcmeState): AcmeStatus {
  if (!state.enabled) return { level: 'unknown', labelKey: 'settings.acme.status.off' };
  if (state.running) return { level: 'running', labelKey: 'settings.acme.status.running' };
  if (state.lastError) return { level: 'crit', labelKey: 'settings.acme.status.failed' };
  if (state.certificates.length === 0) return { level: 'warn', labelKey: 'settings.acme.status.waiting' };
  return { level: 'great', labelKey: 'settings.acme.status.active' };
}

/** Bindet der Listener für HTTP-01 nicht, hilft meist die Capability für Ports unter 1024. */
export function listenerNeedsCapability(state: AcmeState): boolean {
  const port = Number(state.httpListen.split(':').at(-1));
  return state.enabled && state.listenerError !== '' && port > 0 && port < 1024;
}

/** Dateiname für die heruntergeladene Zertifikatskette: der erste Name, ohne Zeichen, die Dateisysteme stören. */
export function chainFileName(names: string[]): string {
  const stem = (names[0] ?? 'certificate').replace(/^\*\./, 'wildcard.').replace(/[^a-z0-9.-]+/gi, '_');
  return `${stem}-chain.pem`;
}
