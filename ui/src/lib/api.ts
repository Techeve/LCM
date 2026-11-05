// API-Client: fetch mit der Browser-Sitzung, JSON und einheitlichen Fehlern. Wie die
// Sitzung getragen wird, bestimmt der Betreiber (Einstellungen → Allgemein):
//  - Arbeitsspeicher (Standard): Der Login liefert `session_token`. Es steht NUR
//    in der Modulvariable `sessionToken` hier - nie in localStorage, sessionStorage,
//    IndexedDB, Cache oder Service Worker - und geht als `X-LCM-Session` mit.
//  - Cookie (nur HTTPS): Die Sitzung liegt im httpOnly-Cookie, das JavaScript nie
//    sieht; ändernde Anfragen tragen zusätzlich `X-CSRF-Token`.
// Daten werden hier nie gespeichert (siehe store.ts).

import { createSseParser, type SseEvent } from './sse';

export type ErrorParams = Record<string, unknown>;

/** Fehlerantwort der API. `code` ist der Übersetzungsschlüssel `errors.<code>`. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly params: ErrorParams;

  constructor(status: number, code: string, params: ErrorParams, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.code = code;
    this.params = params;
  }
}

export interface RequestOptions {
  signal?: AbortSignal;
  /** Aufruf ohne Sitzungserwartung (Anmeldung, /auth/me): ein 401 heißt nicht „abgelaufen“. */
  anonymous?: boolean;
}

export interface ApiResult<T> {
  data: T;
  etag: string | null;
  notModified: boolean;
}

const SESSION_HEADER = 'X-LCM-Session';
const CSRF_COOKIE = '__Host-lcm_csrf';
const CSRF_HEADER = 'X-CSRF-Token';
const CHANGING_METHODS = new Set(['POST', 'PUT', 'PATCH', 'DELETE']);
const ME = '/api/v1/auth/me';
const LOGOUT = '/api/v1/auth/logout';

// Der Sitzungswert der Arbeitsspeicher-Variante. Bewusst nur eine Modulvariable:
// Neuladen, neuer Tab und Schließen lassen ihn verschwinden, das ist gewollt.
let sessionToken: string | null = null;

/** Merkt den Sitzungswert dieses Tabs (`null` nach dem Abmelden oder bei einem 401). */
export function setSessionToken(token: string | null): void {
  sessionToken = token || null;
}

/** Hat dieser Tab einen Sitzungswert? (Arbeitsspeicher-Variante, angemeldet.) */
export function hasSessionToken(): boolean {
  return sessionToken !== null;
}

/**
 * Beendet die Sitzung auf dem Server, wenn der Tab geht (`pagehide`). `keepalive` lässt
 * die Anfrage das Entladen der Seite überleben. Ohne Sitzungswert (Cookie-Variante)
 * geschieht nichts: Das Cookie gehört dem ganzen Browser und bleibt.
 */
export function endSessionOnUnload(): void {
  if (!sessionToken) return;
  void fetch(LOGOUT, {
    method: 'POST',
    headers: { [SESSION_HEADER]: sessionToken },
    credentials: 'same-origin',
    keepalive: true,
  }).catch(() => {});
}

// Das Token, das `/auth/me` zuletzt genannt hat. Es gilt vor dem Cookie: Fehlt das
// Cookie oder ist es veraltet, kennt nur der Server das richtige Token.
let csrfFromServer: string | null = null;

/** Merkt das CSRF-Token der Sitzung (`null` nach dem Abmelden). */
export function setCsrfToken(token: string | null): void {
  csrfFromServer = token || null;
}

const unauthorizedListeners = new Set<() => void>();

/** Meldet jedes 401 einer Anfrage, die eine Sitzung erwartet hat. */
export function onUnauthorized(listener: () => void): () => void {
  unauthorizedListeners.add(listener);
  return () => unauthorizedListeners.delete(listener);
}

/** Das CSRF-Token der Cookie-Sitzung; leer in der Arbeitsspeicher-Variante. */
function csrfToken(): string {
  if (csrfFromServer) return csrfFromServer;
  const cookie = document.cookie.split('; ').find((entry) => entry.startsWith(`${CSRF_COOKIE}=`));
  return cookie ? decodeURIComponent(cookie.slice(CSRF_COOKIE.length + 1)) : '';
}

function isAbort(error: unknown): boolean {
  return error instanceof DOMException && error.name === 'AbortError';
}

function asText(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

async function toApiError(response: Response, options: RequestOptions): Promise<ApiError> {
  const body = await response.json().catch(() => null);
  const record = body && typeof body === 'object' ? (body as Record<string, unknown>) : {};
  const params = record.params && typeof record.params === 'object' ? (record.params as ErrorParams) : {};
  const message = asText(record.error) || response.statusText || `HTTP ${response.status}`;
  if (response.status === 401 && !options.anonymous) unauthorizedListeners.forEach((listener) => listener());
  return new ApiError(response.status, asText(record.code), params, message);
}

function encodeBody(body: unknown, headers: Record<string, string>): BodyInit | undefined {
  if (body === undefined) return undefined;
  if (body instanceof FormData) return body;
  headers['Content-Type'] = 'application/json';
  return JSON.stringify(body);
}

function isCsrfRejection(error: unknown): boolean {
  return error instanceof ApiError && error.status === 403 && error.code === 'auth.csrf_invalid';
}

/** Holt das aktuelle Token von `/auth/me`; scheitert das, bleibt der ursprüngliche Fehler. */
async function refreshCsrfToken(): Promise<boolean> {
  try {
    const { data } = await exchange<{ csrf_token?: string }>('GET', ME, undefined, { anonymous: true });
    setCsrfToken(data.csrf_token ?? null);
    return Boolean(data.csrf_token);
  } catch {
    return false;
  }
}

/** Eine ändernde Anfrage, die am CSRF-Token scheitert, läuft nach dem Auffrischen einmal erneut. */
async function exchange<T>(
  method: string,
  url: string,
  body: unknown,
  options: RequestOptions,
  etag?: string | null,
): Promise<ApiResult<T>> {
  try {
    return await attempt<T>(method, url, body, options, etag);
  } catch (error) {
    if (!CHANGING_METHODS.has(method) || !isCsrfRejection(error) || !(await refreshCsrfToken())) throw error;
    return attempt<T>(method, url, body, options, etag);
  }
}

/** Kopfzeilen der Anmeldung: der Sitzungswert, oder bei ändernden Cookie-Anfragen das CSRF-Token. */
function authHeaders(method: string): Record<string, string> {
  if (sessionToken) return { [SESSION_HEADER]: sessionToken };
  const csrf = CHANGING_METHODS.has(method) ? csrfToken() : '';
  return csrf ? { [CSRF_HEADER]: csrf } : {};
}

async function request(method: string, url: string, headers: Record<string, string>, body: BodyInit | undefined, signal?: AbortSignal): Promise<Response> {
  try {
    // no-store: der HTTP-Cache des Browsers legt keine API-Antworten auf die Platte.
    return await fetch(url, { method, headers, body, credentials: 'same-origin', cache: 'no-store', signal });
  } catch (error) {
    if (isAbort(error)) throw error;
    throw new ApiError(0, 'network', {}, 'network error');
  }
}

async function attempt<T>(
  method: string,
  url: string,
  body: unknown,
  options: RequestOptions,
  etag?: string | null,
): Promise<ApiResult<T>> {
  const headers: Record<string, string> = { Accept: 'application/json', ...authHeaders(method) };
  if (etag) headers['If-None-Match'] = etag;
  const payload = encodeBody(body, headers);

  const response = await request(method, url, headers, payload, options.signal);
  if (response.status === 304) return { data: undefined as T, etag: etag ?? null, notModified: true };
  if (!response.ok) throw await toApiError(response, options);
  const data = response.status === 204 ? undefined : await response.json();
  return { data: data as T, etag: response.headers.get('ETag'), notModified: false };
}

/** Lädt eine Datei mit der Sitzung herunter; ein direkter Link trüge sie nicht mit. */
async function download(url: string, options: RequestOptions = {}): Promise<Blob> {
  const response = await request('GET', url, authHeaders('GET'), undefined, options.signal);
  if (!response.ok) throw await toApiError(response, options);
  return response.blob();
}

/**
 * Liest einen Server-Sent-Events-Strom bis zu seinem Ende. `EventSource` kann keine Kopfzeile setzen
 * und trüge die Sitzung der Arbeitsspeicher-Variante nicht mit.
 */
async function stream(url: string, onEvent: (event: SseEvent) => void, options: RequestOptions = {}): Promise<void> {
  const response = await request('GET', url, { Accept: 'text/event-stream', ...authHeaders('GET') }, undefined, options.signal);
  if (!response.ok) throw await toApiError(response, options);
  if (!response.body) return;
  const parser = createSseParser(onEvent);
  const decoder = new TextDecoder();
  const reader = response.body.getReader();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) return;
      parser.push(decoder.decode(value, { stream: true }));
    }
  } catch (error) {
    if (isAbort(error)) throw error;
    throw new ApiError(0, 'network', {}, 'network error');
  }
}

// Gleiche GET-Anfragen, die gerade laufen, teilen sich eine Antwort. Mit eigenem
// AbortSignal wird nicht zusammengelegt, sonst bräche ein Abbruch fremde Aufrufe ab.
const inflight = new Map<string, Promise<ApiResult<unknown>>>();

function get<T>(url: string, options: RequestOptions, etag?: string | null): Promise<ApiResult<T>> {
  if (options.signal) return exchange<T>('GET', url, undefined, options, etag);
  const key = `${url}\n${etag ?? ''}`;
  let pending = inflight.get(key);
  if (!pending) {
    pending = exchange<unknown>('GET', url, undefined, options, etag).finally(() => inflight.delete(key));
    inflight.set(key, pending);
  }
  return pending as Promise<ApiResult<T>>;
}

async function send<T>(method: string, url: string, body: unknown, options: RequestOptions): Promise<T> {
  return (await exchange<T>(method, url, body, options)).data;
}

export const api = {
  async get<T>(url: string, options: RequestOptions = {}): Promise<T> {
    return (await get<T>(url, options)).data;
  },

  /** GET mit If-None-Match; bei 304 ist `notModified` gesetzt und `data` leer. */
  getWithEtag<T>(url: string, etag: string | null | undefined, options: RequestOptions = {}): Promise<ApiResult<T>> {
    return get<T>(url, options, etag);
  },

  post: <T = void>(url: string, body?: unknown, options: RequestOptions = {}) => send<T>('POST', url, body, options),
  put: <T = void>(url: string, body?: unknown, options: RequestOptions = {}) => send<T>('PUT', url, body, options),
  patch: <T = void>(url: string, body?: unknown, options: RequestOptions = {}) => send<T>('PATCH', url, body, options),
  delete: <T = void>(url: string, options: RequestOptions = {}) => send<T>('DELETE', url, undefined, options),

  download,
  stream,
};
