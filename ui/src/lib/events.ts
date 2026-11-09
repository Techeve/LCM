// Ereigniskanal: je Tab eine WebSocket-Verbindung zu /api/v1/events. Ein
// Ereignis heißt nur „hat sich geändert“, die Daten holt der Tab per REST.
//
// Der Browser kann an einem WebSocket keine Kopfzeile setzen, die Sitzung käme also
// nicht mit. Vor JEDEM Verbindungsaufbau holt der Client deshalb mit seiner Sitzung
// eine Einmal-Fahrkarte (`POST /api/v1/events/ticket`, 30 Sekunden gültig) und
// verbindet mit `?ticket=…`; in beiden Anmeldevarianten dieselbe Logik.
//
// Ein Server, der den Verbindungsaufbau mit 401 ablehnt, erscheint im Browser nur
// als Abbruch mit Code 1006. Nach einem solchen Abbruch fragt der Client deshalb
// `/auth/me`: antwortet es mit 401, gilt die Sitzung als beendet.
//
// Themen werden mit Zählern abonniert: Seiten und Komponenten melden an, was sie
// zeigen, und ab, wenn sie verschwinden. Reißt die Verbindung, wird mit
// Backoff neu verbunden und danach einmal `resync` gemeldet (verpasste
// Ereignisse). Ist der Tab 5 Minuten verborgen, wird die Verbindung geschlossen.
// Bei 4429 (zu viele Verbindungen), 4403 oder wiederholten Fehlern gilt statt
// dessen langsames Abfragen: alle 60 Sekunden `resync`.

import { api, ApiError } from './api';

export interface EventMessage {
  type: 'event';
  event: string;
  topic: string;
  data: unknown;
}

export const HIDDEN_CLOSE_MS = 5 * 60_000;
export const MAX_BACKOFF_MS = 30_000;
export const POLL_INTERVAL_MS = 60_000;
export const POLL_AFTER_FAILURES = 8;

const CLOSE_AUTH_LOST = 4401;
const CLOSE_FORBIDDEN = 4403;
const CLOSE_TOO_MANY = 4429;

const CLOSE_ABNORMAL = 1006;

type Connect = (url: string) => WebSocket;

const TICKET_URL = '/api/v1/events/ticket';

function socketUrl(ticket: string): string {
  const scheme = location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${scheme}//${location.host}/api/v1/events?ticket=${encodeURIComponent(ticket)}`;
}

/** Eine frische Fahrkarte; `anonymous`, weil ein 401 hier der Client selbst behandelt. */
async function fetchTicket(): Promise<string> {
  const { ticket } = await api.post<{ ticket: string }>(TICKET_URL, undefined, { anonymous: true });
  return ticket;
}

/** Schließcode, der einem gescheiterten Fahrkarten-Abruf entspricht. */
function codeForTicketError(error: unknown): number {
  if (error instanceof ApiError && error.status === 401) return CLOSE_AUTH_LOST;
  if (error instanceof ApiError && error.status === 403) return CLOSE_FORBIDDEN;
  return CLOSE_ABNORMAL;
}

function parse(raw: unknown): Record<string, unknown> | null {
  try {
    const message = JSON.parse(String(raw));
    return message && typeof message === 'object' ? message : null;
  } catch {
    return null;
  }
}

export class EventsClient {
  #connect: Connect;
  #getTicket: () => Promise<string>;
  #topics = new Map<string, number>();
  #socket: WebSocket | null = null;
  #connected = false;
  #running = false;
  #suspended = false;
  // Zählt Start und Stopp: Eine Fahrkarte, die nach einem Stopp ankommt, baut keine Verbindung mehr auf.
  #epoch = 0;
  #asking = false;
  #failures = 0;
  #resyncPending = false;
  #retryTimer: ReturnType<typeof setTimeout> | undefined;
  #hideTimer: ReturnType<typeof setTimeout> | undefined;
  #pollTimer: ReturnType<typeof setInterval> | undefined;
  #eventListeners = new Set<(message: EventMessage) => void>();
  #resyncListeners = new Set<() => void>();
  #authLostListeners = new Set<() => void>();

  constructor(connect: Connect = (url) => new WebSocket(url), getTicket: () => Promise<string> = fetchTicket) {
    this.#connect = connect;
    this.#getTicket = getTicket;
  }

  /** Verbindet (nach der Anmeldung) und hält die Verbindung, solange der Tab sichtbar ist. */
  start(): void {
    if (this.#running) return;
    this.#running = true;
    document.addEventListener('visibilitychange', this.#onVisibility);
    if (document.visibilityState === 'hidden') this.#scheduleHide();
    this.#open();
  }

  /** Trennt und vergisst Fehlerstand und Timer (Abmelden). Abonnements bleiben. */
  stop(): void {
    this.#running = false;
    this.#epoch += 1;
    this.#asking = false;
    this.#suspended = false;
    this.#failures = 0;
    this.#resyncPending = false;
    document.removeEventListener('visibilitychange', this.#onVisibility);
    this.#clearTimers();
    this.#closeSocket();
  }

  /** Abonniert Themen; der Rückgabewert hebt genau dieses Abonnement wieder auf. */
  subscribe(topics: string[]): () => void {
    const added = topics.filter((topic) => this.#acquire(topic));
    if (added.length > 0) this.#send('subscribe', added);
    let released = false;
    return () => {
      if (released) return;
      released = true;
      const removed = topics.filter((topic) => this.#release(topic));
      if (removed.length > 0) this.#send('unsubscribe', removed);
    };
  }

  onEvent(listener: (message: EventMessage) => void): () => void {
    this.#eventListeners.add(listener);
    return () => this.#eventListeners.delete(listener);
  }

  /** Sichtbare Daten sollen einmal neu geladen werden. */
  onResync(listener: () => void): () => void {
    this.#resyncListeners.add(listener);
    return () => this.#resyncListeners.delete(listener);
  }

  /** Der Server hat die Sitzung beendet (4401). */
  onAuthLost(listener: () => void): () => void {
    this.#authLostListeners.add(listener);
    return () => this.#authLostListeners.delete(listener);
  }

  #acquire(topic: string): boolean {
    const count = (this.#topics.get(topic) ?? 0) + 1;
    this.#topics.set(topic, count);
    return count === 1;
  }

  #release(topic: string): boolean {
    const count = (this.#topics.get(topic) ?? 0) - 1;
    if (count > 0) {
      this.#topics.set(topic, count);
      return false;
    }
    this.#topics.delete(topic);
    return count === 0;
  }

  #send(type: 'subscribe' | 'unsubscribe', topics: string[]): void {
    if (this.#connected) this.#socket?.send(JSON.stringify({ type, topics }));
  }

  #open(): void {
    if (this.#socket || this.#asking || !this.#running || this.#suspended) return;
    void this.#connectWithTicket();
  }

  async #connectWithTicket(): Promise<void> {
    const epoch = this.#epoch;
    this.#asking = true;
    let ticket: string;
    try {
      ticket = await this.#getTicket();
    } catch (error) {
      if (epoch !== this.#epoch) return;
      this.#asking = false;
      this.#onEnd(codeForTicketError(error));
      return;
    }
    if (epoch !== this.#epoch || this.#suspended) return;
    this.#asking = false;
    const socket = this.#connect(socketUrl(ticket));
    this.#socket = socket;
    socket.onopen = () => this.#onOpen();
    socket.onmessage = (event) => this.#onMessage(event.data);
    socket.onclose = (event) => this.#onClose(event.code);
  }

  #onOpen(): void {
    this.#connected = true;
    this.#failures = 0;
    this.#stopPolling();
    if (this.#topics.size > 0) this.#send('subscribe', [...this.#topics.keys()]);
    if (this.#resyncPending) {
      this.#resyncPending = false;
      this.#emitResync();
    }
  }

  #onMessage(raw: unknown): void {
    const message = parse(raw);
    if (message?.type === 'resync') this.#emitResync();
    if (message?.type === 'event' && typeof message.event === 'string' && typeof message.topic === 'string') {
      this.#eventListeners.forEach((listener) => listener(message as unknown as EventMessage));
    }
  }

  #onClose(code: number): void {
    this.#socket = null;
    this.#connected = false;
    this.#onEnd(code);
  }

  /** Eine Verbindung ist zu Ende oder gar nicht erst zustande gekommen. */
  #onEnd(code: number): void {
    if (code === CLOSE_AUTH_LOST) {
      this.#loseAuth();
      return;
    }
    if (code === CLOSE_ABNORMAL) void this.#probeSession();
    this.#resyncPending = true;
    this.#failures += 1;
    if (code === CLOSE_TOO_MANY || code === CLOSE_FORBIDDEN || this.#failures >= POLL_AFTER_FAILURES) {
      this.#startPolling();
    } else {
      this.#scheduleRetry();
    }
  }

  #loseAuth(): void {
    this.stop();
    this.#authLostListeners.forEach((listener) => listener());
  }

  /** Nach einem Abbruch ohne Code: Ist die Sitzung noch gültig? Nur ein 401 zählt, ein Netzfehler nicht. */
  async #probeSession(): Promise<void> {
    try {
      await api.get('/api/v1/auth/me', { anonymous: true });
    } catch (error) {
      if (error instanceof ApiError && error.status === 401 && this.#running) this.#loseAuth();
    }
  }

  #scheduleRetry(): void {
    const backoff = Math.min(MAX_BACKOFF_MS, 1000 * 2 ** (this.#failures - 1));
    const delay = backoff * (0.5 + Math.random() / 2); // Jitter: nicht alle Tabs zugleich
    this.#retryTimer = setTimeout(() => {
      this.#retryTimer = undefined;
      this.#open();
    }, delay);
  }

  #startPolling(): void {
    if (this.#pollTimer) return;
    this.#pollTimer = setInterval(() => {
      this.#emitResync();
      this.#open(); // vielleicht geht der Kanal inzwischen wieder
    }, POLL_INTERVAL_MS);
  }

  #stopPolling(): void {
    clearInterval(this.#pollTimer);
    this.#pollTimer = undefined;
  }

  #emitResync(): void {
    this.#resyncListeners.forEach((listener) => listener());
  }

  #onVisibility = (): void => {
    if (document.visibilityState === 'hidden') {
      this.#scheduleHide();
      return;
    }
    clearTimeout(this.#hideTimer);
    this.#hideTimer = undefined;
    if (!this.#suspended) return;
    this.#suspended = false;
    this.#failures = 0;
    this.#open(); // `resyncPending` sorgt nach dem Verbinden für frische Daten
  };

  #scheduleHide(): void {
    clearTimeout(this.#hideTimer);
    this.#hideTimer = setTimeout(() => this.#suspend(), HIDDEN_CLOSE_MS);
  }

  /** Verborgener Tab: Verbindung und Timer ruhen, bis der Tab wieder sichtbar ist. */
  #suspend(): void {
    this.#hideTimer = undefined;
    this.#suspended = true;
    this.#resyncPending = true;
    this.#epoch += 1;
    this.#asking = false;
    this.#clearTimers();
    this.#closeSocket();
  }

  #clearTimers(): void {
    clearTimeout(this.#retryTimer);
    clearTimeout(this.#hideTimer);
    this.#retryTimer = undefined;
    this.#hideTimer = undefined;
    this.#stopPolling();
  }

  #closeSocket(): void {
    const socket = this.#socket;
    this.#socket = null;
    this.#connected = false;
    if (!socket) return;
    socket.onopen = socket.onmessage = socket.onclose = null;
    socket.close(1000);
  }
}

export const events = new EventsClient();
