// Datenspeicher dieses Tabs, nur im Arbeitsspeicher (nie localStorage, IndexedDB
// oder Cache Storage). `get` zeigt Vorhandenes sofort und frischt im Hintergrund
// auf (stale-while-revalidate); Abonnenten erfahren von neuen Daten.

import { api } from './api';

type DataListener = (data: never) => void;
type ErrorListener = (error: unknown) => void;

interface Subscription {
  onData: DataListener;
  onError?: ErrorListener;
}

interface Entry {
  data: unknown;
  etag: string | null;
}

export interface InvalidateOptions {
  /** Auch Unterpfade treffen (`/servers/42/packages` bei `/servers/42`). Standard: ja. */
  withChildren?: boolean;
}

/** Trifft die URL (ohne Query) genau oder, mit Unterpfaden, am Pfadende. */
export function urlMatches(url: string, prefix: string, withChildren: boolean): boolean {
  const path = url.split('?')[0];
  return path === prefix || (withChildren && path.startsWith(`${prefix}/`));
}

export class Store {
  #entries = new Map<string, Entry>();
  #subscriptions = new Map<string, Set<Subscription>>();
  #refreshing = new Map<string, Promise<unknown>>();
  #again = new Set<string>();
  #epoch = 0;

  peek<T>(url: string): T | undefined {
    return this.#entries.get(url)?.data as T | undefined;
  }

  /** Vorhandene Daten sofort, dazu eine Auffrischung im Hintergrund; sonst Laden. */
  get<T>(url: string): Promise<T> {
    const entry = this.#entries.get(url);
    if (!entry) return this.refresh<T>(url);
    this.refresh(url).catch(() => {}); // Fehler gehen an die Abonnenten
    return Promise.resolve(entry.data as T);
  }

  /** Lädt neu. Kommt währenddessen ein neuer Anlass, läuft danach ein weiterer Abruf. */
  refresh<T>(url: string): Promise<T> {
    const running = this.#refreshing.get(url);
    if (running) {
      this.#again.add(url);
      return running as Promise<T>;
    }
    const promise: Promise<T> = this.#load<T>(url).finally(() => this.#finished(url, promise));
    this.#refreshing.set(url, promise);
    return promise;
  }

  subscribe<T>(url: string, onData: (data: T) => void, onError?: ErrorListener): () => void {
    const subscription: Subscription = { onData: onData as DataListener, onError };
    const set = this.#subscriptions.get(url) ?? new Set();
    set.add(subscription);
    this.#subscriptions.set(url, set);
    return () => {
      set.delete(subscription);
      if (set.size === 0) this.#subscriptions.delete(url);
    };
  }

  /** Lädt alle abonnierten URLs unter dem Präfix neu (nach Ereignissen oder Änderungen). */
  invalidate(prefix: string, { withChildren = true }: InvalidateOptions = {}): void {
    for (const url of this.#subscriptions.keys()) {
      if (urlMatches(url, prefix, withChildren)) this.refresh(url).catch(() => {});
    }
  }

  /** Nach Verbindungsabbruch oder Tab-Wechsel: alles Sichtbare einmal neu laden. */
  refreshSubscribed(): void {
    for (const url of this.#subscriptions.keys()) this.refresh(url).catch(() => {});
  }

  /** Abmelden: alles vergessen. Laufende Abrufe werden verworfen. */
  clear(): void {
    this.#epoch += 1;
    this.#entries.clear();
    this.#refreshing.clear();
    this.#again.clear();
  }

  async #load<T>(url: string): Promise<T> {
    const epoch = this.#epoch;
    const entry = this.#entries.get(url);
    try {
      const result = await api.getWithEtag<T>(url, entry?.etag);
      if (epoch !== this.#epoch) throw new DOMException('store cleared', 'AbortError');
      if (result.notModified && entry) return entry.data as T;
      this.#entries.set(url, { data: result.data, etag: result.etag });
      this.#subscriptions.get(url)?.forEach((s) => s.onData(result.data as never));
      return result.data;
    } catch (error) {
      if (epoch === this.#epoch) this.#subscriptions.get(url)?.forEach((s) => s.onError?.(error));
      throw error;
    }
  }

  #finished(url: string, promise: Promise<unknown>): void {
    if (this.#refreshing.get(url) !== promise) return; // zwischenzeitlich geleert
    this.#refreshing.delete(url);
    if (this.#again.delete(url)) this.refresh(url).catch(() => {});
  }
}

export const store = new Store();
