// Öffentliche Adressen: Adressmodus, Felder je Modus, abgeleitete Adressen, Prüfung und Weiterleitung nach einem
// Wechsel des Oberflächen-Ports. Reine Logik; die Oberfläche steht in address-block.ts.

import type { EndpointName } from '../../../api/endpoints';
import type { GlobalSettings } from '../../../api/settings';
import { LIMITS, isHost, isIP, isPort } from '../../../lib/validate';
import { choice, flag, int, text, type Draft, type FieldErrors, type FieldSpec } from '../system/form-state';

/** `separate` = A (eigene Ports, eigene Namen), `one_name` = B (ein Name), `shared_port` = C (ein Port). */
export type AddressMode = 'separate' | 'one_name' | 'shared_port';

export const ADDRESS_MODES: readonly AddressMode[] = ['separate', 'one_name', 'shared_port'];
export const DEFAULT_MODE: AddressMode = 'separate';

export const SERVICES: readonly EndpointName[] = ['ui', 'agent', 'mcp'];

/** Schlüssel der Adresse und des Ports je Dienst. */
export const URL_KEY: Record<EndpointName, string> = { ui: 'public_base_url', agent: 'public_agent_url', mcp: 'public_mcp_url' };
export const PORT_KEY: Record<EndpointName, string> = { ui: 'ui_port', agent: 'agent_port', mcp: 'mcp_port' };
/** Schalter an/aus je Dienst; die Oberfläche hat keinen. */
export type SwitchedService = Exclude<EndpointName, 'ui'>;
export const SWITCHED: readonly SwitchedService[] = ['agent', 'mcp'];
export const ENABLED_KEY: Record<SwitchedService, string> = { agent: 'agent_enabled', mcp: 'mcp_enabled' };
export const SHARED_PORT_KEY = 'shared_tls_port';
export const HOST_KEY = 'public_host';
export const MODE_KEY = 'address_mode';

export const DEFAULT_PORTS: Record<string, number> = { ui_port: 9310, agent_port: 9320, mcp_port: 9330, shared_tls_port: 443 };

/** Der Port, bei dem der Browser keinen Port in der Adresse zeigt. */
const HTTPS_PORT = '443';

/** Alle Schlüssel der Adressfelder samt Schaltern; nur diese ändert der Block. */
export const ADDRESS_KEYS: readonly string[] = [MODE_KEY, HOST_KEY, ...Object.values(URL_KEY), ...Object.values(PORT_KEY), SHARED_PORT_KEY, ...Object.values(ENABLED_KEY)];

/** Die Felder des Blocks als Formularfelder: Einstellungen → Allgemein und der Einrichtungsassistent nutzen dieselben. */
export const ADDRESS_FIELDS: FieldSpec[] = [
  choice(MODE_KEY, () => DEFAULT_MODE),
  text(HOST_KEY, LIMITS.host, 'host'),
  int(PORT_KEY.ui, 0, 65535),
  int(PORT_KEY.agent, 0, 65535),
  int(PORT_KEY.mcp, 0, 65535),
  int(SHARED_PORT_KEY, 0, 65535),
  text(URL_KEY.ui, LIMITS.path, 'url'),
  text(URL_KEY.agent, LIMITS.path, 'url'),
  text(URL_KEY.mcp, LIMITS.path, 'url'),
  flag(ENABLED_KEY.agent),
  flag(ENABLED_KEY.mcp),
];

export type Urls = Record<EndpointName, string>;

export function readMode(value: unknown): AddressMode {
  return value === 'one_name' || value === 'shared_port' ? value : DEFAULT_MODE;
}

const raw = (value: unknown): string => (typeof value === 'string' || typeof value === 'number' ? String(value).trim() : '');

/** Die Schlüssel, die im Modus eine Rolle spielen. */
export function keysOf(mode: AddressMode): string[] {
  const ports = mode === 'shared_port' ? [SHARED_PORT_KEY] : Object.values(PORT_KEY);
  const names = mode === 'one_name' ? [HOST_KEY] : Object.values(URL_KEY);
  return [MODE_KEY, ...names, ...ports];
}

/** Legt die `config.json` den Port fest? Die Antwort trägt `address_locks` als flache Schlüssel `address_locks.<feld>`. */
export function isLocked(settings: GlobalSettings, key: string): boolean {
  return settings[`address_locks.${key}`] === true;
}

const SCHEME = /^[a-z][a-z0-9+.-]*:\/\//i;

/** Eine Eingabe ohne Schema und Pfad; ein Port bleibt stehen (Modus A: `name:port`). */
export function cleanEntry(value: string): string {
  const rest = value.trim().replace(SCHEME, '');
  return rest.split(/[/?#]/)[0] ?? '';
}

/** Der Name aus einer Eingabe: ohne Schema, Pfad und Port. */
export function hostOf(value: string): string {
  return cleanEntry(value).replace(/:\d*$/, '');
}

/** Was ein Namensfeld zeigt: `name[:port]` aus der gespeicherten Adresse, ohne den Port 443 (den zeigt kein Browser). */
export function entryOf(url: string): string {
  return cleanEntry(url).replace(/:443$/, '');
}

/** Die Adresse für die API aus der Eingabe `name[:port]`: immer `https://`, ohne Port 443; leer bleibt leer. */
export function urlOfEntry(entry: string): string {
  const shown = entryOf(entry);
  return shown === '' ? '' : `https://${shown}`;
}

/** Hat die Eingabe mehr als einen Namen (Schema, Port oder Pfad)? */
export function isBareHost(value: string): boolean {
  return hostOf(value) === value.trim();
}

function hostnameOfUrl(url: string): string {
  try {
    return new URL(url).hostname.toLowerCase();
  } catch {
    return '';
  }
}

/** Eine Adresse so, wie Browser sie zeigen: der Port fehlt bei 443. */
export function addressOf(host: string, port: string | number): string {
  if (host === '') return '';
  const number = String(port).trim();
  return number === '' || number === HTTPS_PORT ? `https://${host}` : `https://${host}:${number}`;
}

/** Modus B: ein Name, je Dienst sein Port. Ein Agent-Port 0 heißt: kein Agent-Listener. */
function oneNameUrls(draft: Draft): Urls {
  const text = (key: string): string => String(draft[key] ?? '').trim();
  const host = text(HOST_KEY);
  const build = (service: EndpointName): string => {
    const port = text(PORT_KEY[service]);
    if (host === '' || (service === 'agent' && (port === '' || port === '0'))) return '';
    return `https://${host}:${port}`;
  };
  return { ui: build('ui'), agent: build('agent'), mcp: build('mcp') };
}

/** Die Adressen, die LCM nach dem Speichern des Entwurfs nutzt (Vorschau; maßgeblich ist `effective_urls`). */
export function previewUrls(draft: Draft): Urls {
  const text = (key: string): string => String(draft[key] ?? '').trim();
  const mode = readMode(draft[MODE_KEY]);
  const each = (build: (service: EndpointName) => string): Urls => ({ ui: build('ui'), agent: build('agent'), mcp: build('mcp') });
  if (mode === 'one_name') return oneNameUrls(draft);
  if (mode === 'shared_port') return each((service) => addressOf(hostOf(text(URL_KEY[service])), text(SHARED_PORT_KEY)));
  return each((service) => text(URL_KEY[service]));
}

/** Die Adressen, die LCM nach dem letzten Speichern in Befehle schreibt. */
export function effectiveUrls(settings: GlobalSettings): Urls {
  const text = (service: EndpointName): string => raw(settings[`effective_urls.${service}`]);
  return { ui: text('ui'), agent: text('agent'), mcp: text('mcp') };
}

/** Weicht der Entwurf in einem Feld des Modus vom gespeicherten Stand ab? */
export function addressChanged(draft: Draft, settings: GlobalSettings): boolean {
  const mode = readMode(draft[MODE_KEY]);
  if (mode !== readMode(settings[MODE_KEY])) return true;
  return keysOf(mode).some((key) => String(draft[key] ?? '').trim() !== raw(settings[key]));
}

/** Je Dienst die Adresse zum Anzeigen: gespeichert die von LCM gemeldete, sonst die Vorschau des Entwurfs. */
export function shownUrls(draft: Draft, settings: GlobalSettings): Urls {
  const preview = previewUrls(draft);
  if (addressChanged(draft, settings)) return preview;
  const effective = effectiveUrls(settings);
  return { ui: effective.ui || preview.ui, agent: effective.agent || preview.agent, mcp: effective.mcp || preview.mcp };
}

/** Welche Dienste an sind, aus dem Entwurf oder den gespeicherten Einstellungen. Der Agent ist ohne Angabe an, MCP aus. */
export function enabledOf(source: Draft | GlobalSettings): Record<EndpointName, boolean> {
  return { ui: true, agent: source[ENABLED_KEY.agent] !== false, mcp: source[ENABLED_KEY.mcp] === true };
}

/**
 * Die Namen für Let's Encrypt, wie das Backend sie bildet (`SettingsService.ACMENames`): die Namen der Adressen,
 * ohne Doppelte, ohne IP-Adressen und ohne Namen ohne Punkt (`localhost`): Dafür stellt keine öffentliche CA aus.
 * Agent und MCP zählen nur, solange sie eingeschaltet sind.
 */
export function acmeNames(urls: Urls, enabled: Record<EndpointName, boolean> = { ui: true, agent: true, mcp: true }): string[] {
  const services = SERVICES.filter((service) => enabled[service]);
  const hosts = services.map((service) => hostnameOfUrl(urls[service])).filter((host) => host.includes('.') && !isIP(host.replace(/^\[|\]$/g, '')));
  return [...new Set(hosts)];
}

/** Beim Wechsel in Modus C werden die Adressen zu reinen Namen (`https://name`), denn dort trägt der gemeinsame Port die Adresse. */
export function sharedNames(draft: Draft): Record<string, string> {
  const names: Record<string, string> = {};
  for (const service of SERVICES) {
    const key = URL_KEY[service];
    const host = hostOf(String(draft[key] ?? ''));
    if (host !== '') names[key] = `https://${host}`;
  }
  return names;
}

/** Die Namen der gespeicherten Adressen und ob der Entwurf auf andere kommt: Let's Encrypt nutzt die gespeicherten. */
export function certificateNames(draft: Draft, settings: GlobalSettings): { saved: string[]; pending: boolean } {
  const effective = effectiveUrls(settings);
  const fromFields = previewUrls(Object.fromEntries(ADDRESS_KEYS.map((key) => [key, String(settings[key] ?? '')])));
  const saved = acmeNames({ ui: effective.ui || fromFields.ui, agent: effective.agent || fromFields.agent, mcp: effective.mcp || fromFields.mcp }, enabledOf(settings));
  const draftNames = acmeNames(shownUrls(draft, settings), enabledOf(draft));
  return { saved, pending: draftNames.join(',') !== saved.join(',') };
}

/** Warum ein Schalter nicht frei ist: die `config.json` legt ihn fest, oder in Modus C fehlt der Name des Dienstes. */
export type SwitchLock = 'config' | 'noName';

/** Stand des Schalters eines Dienstes: ohne Namen in Modus C aus und gesperrt, sonst der Wert des Entwurfs. */
export function switchState(draft: Draft, settings: GlobalSettings, service: SwitchedService): { checked: boolean; lock?: SwitchLock } {
  const checked = enabledOf(draft)[service];
  if (readMode(draft[MODE_KEY]) === 'shared_port' && hostOf(String(draft[URL_KEY[service]] ?? '')) === '') return { checked: false, lock: 'noName' };
  if (isLocked(settings, ENABLED_KEY[service])) return { checked, lock: 'config' };
  return { checked };
}

/** Beim Wechsel in Modus C verlieren Dienste ohne Namen ihre Freigabe: Sie sind dort aus. */
export function sharedSwitchOff(draft: Draft, names: Record<string, string>): Record<string, boolean> {
  const off: Record<string, boolean> = {};
  for (const service of SWITCHED) {
    const named = (names[URL_KEY[service]] ?? '') !== '';
    if (!named && enabledOf(draft)[service]) off[ENABLED_KEY[service]] = false;
  }
  return off;
}

/** Ports, die beim Wechsel in einen Modus noch leer sind, bekommen die Vorgabe. */
export function portDefaults(draft: Draft, mode: AddressMode): Record<string, string> {
  const defaults: Record<string, string> = {};
  for (const key of keysOf(mode).filter((name) => name in DEFAULT_PORTS)) {
    const value = String(draft[key] ?? '').trim();
    if (value === '' || value === '0') defaults[key] = String(DEFAULT_PORTS[key]);
  }
  return defaults;
}

const ERR = 'settings.general.address.errors';

function portErrors(draft: Draft, keys: string[]): FieldErrors {
  const errors: FieldErrors = {};
  const seen = new Set<string>();
  for (const key of keys) {
    const value = String(draft[key] ?? '').trim();
    if (!isPort(value)) errors[key] = `${ERR}.port`;
    else if (seen.has(value)) errors[key] = `${ERR}.portsDiffer`;
    seen.add(value);
  }
  return errors;
}

function hostErrors(draft: Draft): FieldErrors {
  const host = String(draft[HOST_KEY] ?? '').trim();
  if (host === '') return { [HOST_KEY]: `${ERR}.hostRequired` };
  return !isBareHost(host) || !isHost(host) ? { [HOST_KEY]: `${ERR}.hostFormat` } : {};
}

/** Modus C: der Name der Oberfläche ist Pflicht, Agent und MCP dürfen ohne bleiben; gesetzte Namen sind verschieden. */
function nameErrors(draft: Draft): FieldErrors {
  const errors: FieldErrors = {};
  const seen = new Set<string>();
  for (const service of SERVICES) {
    const key = URL_KEY[service];
    const host = hostOf(String(draft[key] ?? ''));
    if (host === '' && service === 'ui') errors[key] = `${ERR}.nameRequired`;
    else if (host !== '' && !isHost(host)) errors[key] = `${ERR}.nameInvalid`;
    else if (host !== '' && seen.has(host.toLowerCase())) errors[key] = `${ERR}.namesDiffer`;
    if (host !== '') seen.add(host.toLowerCase());
  }
  return errors;
}

/** Modus A: je Adresse `name` oder `name:port`; leere Felder sind erlaubt (die Oberfläche warnt dort). */
function entryErrors(draft: Draft): FieldErrors {
  const errors: FieldErrors = {};
  for (const service of SERVICES) {
    const key = URL_KEY[service];
    const entry = cleanEntry(String(draft[key] ?? ''));
    if (entry === '') continue;
    const [, host = '', port] = /^(\[[^\]]*\]|[^:]*)(?::(.*))?$/.exec(entry) ?? [];
    if (!isHost(host)) errors[key] = `${ERR}.nameInvalid`;
    else if (port !== undefined && !isPort(port)) errors[key] = `${ERR}.entryPort`;
  }
  return errors;
}

/** Was LCM beim Speichern ablehnen würde: Werte sind Übersetzungsschlüssel, die Schlüssel die der Felder. */
export function validateAddresses(draft: Draft): FieldErrors {
  const mode = readMode(draft[MODE_KEY]);
  if (mode === 'shared_port') return { ...portErrors(draft, [SHARED_PORT_KEY]), ...nameErrors(draft) };
  const ports = portErrors(draft, Object.values(PORT_KEY));
  return mode === 'one_name' ? { ...hostErrors(draft), ...ports } : { ...entryErrors(draft), ...ports };
}

/** Die Adresse, unter der die Oberfläche nach einem Portwechsel läuft; `null`, wenn der Port gleich bleibt oder nicht gilt. */
export function uiPortChange(draft: Draft, settings: GlobalSettings, current: string): string | null {
  const mode = readMode(draft[MODE_KEY]);
  const port = String(draft[PORT_KEY.ui] ?? '').trim();
  if (mode === 'shared_port' || isLocked(settings, PORT_KEY.ui) || !isPort(port) || port === raw(settings[PORT_KEY.ui])) return null;
  if (mode === 'one_name') {
    const host = String(draft[HOST_KEY] ?? '').trim();
    if (host !== '') return `https://${host}:${port}`;
  }
  try {
    const url = new URL(current);
    url.port = port;
    return url.origin;
  } catch {
    return null;
  }
}

function sameOrigin(a: string, b: string): boolean {
  try {
    return new URL(a).origin === new URL(b).origin;
  } catch {
    return false;
  }
}

/**
 * Wohin die Seite nach dem Speichern wechselt. Nennt die Antwort des Servers `ui_url`, gilt sie. Sonst
 * folgt die Seite nach einem Portwechsel `effective_urls.ui`, und nur wenn die fehlt, der vorab genannten Adresse.
 * Zeigt das Ziel auf den Ursprung, an dem die Seite schon läuft (etwa hinter einem Proxy), bleibt sie stehen.
 */
export function redirectTarget(updated: GlobalSettings | void, current: string, portChanged: boolean, announced: string | null): string | undefined {
  if (!updated) return undefined;
  const explicit = raw(updated.ui_url);
  const target = explicit || (portChanged ? raw(updated['effective_urls.ui']) || (announced ?? '') : '');
  return target !== '' && !sameOrigin(target, current) ? target : undefined;
}
