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

/**
 * Erkennt Klicks auf den Hintergrund eines `<dialog>`. Ein Klick zählt nur, wenn er dort auch begonnen hat:
 * Wer in einem Eingabefeld Text markiert und die Maus erst außerhalb des Fensters loslässt, erzeugt einen
 * `click` auf dem `<dialog>` selbst — das darf das Fenster nicht schließen.
 */
export class BackdropClick {
  private pressedOnBackdrop = false;

  /** An `@pointerdown` des `<dialog>` hängen. */
  readonly down = (event: PointerEvent): void => {
    this.pressedOnBackdrop = event.target === event.currentTarget;
  };

  /** Im `@click` des `<dialog>` fragen: lagen Drücken und Loslassen beide auf dem Hintergrund? */
  hit(event: MouseEvent): boolean {
    const hit = this.pressedOnBackdrop && event.target === event.currentTarget;
    this.pressedOnBackdrop = false;
    return hit;
  }
}
