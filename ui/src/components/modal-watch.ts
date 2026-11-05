/**
 * Welche Fenster gerade offen sind. Alle modalen Fenster (`lcm-modal`, `lcm-dialog`, Befehlspalette, Kürzelhilfe, Konsole) melden sich hier an und ab; der Toaster hält
 * neue Meldungen zurück, solange eines offen ist, und erfährt über `lcm-modal-change` am Fenster, wann es zugeht.
 */

const open = new Set<Element>();

export const MODAL_CHANGE = 'lcm-modal-change';

function changed(): void {
  window.dispatchEvent(new Event(MODAL_CHANGE));
}

export function modalOpened(element: Element): void {
  if (open.has(element)) return;
  open.add(element);
  changed();
}

export function modalClosed(element: Element): void {
  if (!open.delete(element)) return;
  changed();
}

export function anyModalOpen(): boolean {
  return open.size > 0;
}
