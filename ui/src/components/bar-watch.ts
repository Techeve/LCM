/**
 * Welche Speicherleisten gerade zu sehen sind. `lcm-save-bar` meldet sich hier an und ab; der Toaster hebt seinen
 * Stapel über eine Leiste, die er sonst verdeckte, und erfährt über `lcm-bars-change`, wann eine dazukommt oder geht.
 */

const shown = new Set<Element>();

export const BARS_CHANGE = 'lcm-bars-change';

function changed(): void {
  window.dispatchEvent(new Event(BARS_CHANGE));
}

export function barShown(element: Element): void {
  if (shown.has(element)) return;
  shown.add(element);
  changed();
}

export function barHidden(element: Element): void {
  if (!shown.delete(element)) return;
  changed();
}

export function shownBars(): Element[] {
  return [...shown];
}
