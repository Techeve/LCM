// Daten einer Seite: aus dem Datenspeicher des Tabs, im Hintergrund aufgefrischt,
// und bei Ereignissen der abonnierten Themen neu geladen (Zuordnung in `live.ts`).
// Ändert sich die Adresse (Filter, Seite), wechselt das Abonnement von selbst.

import type { ReactiveController, ReactiveControllerHost } from 'lit';
import { errorText } from './i18n';
import { events } from './events';
import { store } from './store';

export interface LiveDataOptions<T> {
  /** Adresse der Daten; `null`, solange es nichts zu laden gibt. Wird bei jedem Rendern neu gelesen. */
  url: () => string | null;
  /** Ereignisthemen, die diese Daten betreffen (`['dashboard']`, `['job:<id>']`). */
  topics?: () => string[];
  /** Macht aus der Antwort der API die Form, mit der die Seite arbeitet. */
  transform?: (raw: never) => T;
}

export class LiveData<T> implements ReactiveController {
  data: T | undefined;
  /** Text des letzten Fehlers; leer, solange alles in Ordnung ist. Vorhandene Daten bleiben stehen. */
  error = '';
  /**
   * Es wird geladen. Wechselt die Adresse (anderer Filter), bleiben die bisherigen Daten stehen,
   * bis die neuen da sind: `data === undefined` heißt „noch nichts“, sonst „wird aufgefrischt“.
   */
  loading = true;

  #host: ReactiveControllerHost;
  #options: LiveDataOptions<T>;
  #url: string | null = null;
  #dataUrl: string | null = null;
  #topics = '';
  #stopStore: (() => void) | undefined;
  #stopEvents: (() => void) | undefined;
  #connected = false;

  constructor(host: ReactiveControllerHost, options: LiveDataOptions<T>) {
    this.#host = host;
    this.#options = options;
    host.addController(this);
  }

  hostConnected(): void {
    this.#connected = true;
    this.#sync();
  }

  hostUpdate(): void {
    if (this.#connected) this.#sync();
  }

  hostDisconnected(): void {
    this.#connected = false;
    this.#stopStore?.();
    this.#stopEvents?.();
    this.#stopStore = this.#stopEvents = undefined;
    this.#url = null;
    this.#topics = '';
  }

  /** Lädt die aktuelle Adresse unabhängig von Ereignissen neu (z. B. nach einer Änderung). */
  reload(): void {
    if (this.#url) store.refresh(this.#url).catch(() => {});
  }

  #sync(): void {
    this.#syncTopics();
    const url = this.#options.url();
    if (url === this.#url) return;
    this.#stopStore?.();
    this.#stopStore = undefined;
    this.#url = url;
    this.error = '';
    if (!url) {
      this.data = undefined;
      this.#dataUrl = null;
      this.loading = false;
      return;
    }
    const cached = store.peek<never>(url);
    if (cached !== undefined) {
      this.#adopt(url, cached);
      // Mitten in einem Rendern (hostUpdate) hat willUpdate des Elements schon gelaufen; ohne neues Rendern
      // blieben davon abgeleitete Zustände (etwa ein Formular beim erneuten Öffnen) leer.
      queueMicrotask(() => this.#host.requestUpdate());
    }
    this.loading = cached === undefined;
    this.#stopStore = store.subscribe<never>(
      url,
      (raw) => this.#receive(raw),
      (error) => this.#fail(error),
    );
    store.get<never>(url).catch(() => {}); // Fehler kommen über das Abonnement
  }

  #syncTopics(): void {
    const topics = this.#options.topics?.() ?? [];
    const key = topics.join('|');
    if (key === this.#topics) return;
    this.#stopEvents?.();
    this.#topics = key;
    this.#stopEvents = topics.length > 0 ? events.subscribe(topics) : undefined;
  }

  #convert(raw: never): T {
    return this.#options.transform ? this.#options.transform(raw) : (raw as unknown as T);
  }

  #adopt(url: string, raw: never): void {
    this.data = this.#convert(raw);
    this.#dataUrl = url;
  }

  #receive(raw: never): void {
    if (this.#url) this.#adopt(this.#url, raw);
    this.error = '';
    this.loading = false;
    this.#host.requestUpdate();
  }

  #fail(error: unknown): void {
    if (this.#dataUrl !== this.#url) this.data = undefined; // Daten eines anderen Filters nicht als aktuelle zeigen
    this.error = errorText(error);
    this.loading = false;
    this.#host.requestUpdate();
  }
}
