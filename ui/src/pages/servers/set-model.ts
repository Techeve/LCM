// Prüf- und Vorschlagslogik des Einstellungsfensters. Die Grenzen folgen dem Backend: PATCH /settings prüft Name
// (checkLine, 64), Adresse (checkHost) und Port; die Aktionen prüfen Nameserver (net.ParseIP), Zeitserver
// (Hostname oder IP) und die Zeitzone (IANA-Muster, höchstens 64 Zeichen).

import type { SettingsForm } from '../../api/servers-settings';
import { LIMITS, charCount, isHost, isHostname, isIP, isIPv4, isLine, isPort, splitList } from '../../lib/validate';

export type SettingsField = 'name' | 'host' | 'port' | 'grace';

/** Fehlertexte (Sprachschlüssel) je Feld. */
export type SettingsErrors = Partial<Record<SettingsField, string>>;

const GRACE_MIN = 1;
const GRACE_MAX = 365;
const TIMEZONE_MAX = 64;
// Wie reTimezone in internal/core/services/timesync.go.
const TIMEZONE = /^[A-Za-z][A-Za-z0-9+_-]*(\/[A-Za-z0-9+._-]+){0,2}$/;

function nameError(name: string): string {
  if (name === '') return 'serverPackages.settings.invalid.name';
  return isLine(name, LIMITS.name) ? '' : 'serverPackages.settings.invalid.nameFormat';
}

function hostError(host: string): string {
  if (host === '') return 'serverPackages.settings.invalid.host';
  return charCount(host) <= LIMITS.host && isHost(host) ? '' : 'serverPackages.settings.invalid.hostFormat';
}

/**
 * Prüft das Formular wie das Backend. Name und Adresse werden nur geprüft, wenn sie geändert sind: Das Backend
 * prüft nur, was im PATCH steht, und ein gespeicherter Wert soll das Speichern anderer Felder nicht sperren.
 * Agent-Server haben keine Verbindungsadresse (`ssh` = false).
 */
export function settingsErrors(base: SettingsForm, form: SettingsForm, ssh = true): SettingsErrors {
  const errors: SettingsErrors = {};
  const name = form.name.trim();
  const host = form.host.trim();
  const nameMessage = name === base.name ? '' : nameError(name);
  if (nameMessage) errors.name = nameMessage;
  if (ssh) {
    const hostMessage = host === base.host ? '' : hostError(host);
    if (hostMessage) errors.host = hostMessage;
    if (!isPort(form.port)) errors.port = 'serverPackages.settings.invalid.port';
  }
  if (form.unreachable_uncritical && !validGrace(form.unreachable_grace_days)) errors.grace = 'serverPackages.settings.invalid.grace';
  return errors;
}

export function validGrace(days: number): boolean {
  return Number.isInteger(days) && days >= GRACE_MIN && days <= GRACE_MAX;
}

/** Ein Nameserver-Feld: leer oder eine IP-Adresse. */
export function validNameserver(value: string): boolean {
  const v = value.trim();
  return v === '' || isIP(v);
}

/** Ein Zeitserver-Feld: leer oder Hostname bzw. IP. */
export function validTimeServer(value: string): boolean {
  const v = value.trim();
  return v === '' || (charCount(v) <= LIMITS.host && isHost(v));
}

export function validTimezone(value: string): boolean {
  const v = value.trim();
  return v.length <= TIMEZONE_MAX && TIMEZONE.test(v);
}

/** Loopback und Link-Local taugen nicht als Adresse, unter der LCM einen Server erreicht. */
function isLocalOnly(ip: string): boolean {
  if (isIPv4(ip)) return ip.startsWith('127.') || ip.startsWith('169.254.');
  const v6 = ip.toLowerCase();
  return v6 === '::1' || /^fe[89ab]/.test(v6);
}

const bare = (host: string) => host.trim().replace(/^\[(.*)\]$/, '$1').toLowerCase();

/**
 * Vorschläge für die Verbindungsadresse: die IP-Adressen aus dem letzten Scan (`hostname -I`, durch Komma
 * getrennt) und der Systemname, sofern er ein voll qualifizierter Name ist. Die eingetragene Adresse fehlt,
 * Loopback und Link-Local auch.
 */
export function addressSuggestions(ipAddresses: string, hostname: string, current: string): string[] {
  const ips = splitList(ipAddresses).filter((ip) => isIP(ip) && !isLocalOnly(ip));
  const fqdn = hostname.includes('.') && isHostname(hostname) ? [hostname] : [];
  const seen = new Set([bare(current)]);
  return [...ips, ...fqdn].filter((candidate) => {
    const key = bare(candidate);
    if (seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}
