// Tastenkürzel: `g` und dann ein Buchstabe springt zu einer Seite, `?` öffnet die
// Hilfe, Strg/Cmd+S meldet „speichern“. In Eingabefeldern gehören Buchstaben dem
// Benutzer, dort gilt nur Speichern. Strg/Cmd+K gehört dem Hauptmenü.
//
// Seitenkürzel mit einer Taste: `/` setzt den Fokus ins Suchfeld der Seite, und
// jeder Befehl der Befehlspalette mit `key` (`n` = neu, `u` = Sammel-Update …)
// läuft auch über seine Taste. Pfeil links/rechts wechselt in Menüleisten und
// zwischen den Bedienelementen einer Tabellenzeile (`focus-nav.ts`).

import { arrowTarget } from './focus-nav';
import { isAvailable, PAGES, type Can, type PageEntry } from './navigation';

export const SEQUENCE_MS = 1500;
export const SAVE_EVENT = 'lcm-save';

const TYPING_INPUTS = new Set(['checkbox', 'radio', 'button', 'submit', 'reset', 'file', 'range', 'color']);

export interface HotkeyHost {
  isAuthenticated(): boolean;
  can: Can;
  go(path: string): void;
  toggleHelp(): void;
  /** Führt den Seitenbefehl mit dieser Taste aus; `true`, wenn es einen gab. */
  pageKey?(key: string): boolean;
  /** Setzt den Fokus ins Suchfeld der Seite; `true`, wenn es eines gibt. */
  focusSearch?(): boolean;
}

export const isMac = typeof navigator !== 'undefined' && /mac|iphone|ipad/i.test(navigator.platform || navigator.userAgent);

/** Tippt der Benutzer gerade? `target` ist das echte Element, auch aus einem Shadow DOM heraus. */
export function isTyping(target: EventTarget | undefined): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable || target.closest('.xterm')) return true;
  if (target instanceof HTMLTextAreaElement || target instanceof HTMLSelectElement) return true;
  return target instanceof HTMLInputElement && !TYPING_INPUTS.has(target.type);
}

/** Liegt das Ereignis in einem geöffneten modalen Dialog? Dahinter soll nichts springen. */
function isInModal(path: EventTarget[]): boolean {
  return path.some((node) => node instanceof HTMLDialogElement && node.open);
}

/** Die Seiten mit Sprungkürzel, die der Benutzer erreichen darf. */
export function gotoPages(can: Can): PageEntry[] {
  return PAGES.filter((entry) => entry.hotkey && isAvailable(entry, can));
}

export class Hotkeys {
  #host: HotkeyHost;
  #pending = false;
  #timer: ReturnType<typeof setTimeout> | undefined;

  constructor(host: HotkeyHost) {
    this.#host = host;
  }

  start(): () => void {
    document.addEventListener('keydown', this.#onKeyDown);
    return () => {
      document.removeEventListener('keydown', this.#onKeyDown);
      this.#clearPending();
    };
  }

  #onKeyDown = (event: KeyboardEvent): void => {
    const modifier = isMac ? event.metaKey : event.ctrlKey;
    if (modifier && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 's') {
      this.#save(event);
      return;
    }
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.key === 'Escape') {
      this.#clearPending();
      return;
    }
    const path = event.composedPath();
    if (isTyping(path[0]) || (isInModal(path) && event.key !== '?') || !this.#host.isAuthenticated()) {
      this.#clearPending();
      return;
    }
    if (this.#pending) this.#jump(event);
    else if (!this.#arrow(event, path)) this.#start(event);
  };

  #arrow(event: KeyboardEvent, path: EventTarget[]): boolean {
    if (event.defaultPrevented || event.shiftKey) return false;
    const target = arrowTarget(path, event.key);
    if (!target) return false;
    event.preventDefault();
    target.focus();
    return true;
  }

  /** Die Seite meldet mit `preventDefault()` am Ereignis, dass sie speichert; nur dann bleibt der Browser still. */
  #save(event: KeyboardEvent): void {
    const handled = !document.dispatchEvent(new CustomEvent(SAVE_EVENT, { cancelable: true }));
    if (handled) event.preventDefault();
  }

  #start(event: KeyboardEvent): void {
    if (event.key === '?') {
      event.preventDefault();
      this.#host.toggleHelp();
    } else if (event.key === 'g') {
      this.#pending = true;
      this.#timer = setTimeout(() => this.#clearPending(), SEQUENCE_MS);
    } else if (event.key === '/') {
      if (this.#host.focusSearch?.()) event.preventDefault();
    } else if (event.key.length === 1 && !event.shiftKey && this.#host.pageKey?.(event.key.toLowerCase())) {
      event.preventDefault();
    }
  }

  #jump(event: KeyboardEvent): void {
    this.#clearPending();
    const target = gotoPages(this.#host.can).find((entry) => entry.hotkey === event.key.toLowerCase());
    if (!target) return;
    event.preventDefault();
    this.#host.go(target.path);
  }

  #clearPending(): void {
    this.#pending = false;
    clearTimeout(this.#timer);
  }
}
