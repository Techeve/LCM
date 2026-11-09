// Was der Update-Balken unter dem Hauptmenü zeigt. Ein Balken, zwei Lagen:
// Läuft ein Selbst-Update, hat dessen Fortschritt Vorrang. Sonst die neue Version,
// solange sie nicht weggeklickt ist (je Version gemerkt). Die Antwort auf „Jetzt prüfen“
// gehört nicht hierher: Sie steht im Info-Fenster neben dem Knopf.

import { selfUpdateBusy, type SelfUpdate, type UpdateInfo } from '../api/system';
import { safeStorage } from '../components/shared';

export type UpdateBanner =
  | { kind: 'busy'; status: SelfUpdate }
  | { kind: 'available'; info: UpdateInfo; self: SelfUpdate | null }
  | null;

export function updateBanner(info: UpdateInfo | undefined, self: SelfUpdate | null | undefined, dismissed: string): UpdateBanner {
  if (self && selfUpdateBusy(self)) return { kind: 'busy', status: self };
  if (!info) return null;
  if (info.update_available && info.latest_version !== dismissed) return { kind: 'available', info, self: self ?? null };
  return null;
}

// ---- Fortschritt im Update-Fenster ----

export type StepState = 'done' | 'current' | 'pending';

export interface UpdateStep {
  key: 'jobs' | 'backup' | 'install';
  state: StepState;
}

const STEP_ORDER: readonly UpdateStep['key'][] = ['jobs', 'backup', 'install'];
const CURRENT_STEP: Partial<Record<SelfUpdate['phase'], UpdateStep['key']>> = { waiting: 'jobs', backup: 'backup', running: 'install' };

/** Die drei Schritte des Updates und wie weit es ist. Ohne laufendes Update gibt es keine. */
export function updateSteps(status: SelfUpdate | null | undefined): UpdateStep[] {
  const current = status ? CURRENT_STEP[status.phase] : undefined;
  if (!current) return [];
  const at = STEP_ORDER.indexOf(current);
  return STEP_ORDER.map((key, i) => ({ key, state: i < at ? 'done' : i === at ? 'current' : 'pending' }));
}

const DISMISSED_KEY = 'lcm.update.dismissed';

/** Gemeinsamer Stand von Balken und Info-Fenster: die weggeklickte Version. */
export class UpdateState {
  #dismissed = safeStorage.get(DISMISSED_KEY) ?? '';
  #listeners = new Set<() => void>();

  get dismissed(): string {
    return this.#dismissed;
  }

  /** Nach „Jetzt prüfen“: Auch eine früher weggeklickte Version kommt zurück, im Balken liegt „Jetzt installieren“. */
  restoreDismissed(): void {
    this.#setDismissed('');
  }

  dismiss(version: string): void {
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
