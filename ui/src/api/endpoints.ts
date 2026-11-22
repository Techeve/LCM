// Endpunkte von LCM (Weboberfläche, Agent, MCP): Zertifikate, erlaubte Netzbereiche und Zertifikate von einer
// ACME-CA (Let's Encrypt). Die Antworten werden hier in die Form gebracht, mit der die Seiten arbeiten; die
// ändernden Aufrufe laden die betroffenen Adressen danach neu.

import { api } from '../lib/api';
import { store } from '../lib/store';

export const TLS_URL = '/api/v1/system/tls';
export const NETWORK_URL = '/api/v1/system/network';
export const ACME_URL = '/api/v1/system/acme';

type Raw = Record<string, unknown>;

function isRecord(value: unknown): value is Raw {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

const text = (value: unknown): string => (typeof value === 'string' ? value : '');
const flag = (value: unknown): boolean => value === true;
const list = (value: unknown): Raw[] => (Array.isArray(value) ? value.filter(isRecord) : []);
const strings = (value: unknown): string[] => (Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []);

export type EndpointName = 'ui' | 'agent' | 'mcp';
export const ENDPOINTS: readonly EndpointName[] = ['ui', 'agent', 'mcp'];

export function isEndpointName(value: unknown): value is EndpointName {
  return value === 'ui' || value === 'agent' || value === 'mcp';
}

// ---- Zertifikate ----

export type CertSource = 'config' | 'upload' | 'acme' | 'lcm-ca';

export interface Certificate {
  id: string;
  source: CertSource;
  names: string[];
  issuer: string;
  /** ISO-Zeit des Ablaufs. */
  notAfter: string;
  expired: boolean;
  /** SHA-256 des Zertifikats, hex. */
  fingerprint: string;
  /** Hinweise des Servers (deutscher Klartext), die das Laden nicht verhindern. */
  warnings: string[];
}

/** Das Zertifikat, das ein Endpunkt für den Namen seiner öffentlichen Adresse ausliefert. */
export interface EndpointCertificate {
  name: EndpointName;
  /** Leer, solange keine öffentliche Adresse eingetragen ist. */
  url: string;
  host: string;
  /** `null` ohne Adresse: Dann gilt das LCM-Zertifikat. */
  cert: Certificate | null;
}

export interface SharedPortRoute {
  service: EndpointName;
  host: string;
  inactive: boolean;
  shadowedBy: string;
}

export interface SharedPort {
  address: string;
  listening: boolean;
  routes: SharedPortRoute[];
}

export interface TlsStatus {
  certificates: Certificate[];
  endpoints: EndpointCertificate[];
  /** `null`, solange kein gemeinsamer Port eingerichtet ist. */
  sharedPort: SharedPort | null;
}

function readSource(value: unknown): CertSource {
  return value === 'config' || value === 'upload' || value === 'acme' ? value : 'lcm-ca';
}

export function readCertificate(raw: unknown): Certificate {
  const r = isRecord(raw) ? raw : {};
  return {
    id: text(r.id),
    source: readSource(r.source),
    names: strings(r.names),
    issuer: text(r.issuer),
    notAfter: text(r.not_after),
    expired: flag(r.expired),
    fingerprint: text(r.fingerprint),
    warnings: strings(r.warnings),
  };
}

function readEndpointCertificate(r: Raw): EndpointCertificate | null {
  if (!isEndpointName(r.name)) return null;
  return { name: r.name, url: text(r.url), host: text(r.host), cert: isRecord(r.cert) ? readCertificate(r.cert) : null };
}

function readSharedPort(raw: unknown): SharedPort | null {
  if (!isRecord(raw)) return null;
  const routes = list(raw.routes).flatMap((r) =>
    isEndpointName(r.service)
      ? [{ service: r.service, host: text(r.host), inactive: flag(r.inactive), shadowedBy: text(r.shadowed_by) }]
      : [],
  );
  return { address: text(raw.address), listening: flag(raw.listening), routes };
}

export function readTlsStatus(raw: unknown): TlsStatus {
  const r = isRecord(raw) ? raw : {};
  return {
    certificates: list(r.certificates).map(readCertificate),
    endpoints: list(r.endpoints).flatMap((item) => readEndpointCertificate(item) ?? []),
    sharedPort: readSharedPort(r.shared_port),
  };
}

/** Der Teil der Antworten von Hochladen, Entfernen und Neuladen, der den neuen Stand trägt. */
function readStatusOf(raw: unknown): TlsStatus {
  return readTlsStatus(isRecord(raw) ? raw.status : undefined);
}

export interface UploadResult {
  /** ID des neuen Zertifikats im Pool. */
  id: string;
  status: TlsStatus;
}

// ---- Netzbereiche ----

export interface NetworkEndpoint {
  endpoint: EndpointName;
  /** Leer heißt: alle Netze erlaubt. */
  allowed: string[];
}

export interface NetworkState {
  /** Die Adresse des Aufrufers, wie LCM sie sieht. */
  clientAddress: string;
  endpoints: NetworkEndpoint[];
}

export interface AgentPeer {
  id: string;
  name: string;
  address: string;
}

export function readNetworkEndpoint(raw: unknown): NetworkEndpoint | null {
  const r = isRecord(raw) ? raw : {};
  if (!isEndpointName(r.endpoint)) return null;
  return { endpoint: r.endpoint, allowed: strings(r.allowed) };
}

export function readNetworkState(raw: unknown): NetworkState {
  const r = isRecord(raw) ? raw : {};
  return { clientAddress: text(r.client_address), endpoints: list(r.endpoints).flatMap((item) => readNetworkEndpoint(item) ?? []) };
}

export function readAgentPeers(raw: unknown): AgentPeer[] {
  return list(raw).map((r) => ({ id: text(r.id), name: text(r.name), address: text(r.address) }));
}

export interface NetworkUpdate {
  state: NetworkEndpoint | null;
  /** Agenten, die LCM wegen der engeren Liste getrennt hat. */
  disconnected: AgentPeer[];
}

// ---- ACME ----

export interface AcmeCertificate {
  id: string;
  names: string[];
  issuer: string;
  notAfter: string;
  fingerprint: string;
  /** Ab hier erneuert LCM das Zertifikat. */
  renewAt: string;
}

export type AcmeChallenge = 'http-01' | 'dns-01';

export type DnsFieldType = 'text' | 'secret' | 'bool' | 'select' | 'number';

/** Ein Eingabefeld eines DNS-Anbieters; die API liefert keine Beschriftungen, die kommen aus den Sprachdateien. */
export interface DnsField {
  name: string;
  type: DnsFieldType;
  required: boolean;
  options: string[];
  /** Was LCM bei leerem Feld nimmt (nur zur Anzeige). */
  default: string;
}

export interface DnsProvider {
  id: string;
  fields: DnsField[];
}

/** Gespeicherte Werte ohne Geheimnisse; Zahlen und Schalter kommen als solche. */
export type DnsValues = Record<string, string | number | boolean>;

/** Einmaliger Hinweis beim Umstieg von einem eigenen Namensfeld auf die Namen aus den Adressen. */
export interface AcmeNamesNotice {
  /** Die Namen, für die ältere Versionen ausstellten. */
  old: string[];
  /** Die Namen aus den Adressen, für die jetzt ausgestellt wird. */
  new: string[];
  /** Bisher gab es je Name ein Zertifikat; jetzt gibt es eines für alle. */
  wasPerName: boolean;
}

export interface AcmeState {
  enabled: boolean;
  directoryUrl: string;
  email: string;
  names: string[];
  mode: 'single' | 'per_name';
  challenge: AcmeChallenge;
  /** Die Nutzungsbedingungen sind für die gespeicherte CA angenommen. */
  termsAccepted: boolean;
  /** Der Hinweis zum Umstieg, solange er nicht bestätigt ist. */
  namesChangedNotice: AcmeNamesNotice | null;
  /** Adresse des Listeners für HTTP-01 (acme_http_listen der config.json). Er ist nur während einer Prüfung offen. */
  httpListen: string;
  listenerRunning: boolean;
  /** Der Grund, wenn sich der Listener bei der letzten Prüfung nicht öffnen ließ. */
  listenerError: string;
  running: boolean;
  lastRunAt: string | null;
  lastError: string;
  certificates: AcmeCertificate[];
  /** Gewählter DNS-Anbieter, leer solange keiner gewählt ist. */
  dnsProvider: string;
  dnsValues: DnsValues;
  /** Je Geheimfeld, ob ein Wert gespeichert ist; den Wert selbst liefert die API nie. */
  dnsSecretsSet: Record<string, boolean>;
  dnsCheckNameservers: string[];
  /** Die Anbieter, die sich wählen lassen (exec nur, wenn die config.json ein Programm festlegt). */
  dnsProviders: DnsProvider[];
}

function readAcmeCertificate(r: Raw): AcmeCertificate {
  return {
    id: text(r.id),
    names: strings(r.names),
    issuer: text(r.issuer),
    notAfter: text(r.not_after),
    fingerprint: text(r.fingerprint),
    renewAt: text(r.renew_at),
  };
}

const FIELD_TYPES: readonly DnsFieldType[] = ['text', 'secret', 'bool', 'select', 'number'];

function readDnsField(r: Raw): DnsField {
  const type = FIELD_TYPES.find((known) => known === r.type) ?? 'text';
  return { name: text(r.name), type, required: flag(r.required), options: strings(r.options), default: text(r.default) };
}

function readDnsProvider(r: Raw): DnsProvider {
  return { id: text(r.id), fields: list(r.fields).map(readDnsField) };
}

function readDnsValues(value: unknown): DnsValues {
  const entries = isRecord(value) ? Object.entries(value) : [];
  return Object.fromEntries(entries.filter((entry): entry is [string, string | number | boolean] => ['string', 'number', 'boolean'].includes(typeof entry[1])));
}

function readFlags(value: unknown): Record<string, boolean> {
  return Object.fromEntries((isRecord(value) ? Object.entries(value) : []).map(([name, set]) => [name, set === true]));
}

function readNamesNotice(value: unknown): AcmeNamesNotice | null {
  if (!isRecord(value)) return null;
  return { old: strings(value.old), new: strings(value.new), wasPerName: flag(value.was_per_name) };
}

export function readAcmeState(raw: unknown): AcmeState {
  const r = isRecord(raw) ? raw : {};
  return {
    enabled: flag(r.enabled),
    directoryUrl: text(r.directory_url),
    email: text(r.email),
    names: strings(r.names),
    mode: r.mode === 'per_name' ? 'per_name' : 'single',
    challenge: r.challenge === 'dns-01' ? 'dns-01' : 'http-01',
    termsAccepted: flag(r.terms_accepted),
    namesChangedNotice: readNamesNotice(r.names_changed_notice),
    httpListen: text(r.http_listen),
    listenerRunning: flag(r.listener_running),
    listenerError: text(r.listener_error),
    running: flag(r.running),
    lastRunAt: text(r.last_run_at) || null,
    lastError: text(r.last_error),
    certificates: list(r.certificates).map(readAcmeCertificate),
    dnsProvider: text(r.dns_provider),
    dnsValues: readDnsValues(r.dns_values),
    dnsSecretsSet: readFlags(r.dns_secrets_set),
    dnsCheckNameservers: strings(r.dns_check_nameservers),
    dnsProviders: list(r.dns_providers).map(readDnsProvider),
  };
}

/**
 * Die Angaben zu DNS-01 in einer Anfrage. Ein Geheimfeld, das in `dns_values` fehlt, behält seinen gespeicherten
 * Wert; ein leerer Text löscht ihn.
 */
export interface DnsBody {
  dns_provider: string;
  dns_values: Record<string, string | number | boolean>;
  dns_check_nameservers: string[];
}

export interface AcmeBody extends Partial<DnsBody> {
  enabled: boolean;
  directory_url: string;
  email: string;
  names: string[];
  mode: 'single' | 'per_name';
  challenge: AcmeChallenge;
  accept_terms: boolean;
}

/** Probe des DNS-Anbieters; ohne `names` gelten die gespeicherten Namen. */
export interface DnsTestBody extends DnsBody {
  names?: string[];
}

// ---- ändernde Aufrufe ----

const network = (endpoint: EndpointName) => `${NETWORK_URL}/${endpoint}`;

export const endpointsApi = {
  async uploadCertificate(certificate: File, key: File): Promise<UploadResult> {
    const form = new FormData();
    form.append('certificate', certificate);
    form.append('key', key);
    const raw = await api.post<unknown>(`${TLS_URL}/certificates`, form);
    store.invalidate(TLS_URL);
    return { id: isRecord(raw) ? text(raw.id) : '', status: readStatusOf(raw) };
  },

  async removeCertificate(id: string): Promise<TlsStatus> {
    const status = readStatusOf(await api.delete<unknown>(`${TLS_URL}/certificates/${encodeURIComponent(id)}`));
    store.invalidate(TLS_URL);
    return status;
  },

  getTls: () => api.get<unknown>(TLS_URL).then(readTlsStatus),

  /**
   * Die Zertifikatskette als PEM, nie der private Schlüssel. `id` ist die ID im Pool wie in `GET /system/tls`
   * (`acme-<id>`, `upload-<id>`). Die Datei kommt mit der Sitzung, ein Link trüge sie nicht mit.
   */
  downloadCertificate: (id: string): Promise<Blob> => api.download(`${TLS_URL}/certificates/${encodeURIComponent(id)}/download`),

  getNetwork: () => api.get<unknown>(NETWORK_URL).then(readNetworkState),

  async updateNetwork(endpoint: EndpointName, allowed: string[], confirm: boolean): Promise<NetworkUpdate> {
    const raw = await api.put<unknown>(network(endpoint), { allowed, confirm });
    store.invalidate(NETWORK_URL);
    return { state: readNetworkEndpoint(raw), disconnected: readAgentPeers(isRecord(raw) ? raw.disconnected : undefined) };
  },

  async updateAcme(body: AcmeBody): Promise<AcmeState> {
    const state = readAcmeState(await api.put<unknown>(ACME_URL, body));
    store.invalidate(ACME_URL);
    store.invalidate(TLS_URL);
    return state;
  },

  /** Bestätigt den Hinweis zum Umstieg auf die Namen aus den Adressen; er kommt danach nicht wieder. */
  async ackAcmeNamesNotice(): Promise<AcmeState> {
    const state = readAcmeState(await api.post<unknown>(`${ACME_URL}/names-notice/ack`));
    store.invalidate(ACME_URL);
    return state;
  },

  /** Setzt einen TXT-Eintrag über den Anbieter und räumt ihn wieder auf; dauert bis zu einer Minute. */
  async testAcmeDns(body: DnsTestBody): Promise<void> {
    await api.post(`${ACME_URL}/dns-test`, body);
  },

  /** Stößt einen Lauf im Hintergrund an (202); der Zustand steht danach in `GET /system/acme`. */
  async issueAcme(): Promise<void> {
    await api.post(`${ACME_URL}/issue`);
    store.invalidate(ACME_URL);
  },
};
