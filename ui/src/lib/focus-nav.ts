// Fokus mit der Tastatur weiterreichen, quer durch Shadow DOMs: das Suchfeld der
// Seite finden (`/`) und mit den Pfeiltasten links/rechts zwischen den Einträgen
// einer Menüleiste oder den Bedienelementen einer Tabellenzeile wechseln; Pos1 und
// Ende springen in einer Menüleiste zum ersten und letzten Eintrag.
// Elemente mit eigener Pfeiltasten-Bedienung (Reiter, Menüs, Listen) bleiben unberührt; die Tabellen
// (role=grid) führen selbst nur hoch/runter, links/rechts kommt von hier.

const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex="0"]';
const OWN_ARROWS = new Set(['tablist', 'tab', 'menu', 'menuitem', 'menuitemradio', 'listbox', 'option', 'combobox', 'slider', 'radiogroup', 'tree']);

function visible(element: Element): boolean {
  if (element.closest('[popover]:not(:popover-open), dialog:not([open]), [hidden], [inert]')) return false;
  return typeof element.checkVisibility === 'function' ? element.checkVisibility() : true;
}

/** Alle Elemente unter `root` in Dokumentreihenfolge, auch in den Shadow Roots eigener Elemente. */
function* walk(root: Element | ShadowRoot): Generator<Element> {
  for (const child of Array.from(root.children)) {
    const nodes = child instanceof HTMLSlotElement ? child.assignedElements({ flatten: true }) : [child];
    for (const node of nodes) {
      yield node;
      yield* walk(node.shadowRoot ?? node);
    }
  }
}

/** Das erste passende, sichtbare Element unter `root`, Shadow DOMs eingeschlossen. */
export function findDeep(root: Element | ShadowRoot, match: (element: Element) => boolean): Element | null {
  for (const element of walk(root)) if (match(element) && visible(element)) return element;
  return null;
}

/** Die erreichbaren Bedienelemente unter `root` (in Shadow DOMs nur die echten Knöpfe und Links). */
export function focusablesIn(root: Element): HTMLElement[] {
  const found: HTMLElement[] = [];
  for (const element of walk(root)) {
    if (element instanceof HTMLElement && element.matches(FOCUSABLE) && visible(element)) found.push(element);
  }
  return found;
}

/** Fokussiert das Suchfeld der Seite (`lcm-input type="search"` oder ein Suchfeld im Inhalt). */
export function focusPageSearch(main: Element | null): boolean {
  if (!main) return false;
  const field = findDeep(main, (element) => element.getAttribute('type') === 'search' && (element.localName === 'lcm-input' || element instanceof HTMLInputElement));
  if (!(field instanceof HTMLElement)) return false;
  field.focus();
  return true;
}

/** Wohin links/rechts (in einer Leiste auch Pos1/Ende) den Fokus bringt; `null`, wenn die Taste hier nichts tun soll. */
export function arrowTarget(path: EventTarget[], key: string): HTMLElement | null {
  const edge = key === 'Home' || key === 'End';
  if (key !== 'ArrowLeft' && key !== 'ArrowRight' && !edge) return null;
  const elements = path.filter((node): node is HTMLElement => node instanceof HTMLElement);
  if (elements.some((element) => OWN_ARROWS.has(element.getAttribute('role') ?? ''))) return null;
  const focused = elements[0];
  if (!focused || (focused instanceof HTMLInputElement && (focused.type === 'radio' || focused.type === 'range'))) return null;
  const row = elements.find((element) => element.localName === 'tr');
  if (row) return edge ? null : stepInRow(row, focused, key === 'ArrowRight' ? 1 : -1);
  const bar = elements.find((element) => element.localName === 'nav' || element.getAttribute('role') === 'menubar' || element.getAttribute('role') === 'toolbar');
  if (!bar) return null;
  const items = focusablesIn(bar);
  if (!edge) return stepIn(items, focused, key === 'ArrowRight' ? 1 : -1, true);
  return items.includes(focused) ? ((key === 'Home' ? items[0] : items[items.length - 1]) ?? null) : null;
}

function stepIn(items: HTMLElement[], focused: HTMLElement, step: number, wrap: boolean): HTMLElement | null {
  const index = items.indexOf(focused);
  if (index < 0) return null;
  const next = index + step;
  if (wrap) return items[(next + items.length) % items.length] ?? null;
  return items[next] ?? null;
}

/** In einer Tabellenzeile: von der Zeile zum ersten Bedienelement, dann weiter; links wieder zurück zur Zeile. */
function stepInRow(row: HTMLElement, focused: HTMLElement, step: number): HTMLElement | null {
  const controls = focusablesIn(row).filter((element) => element !== row);
  if (focused === row) return step > 0 ? (controls[0] ?? null) : null;
  const index = controls.indexOf(focused);
  if (index < 0) return null;
  if (index + step < 0) return row.tabIndex >= -1 && row.hasAttribute('tabindex') ? row : null;
  return controls[index + step] ?? null;
}
