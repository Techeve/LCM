// Was der Update-Balken unter dem Hauptmenü zeigt. Ein Balken, drei Lagen:
// Läuft ein Selbst-Update, hat dessen Fortschritt Vorrang. Sonst die neue Version,
// solange sie nicht weggeklickt ist (je Version gemerkt). Und wenn es keine gibt,
// aber gerade jemand von Hand geprüft hat, die Entwarnung: „alles aktuell“ ist eine Antwort.

import { selfUpdateBusy, type SelfUpdate, type UpdateInfo } from '../api/system';
import { safeStorage } from '../components/shared';

export type UpdateBanner =
  | { kind: 'busy'; status: SelfUpdate }
  | { kind: 'available'; info: UpdateInfo; self: SelfUpdate | null }
  | { kind: 'current'; info: UpdateInfo }
  | null;

export function updateBanner(info: UpdateInfo | undefined, self: SelfUpdate | null | undefined, dismissed: string, checkedNow: boolean): UpdateBanner {
  if (self && selfUpdateBusy(self)) return { kind: 'busy', status: self };
  if (!info) return null;
  if (info.update_available && info.latest_version !== dismissed) return { kind: 'available', info, self: self ?? null };
  return checkedNow ? { kind: 'current', info } : null;
}

const DISMISSED_KEY = 'lcm.update.dismissed';

/** Gemeinsamer Stand von Balken und Info-Fenster: weggeklickte Version und „gerade von Hand geprüft“. */
export class UpdateState {
  #dismissed = safeStorage.get(DISMISSED_KEY) ?? '';
  #checkedNow = false;
  #listeners = new Set<() => void>();

  get dismissed(): string {
    return this.#dismissed;
  }

  get checkedNow(): boolean {
    return this.#checkedNow;
  }

  /** Nach „Jetzt prüfen“: Das Ergebnis gehört in den Balken, auch eine früher weggeklickte Version. */
  markChecked(): void {
    this.#checkedNow = true;
    this.#setDismissed('');
  }

  dismiss(version: string): void {
    this.#checkedNow = false;
    this.#setDismissed(version);
  }

  onChange(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  #setDismissed(version: string): void {
    this.#dismissed = version;
    safeStorage.set(DISMISSED_KEY, version || null);
    this.#listeners.forEach((listener) => listener());
  }
}

export const updateState = new UpdateState();

// ---- Info-Fenster ----

/** Das Info-Fenster lässt sich von überall öffnen, auch von der Anmeldeseite. */
class AboutOpener {
  #listeners = new Set<() => void>();

  open(): void {
    this.#listeners.forEach((listener) => listener());
  }

  onOpen(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }
}

export const about = new AboutOpener();
