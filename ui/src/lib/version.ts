// Neue Version: Der Service Worker meldet sie per BroadcastChannel. Jeder Tab
// lädt dann neu, sobald nichts Ungespeichertes offen ist. Sonst bleibt ein
// Hinweis stehen (`pending`), und neu geladen wird nach dem Speichern. So
// spricht nie ein altes Frontend dauerhaft mit einem neuen Backend.
//
// Neu geladen wird nicht kommentarlos: Ein paar Sekunden steht vorher die Ansage
// „wird neu geladen“ (`reloading`), sonst verschwindet der Bildschirm ohne Grund.

import { onBroadcast } from './broadcast';
import { unsaved } from './unsaved';

export const RELOAD_DELAY_MS = 3000;

export class VersionWatcher {
  #reload: () => void;
  #delay: number;
  #pending = false;
  #reloading = false;
  #timer: ReturnType<typeof setTimeout> | undefined;
  #listeners = new Set<(pending: boolean) => void>();

  constructor(reload: () => void = () => location.reload(), delay = RELOAD_DELAY_MS) {
    this.#reload = reload;
    this.#delay = delay;
  }

  /** Neue Version da, aber noch Ungespeichertes offen. */
  get pending(): boolean {
    return this.#pending;
  }

  /** Die Seite lädt gleich neu. */
  get reloading(): boolean {
    return this.#reloading;
  }

  start(): () => void {
    const stopBroadcast = onBroadcast((message) => {
      if (message.type === 'new-version') this.#apply();
    });
    const stopUnsaved = unsaved.subscribe(() => {
      if (this.#pending) this.#apply();
    });
    return () => {
      stopBroadcast();
      stopUnsaved();
      clearTimeout(this.#timer);
    };
  }

  /** Sofort neu laden (Knopf in der Ansage). */
  reloadNow(): void {
    clearTimeout(this.#timer);
    this.#reload();
  }

  /** Hinweis oder Ansage sollen erscheinen oder verschwinden. */
  onPending(listener: (pending: boolean) => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  #apply(): void {
    if (this.#reloading) return;
    if (unsaved.has()) {
      this.#setPending(true);
      return;
    }
    this.#pending = false;
    this.#reloading = true;
    this.#notify();
    this.#timer = setTimeout(() => this.#reload(), this.#delay);
  }

  #setPending(pending: boolean): void {
    if (pending === this.#pending) return;
    this.#pending = pending;
    this.#notify();
  }

  #notify(): void {
    this.#listeners.forEach((listener) => listener(this.#pending));
  }
}

export const versionWatcher = new VersionWatcher();
