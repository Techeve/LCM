// Schritt „Zertifikat“: die Wahl zwischen Let's Encrypt und einem Reverse-Proxy, die Proxy-Adressen und die
// Weiterleitung auf die neue Adresse der Oberfläche. Reine Logik; der Schritt steht in step-certificate.ts.

import type { GlobalSettings } from '../../api/settings';
import { isAddressOrCidr, parseNetworks } from '../settings/endpoints/network-model';
import { isIP } from '../../lib/validate';
import { wizardUrl } from './setup-model';

/** Eine dritte Wahl gibt es nicht: Ohne beides liefert LCM sein eigenes Zertifikat. */
export type CertificateChoice = 'letsencrypt' | 'proxy';
export const CERTIFICATE_CHOICES: readonly CertificateChoice[] = ['letsencrypt', 'proxy'];

export const PROXY_KEY = 'trusted_proxies';
export const TRUST_KEY = 'trust_proxy_header';

/** Steht LCM schon hinter einem Proxy, beginnt die Wahl dort; sonst bei Let's Encrypt. */
export function certificateChoice(settings: GlobalSettings): CertificateChoice {
  return settings[TRUST_KEY] === true ? 'proxy' : 'letsencrypt';
}

/** Die Einträge aus dem Textfeld: eine Adresse oder ein Netz je Zeile oder durch Komma getrennt, ohne Doppelte. */
export function proxyEntries(input: string): string[] {
  return parseNetworks(input);
}

export type ProxyProblem = { kind: 'empty' } | { kind: 'invalid'; entries: string[] } | { kind: 'wide'; entries: string[] };

/** Ein Netz mit Präfix 0 (`0.0.0.0/0`, `::/0`) umfasste jede Adresse: Dann glaubte LCM jedem Absender. */
const isWide = (entry: string): boolean => entry.endsWith('/0');

/** Wie das Backend: keine leere Liste, nur Adressen und Netze, keine Netze über alle Adressen. */
export function proxyProblem(entries: string[]): ProxyProblem | null {
  if (entries.length === 0) return { kind: 'empty' };
  const invalid = entries.filter((entry) => !isAddressOrCidr(entry));
  if (invalid.length > 0) return { kind: 'invalid', entries: invalid };
  const wide = entries.filter(isWide);
  return wide.length > 0 ? { kind: 'wide', entries: wide } : null;
}

/** Der Entwurf des Reverse-Proxys: Schalter und Adressliste als Text (eine je Zeile). */
export interface ProxyDraft {
  trust: boolean;
  entries: string;
}

export function proxyDraftOf(settings: GlobalSettings): ProxyDraft {
  return { trust: settings[TRUST_KEY] === true, entries: String(settings[PROXY_KEY] ?? '') };
}

export function proxyChanged(draft: ProxyDraft, settings: GlobalSettings): boolean {
  const saved = proxyDraftOf(settings);
  return draft.trust !== saved.trust || (draft.trust && proxyEntries(draft.entries).join('\n') !== proxyEntries(saved.entries).join('\n'));
}

/** Das Problem der Angaben; nur bei eingeschaltetem Schalter gilt die Liste. */
export function proxyDraftProblem(draft: ProxyDraft): ProxyProblem | null {
  return draft.trust ? proxyProblem(proxyEntries(draft.entries)) : null;
}

/** Der Rumpf für `PATCH /settings/global`: aus nur der Schalter, an mit der Liste. */
export function proxyBody(draft: ProxyDraft): Record<string, unknown> {
  return draft.trust ? { [TRUST_KEY]: true, [PROXY_KEY]: proxyEntries(draft.entries) } : { [TRUST_KEY]: false };
}

/** Der Fehlertext zu einem Problem als Übersetzungsschlüssel samt Platzhalter `list`. */
export function proxyProblemText(problem: ProxyProblem): { key: string; list: string } {
  if (problem.kind === 'empty') return { key: 'setup.certificate.proxy.empty', list: '' };
  const shown = problem.entries.slice(0, 3).join(', ');
  const list = problem.entries.length > 3 ? `${shown} …` : shown;
  return { key: problem.kind === 'invalid' ? 'setup.certificate.proxy.invalid' : 'setup.certificate.proxy.wide', list };
}

/** Was der Schritt zusätzlich speichert: beim Proxy die Liste samt Schalter, bei Let's Encrypt nichts (die Karte speichert selbst). */
export function certificateBody(choice: CertificateChoice, entries: string[]): Record<string, unknown> {
  return choice === 'proxy' ? { [TRUST_KEY]: true, [PROXY_KEY]: entries } : {};
}

function urlOf(url: string): URL | null {
  try {
    return new URL(url);
  } catch {
    return null;
  }
}

/** Trägt die Adresse einen eingetragenen Namen? Nicht `localhost` und keine IP-Adresse (die Rückfalladresse). */
function namedAddress(url: URL): boolean {
  const host = url.hostname.replace(/^\[|\]$/g, '').toLowerCase();
  return host !== '' && host !== 'localhost' && !isIP(host);
}

/** Hat Schritt 2 einen Namen eingetragen (`public_base_url` in A und C, `public_host` in B)? */
function nameConfigured(settings: GlobalSettings): boolean {
  return [settings.public_base_url, settings.public_host].some((value) => typeof value === 'string' && value.trim() !== '');
}

/**
 * Wohin der Browser nach dem Speichern wechselt: der Assistent auf der Adresse der Oberfläche, aber nur, wenn sie einen
 * eingetragenen Namen trägt und von der aktuellen abweicht; sonst `null` (ohne Namen wäre die Adresse `localhost` oder die
 * Rückfalladresse, dort bleibt der Browser). Nennt die Antwort `ui_url`, gilt sie, sonst `effective_urls.ui`.
 */
export function certificateRedirect(updated: GlobalSettings, current: string): string | null {
  const target = urlOf(String(updated.ui_url ?? '') || String(updated['effective_urls.ui'] ?? ''));
  if (!target || !nameConfigured(updated) || !namedAddress(target)) return null;
  if (target.origin === urlOf(current)?.origin) return null;
  return wizardUrl(target.origin);
}
