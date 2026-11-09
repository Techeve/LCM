// Serverdetail, Reiter Pakete, Repositories und Apps: Paket-Pins, Versionswahl, Paketquellen aus dem Katalog,
// APT-Cache-Anbindung und Dienste ohne Paket. Lesen über `LiveData`, Ändern über `packagesApi`/`reposApi`.

import { api } from '../lib/api';
import { LIMITS, hasControlChars } from '../lib/validate';
import { SERVERS_URL, type StartedJob } from './servers';

type Raw = Record<string, unknown>;

const isRecord = (value: unknown): value is Raw => typeof value === 'object' && value !== null;
const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const num = (value: unknown): number => (typeof value === 'number' && Number.isFinite(value) ? value : 0);
const records = (value: unknown): Raw[] => (Array.isArray(value) ? value.filter(isRecord) : []);

export const pinsUrl = (id: number) => `${SERVERS_URL}/${id}/packages/pins`;
export const versionsUrl = (id: number, name: string) => `${SERVERS_URL}/${id}/packages/${encodeURIComponent(name)}/versions`;
export const KNOWN_REPOS_URL = `${SERVERS_URL}/known-repos`;
export const APT_CACHES_URL = '/api/v1/apt-caches';

// ---- Paket-Pins ----

export interface PackagePin {
  id: number;
  name: string;
  /** 0 = gilt für alle Server. */
  serverId: number;
  noRemove: boolean;
  hold: boolean;
  note: string;
}

export interface PinsView {
  pins: PackagePin[];
  available: boolean;
  reason: string;
  kernelPreset: string[];
}

function normalisePin(r: Raw): PackagePin {
  return { id: num(r.id), name: text(r.name), serverId: num(r.server_id), noRemove: r.no_remove === true, hold: r.hold === true, note: text(r.note) };
}

/** Globale und serverspezifische Pins in einer Liste (globale zuerst), dazu Verfügbarkeit und Kernel-Vorschlag. */
export function normalisePins(raw: unknown): PinsView {
  const r = isRecord(raw) ? raw : {};
  return {
    pins: [...records(r.global), ...records(r.server)].map(normalisePin),
    available: r.available === true,
    reason: text(r.reason),
    kernelPreset: (Array.isArray(r.kernel_preset) ? r.kernel_preset : []).filter((p): p is string => typeof p === 'string'),
  };
}

/** Der Pin, der ein Paket schützt. Ein abschließendes `*` wirkt als Präfix (`linux-image-*`). */
export function pinFor(pins: readonly PackagePin[], name: string): PackagePin | undefined {
  const lower = name.toLowerCase();
  return pins.find((pin) => (pin.name.endsWith('*') ? lower.startsWith(pin.name.slice(0, -1).toLowerCase()) : lower === pin.name.toLowerCase()));
}

export interface PinInput {
  name: string;
  no_remove: boolean;
  hold: boolean;
  note: string;
  global: boolean;
}

/** Höchstlänge des Paketnamens eines Pins (`validPinName`). */
export const PIN_NAME_MAX = 128;

/** Fehlerschlüssel des Namens eines Pins: Paketname oder Muster mit `*` am Ende, wie `validPinName`. */
export function pinNameProblem(name: string): string | undefined {
  const value = name.trim().toLowerCase();
  if (value === '') return undefined;
  if ([...value].length > PIN_NAME_MAX) return 'check.tooLong';
  return /^[a-z0-9._+-]+\*?$/.test(value) ? undefined : 'serverPackages.pins.new.nameInvalid';
}

/** Ein Pin braucht einen gültigen Namen, höchstens eine Notiz bis zur Grenze und mindestens eine Wirkung. */
export function pinInputValid(input: PinInput): boolean {
  return input.name.trim() !== '' && pinNameProblem(input.name) === undefined && [...input.note.trim()].length <= LIMITS.description && !hasControlChars(input.note) && (input.no_remove || input.hold);
}

// ---- Versionen ----

export function normaliseVersions(raw: unknown): string[] {
  return (Array.isArray(raw) ? raw : []).filter((v): v is string => typeof v === 'string' && v !== '');
}

/** Versionen teilweise numerisch vergleichen: „1.10“ ist neuer als „1.9“. Nur für den Hinweis „Downgrade“. */
export function versionLess(a: string, b: string): boolean {
  const pa = a.split(/[.\-+~:]/);
  const pb = b.split(/[.\-+~:]/);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? '';
    const y = pb[i] ?? '';
    if (x === y) continue;
    const nx = Number.parseInt(x, 10);
    const ny = Number.parseInt(y, 10);
    if (!Number.isNaN(nx) && !Number.isNaN(ny) && nx !== ny) return nx < ny;
    return x < y;
  }
  return false;
}

// ---- Paketquellen ----

export interface KnownRepo {
  key: string;
  name: string;
  description: string;
  keyUrl: string;
  packageManager: string;
}

export function normaliseKnownRepos(raw: unknown): KnownRepo[] {
  return records(raw).map((r) => ({
    key: text(r.key),
    name: text(r.name),
    description: text(r.description),
    keyUrl: text(r.key_url),
    // Altbestand ohne Angabe gilt als apt (wie im Backend).
    packageManager: text(r.package_manager) || 'apt',
  }));
}

/** Nur Katalogquellen für die Paketverwaltung dieses Servers; eine apt-Quelle passt nicht auf ein dnf-System. */
export function reposFor(repos: readonly KnownRepo[], packageManager: string): KnownRepo[] {
  const mine = packageManager || 'apt';
  return repos.filter((repo) => repo.packageManager === mine);
}

export interface AptCache {
  id: number;
  name: string;
  url: string;
}

export function normaliseAptCaches(raw: unknown): AptCache[] {
  return records(raw).map((r) => ({ id: num(r.id), name: text(r.name), url: text(r.url) }));
}

/** Was der Reiter Repositories über die Quellenliste hinaus vom Server braucht. */
export interface RepoState {
  packageManager: string;
  proxmox: boolean;
  revertUrls: string[];
  /** http-Quellen, die bei der letzten Prüfung kein HTTPS boten. */
  unavailableUrls: string[];
  aptProxyActive: boolean;
  aptProxyUrl: string;
}

const csv = (value: unknown): string[] =>
  text(value)
    .split(',')
    .map((url) => url.trim())
    .filter(Boolean);

export function normaliseRepoState(raw: unknown): RepoState {
  const r = isRecord(raw) ? raw : {};
  return {
    packageManager: text(r.package_manager) || 'apt',
    proxmox: text(r.proxmox_type) !== '',
    revertUrls: csv(r.https_revert_urls),
    unavailableUrls: csv(r.https_unavailable_urls),
    aptProxyActive: r.apt_proxy_active === true,
    aptProxyUrl: text(r.apt_proxy_url),
  };
}

// ---- HTTPS-Prüfung vor der Umstellung ----

/** Ergebnis je Quelle: HTTPS geht, geht nicht, oder ließ sich nicht prüfen. */
export type HttpsState = 'ok' | 'no_https' | 'unknown';

export interface HttpsCheckResult {
  /** Die Quelle in ihrer http-Form - so geht sie auch an die Umstellung. */
  uri: string;
  state: HttpsState;
  /** Spiegel derselben Distribution mit HTTPS; leer, wenn LCM keinen kennt. */
  alternative: string;
}

const HTTPS_STATES: readonly HttpsState[] = ['ok', 'no_https', 'unknown'];

/** Fehlen dem Server die Stammzertifikate, sagt die Prüfung nichts über die Spiegel. */
export function httpsCheckMissingCerts(raw: unknown): boolean {
  return isRecord(raw) && raw.missing_certificates === true;
}

export function normaliseHttpsCheck(raw: unknown): HttpsCheckResult[] {
  const r = isRecord(raw) ? raw : {};
  return records(r.results)
    .map((x) => {
      const state = text(x.state) as HttpsState;
      return { uri: text(x.uri), state: HTTPS_STATES.includes(state) ? state : 'unknown', alternative: text(x.alternative) };
    })
    .filter((x) => x.uri !== '');
}

/** Vorausgewählt ist, was nachweislich HTTPS spricht; Ungeprüftes wählt der Anwender selbst. */
export function initialHttpsSelection(results: HttpsCheckResult[]): string[] {
  return results.filter((x) => x.state === 'ok').map((x) => x.uri);
}

/** Schaltet eine Quelle in der Auswahl an oder ab - die Reihenfolge der Prüfung bleibt erhalten. */
export function toggleHttpsSelection(results: HttpsCheckResult[], chosen: string[], uri: string, on: boolean): string[] {
  return results.map((x) => x.uri).filter((u) => (u === uri ? on : chosen.includes(u)));
}

/** Die erste http(s)-URL einer Quellen-Zeile, wie `domain.RepoURI` im Backend. */
export function repoUri(line: string): string {
  const field = line.split(/\s+/).find((f) => f.startsWith('http://') || f.startsWith('https://'));
  return field ? field.replace(/[,;]+$/, '') : '';
}

/**
 * Spiegel mit HTTPS für eine Quelle ohne, wie `domain.HTTPSMirrorAlternative` im Backend: die zentralen Adressen von
 * Ubuntu und Debian. Die Quellenliste braucht ihn ohne frische Prüfung; leer, wenn LCM keinen kennt oder er die Quelle
 * selbst wäre.
 */
export function httpsMirrorAlternative(uri: string): string {
  let parsed: URL;
  try {
    parsed = new URL(uri);
  } catch {
    return '';
  }
  const host = parsed.hostname.toLowerCase();
  const path = parsed.pathname.replace(/\/+$/, '');
  const within = (domain: string) => host === domain || host.endsWith(`.${domain}`);
  let alternative = '';
  if (within('archive.ubuntu.com')) alternative = `https://archive.ubuntu.com${path}`;
  else if (within('ports.ubuntu.com')) alternative = `https://ports.ubuntu.com${path}`;
  else if (host === 'security.ubuntu.com') alternative = `https://security.ubuntu.com${path}`;
  else if (host.endsWith('.debian.org') && host !== 'archive.debian.org') {
    // Nur das Hauptarchiv und die Sicherheitsupdates; debian-ports und abgelaufene Releases liegen woanders.
    const under = (base: string) => path === base || path.startsWith(`${base}/`);
    if (under('/debian-security')) alternative = 'https://security.debian.org/debian-security';
    else if (under('/debian')) alternative = 'https://deb.debian.org/debian';
  }
  return alternative === `https://${uri.slice('http://'.length)}`.replace(/\/+$/, '') ? '' : alternative;
}

// ---- Apps ohne Paket ----

export interface UnknownApp {
  id: string;
  unit: string;
  execPath: string;
  fragmentPath: string;
}

export function normaliseUnknownApps(raw: unknown): UnknownApp[] {
  const r = isRecord(raw) ? raw : {};
  return records(r.unknown).map((u) => ({ id: String(num(u.id) || text(u.unit)), unit: text(u.unit), execPath: text(u.exec_path), fragmentPath: text(u.fragment_path) }));
}

// ---- Ändernde Aufrufe ----

/** Ergebnis der synchronen Server-Aktionen: was die Konsole dabei ausgegeben hat. */
export interface ActionOutput {
  output: string;
}

const output = (raw: unknown): ActionOutput => ({ output: isRecord(raw) ? text(raw.output) : '' });
const postOutput = async (url: string, body?: unknown) => output(await api.post<unknown>(url, body));

function started(raw: unknown): StartedJob {
  const r = isRecord(raw) ? raw : {};
  return { status: text(r.status), job_id: text(r.job_id), job_name: text(r.job_name) };
}

export const packagesApi = {
  versions: async (id: number, name: string) => normaliseVersions(await api.get<unknown>(versionsUrl(id, name))),
  createPin: (id: number, input: PinInput) => api.post(pinsUrl(id), { ...input, name: input.name.trim(), note: input.note.trim() }),
  deletePin: (id: number, pinId: number) => api.delete(`${pinsUrl(id)}/${pinId}`),
  pinKernel: (id: number, global = false) => api.post(`${pinsUrl(id)}/kernel`, { global }),
  applyPins: async (id: number) => started(await api.post<unknown>(`${pinsUrl(id)}/apply`)),
};

export const reposApi = {
  /** Prüft vom Server aus, welche http-Quellen HTTPS sprechen. */
  checkHttps: async (id: number) => {
    const raw = await api.post<unknown>(`${SERVERS_URL}/${id}/repositories/https-check`);
    return { results: normaliseHttpsCheck(raw), missingCertificates: httpsCheckMissingCerts(raw) };
  },
  /** Stellt die gewählten http-Quellen auf HTTPS um; ohne Auswahl alle. */
  secure: (id: number, uris: string[] = []) => postOutput(`${SERVERS_URL}/${id}/repositories/secure`, { uris }),
  revertHttps: (id: number) => postOutput(`${SERVERS_URL}/${id}/repositories/revert-https`, { uris: [] }),
  add: (id: number, key: string) => postOutput(`${SERVERS_URL}/${id}/repositories/add`, { key }),
  aptProxy: (id: number, enable: boolean, cacheId = 0) => postOutput(`${SERVERS_URL}/${id}/apt-proxy`, { enable, cache_id: enable ? cacheId : 0 }),
};
