// Bewegung nur zur Orientierung: kurze Einblendungen, wenn Inhalt wechselt oder neu hinzukommt.
// Dauer und Kurve kommen aus den Tokens (`--dur-1`, `--ease`). Bei `prefers-reduced-motion: reduce`
// stehen die Tokens auf 0s und es bewegt sich nichts; der Inhalt ist sofort da.
// Animiert werden nur Deckkraft und Verschiebung: kein Layout-Sprung, nichts wartet, Klicks gehen immer durch.
// Popover, Dialoge und neue Listeneinträge (`data-fresh`) bewegen sich per CSS, siehe `baseStyles` in components/shared.ts.

import type { ReactiveController, ReactiveControllerHost } from 'lit';

/** Deckkraft, mit der neuer Inhalt beginnt: nicht ganz 0, sonst blitzt beim Wechsel kurz der leere Hintergrund auf. */
const FADE_FROM = 0.3;
/** Pixel, um die Inhalt beim Einblenden aufsteigt bzw. beim Ausblenden absinkt. */
const RISE = 4;

/** Liest eine CSS-Dauer (`0.16s`, `120ms`) in Millisekunden; alles Unlesbare oder Negative gilt als 0. */
export function parseDuration(value: string): number {
  const match = /^(\d*\.?\d+)(ms|s)$/.exec(value.trim());
  if (!match) return 0;
  const amount = Number(match[1]);
  return match[2] === 's' ? amount * 1000 : amount;
}

/** Wünscht die Person weniger Bewegung? */
export function prefersReducedMotion(): boolean {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
}

/** Dauer (`--dur-1`, der schnellste Schritt) und Kurve aus den Tokens; `null`, wenn sich nichts bewegen soll. */
function timing(element: Element): KeyframeAnimationOptions | null {
  if (typeof element.animate !== 'function' || prefersReducedMotion()) return null;
  const style = getComputedStyle(element);
  const duration = parseDuration(style.getPropertyValue('--dur-1'));
  if (duration === 0) return null;
  return { duration, easing: style.getPropertyValue('--ease').trim() || 'ease-out' };
}

/** Blendet ein Element ein: aus halb durchsichtig und ein paar Pixel tiefer an seinen Platz. */
export function fadeIn(element: Element): Animation | null {
  const base = timing(element);
  if (!base) return null;
  return element.animate(
    [
      { opacity: FADE_FROM, transform: `translateY(${RISE}px)` },
      { opacity: 1, transform: 'none' },
    ],
    base,
  );
}

/**
 * Blendet ein Element aus, bevor es entfernt wird; erfüllt sich, sobald es unsichtbar ist (bei reduzierter Bewegung sofort).
 * Das Element bleibt danach unsichtbar stehen, bis der Aufrufer es entfernt.
 */
export function fadeOut(element: Element): Promise<void> {
  const base = timing(element);
  if (!base) return Promise.resolve();
  const animation = element.animate([{ opacity: 1 }, { opacity: 0, transform: `translateY(${RISE}px)` }], { ...base, fill: 'forwards' });
  return animation.finished.then(
    () => undefined,
    () => undefined, // abgebrochen: das Element ist ohnehin weg
  );
}

/**
 * Blendet `target` kurz ein, sobald sich `key` ändert (anderer Server, anderer Reiter, Daten statt Ladeplatzhalter).
 * Das erste Rendern bewegt sich nicht: Was beim Öffnen schon da ist, steht einfach da.
 */
export class FadeOnChange implements ReactiveController {
  #seen = false;
  #key: unknown;

  constructor(
    host: ReactiveControllerHost,
    private readonly key: () => unknown,
    private readonly target: () => Element | null | undefined,
  ) {
    host.addController(this);
  }

  hostUpdated(): void {
    const key = this.key();
    if (this.#seen && Object.is(key, this.#key)) return;
    const first = !this.#seen;
    this.#seen = true;
    this.#key = key;
    const target = first ? null : this.target();
    if (target) fadeIn(target);
  }

  hostDisconnected(): void {
    this.#seen = false;
  }
}

/**
 * Welche Schlüssel sind neu hinzugekommen? Ohne Überschneidung mit vorher (andere Seite, anderer Filter) ist nichts
 * „neu“, sondern einfach eine andere Liste.
 */
export function freshKeys(previous: ReadonlySet<string>, next: ReadonlySet<string>): Set<string> {
  const added = [...next].filter((key) => !previous.has(key));
  if (previous.size > 0 && added.length === next.size) return new Set();
  return new Set(added);
}

/**
 * Merkt sich die Einträge einer live aktualisierten Liste und nennt die, die seit der letzten Antwort neu sind.
 * Solange dieselbe Liste (dasselbe Array) kommt, bleibt die Antwort gleich, auch wenn öfter gerendert wird.
 * Die erste Liste gilt nie als neu.
 */
export class FreshKeys<T> {
  #items: readonly T[] | undefined;
  #keys: Set<string> | undefined;
  #fresh: ReadonlySet<string> = new Set();

  constructor(private readonly keyOf: (item: T) => string) {}

  track(items: readonly T[]): ReadonlySet<string> {
    if (items === this.#items) return this.#fresh;
    const keys = new Set(items.map(this.keyOf));
    this.#fresh = this.#keys ? freshKeys(this.#keys, keys) : new Set();
    this.#items = items;
    this.#keys = keys;
    return this.#fresh;
  }
}
