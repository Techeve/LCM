import { LitElement, css, html, nothing } from 'lit';
import { repeat } from 'lit/directives/repeat.js';
import './lcm-ampel.js';
import { fadeOut } from '../lib/motion.js';
import { BARS_CHANGE, shownBars } from './bar-watch.js';
import { icon } from './icons.js';
import { MODAL_CHANGE, anyModalOpen } from './modal-watch.js';
import { baseStyles, type StatusLevel } from './shared.js';

export interface ToastOptions {
  tone: 'ok' | 'info' | 'warn' | 'crit';
  title: string;
  message?: string;
  actionLabel?: string;
  onAction?: () => void;
  /** Hintergrundereignis (Job fertig, Server wieder da): bleibt länger stehen und trägt meist einen Link zum Objekt. */
  background?: boolean;
  /** Antwort auf einen Klick im offenen Fenster („Kopiert“): erscheint sofort, statt bis zum Schließen zu warten. */
  immediate?: boolean;
  /** Millisekunden; 0 = bleibt, bis es geschlossen wird. Ohne Angabe nach Ton und Herkunft (`toastDuration`). */
  duration?: number;
}

interface ToastItem extends ToastOptions {
  id: number;
}

/** Zeigt eine Meldung im Toaster, den die Shell einmal eingebunden hat. */
export function toast(options: ToastOptions): void {
  window.dispatchEvent(new CustomEvent<ToastOptions>('lcm-toast', { detail: options }));
}

/** Höchstens so viele Meldungen stehen sichtbar übereinander, der Rest als „+n weitere“. */
export const VISIBLE_TOASTS = 3;

/** Wie lange eine Meldung steht: Erfolg 5 s, Hintergrund 8 s, Fehler (`crit`) bleibt, bis man sie schließt. */
export function toastDuration(options: ToastOptions): number {
  if (options.duration !== undefined) return options.duration;
  if (options.tone === 'crit') return 0;
  return options.background || options.tone === 'warn' ? 8000 : 5000;
}

/** Abstand zwischen Stapel und Speicherleiste, über die er sich hebt. */
const BAR_GAP = 12;

interface Box {
  top: number;
  bottom: number;
  left: number;
  right: number;
}

/**
 * Wie weit der Stapel vom unteren Rand wandern muss, damit er keine Leiste verdeckt: bis über die oberste Leiste,
 * die sich mit ihm überschneidet; 0, wenn keine im Weg ist.
 */
export function liftOver(stack: Box, bars: Box[], viewportHeight: number): number {
  const blocking = bars.filter((bar) => bar.left < stack.right && bar.right > stack.left && bar.bottom > stack.top && bar.top < stack.bottom);
  if (blocking.length === 0) return 0;
  return viewportHeight - Math.min(...blocking.map((bar) => bar.top)) + BAR_GAP;
}

/** Nach Hover oder Fokus läuft eine Meldung noch mindestens so lange weiter. */
const RESUME_MIN = 2000;

const LEVEL: Record<Exclude<ToastOptions['tone'], 'info'>, StatusLevel> = { ok: 'great', warn: 'warn', crit: 'crit' };
let nextId = 1;

/**
 * Regel für alle Meldungen: Kurzes schwebt unten rechts (Toast), Behebbares steht am Auslöser (Feld,
 * Speicherleiste, Fensterfuß), Dauerhaftes hängt am Objekt (Band, `lcm-notice`).
 *
 * <lcm-toaster close-label="Schließen" .moreText=${(n) => …} .waitingText=${(n) => …}> – einmal pro Seite.
 * Meldungen kommen über `toaster.show({...})` oder das Fensterereignis `lcm-toast`. Neueste unten, höchstens drei
 * sichtbar. Hover oder Fokus hält den Ablauf an. Solange ein Fenster offen ist, warten neue Meldungen (ein kleiner
 * Hinweis zählt sie); kritische und Antworten auf einen Klick im Fenster erscheinen sofort. Der Stapel sagt höflich an, Kritisches als Alarm; eine Meldung
 * nimmt nie den Fokus. Speicherfehler und Fehler im offenen Fenster gehören nicht hierher.
 */
export class LcmToaster extends LitElement {
  static override properties = {
    closeLabel: { type: String, attribute: 'close-label' },
    moreText: { attribute: false },
    waitingText: { attribute: false },
    _items: { state: true },
    _held: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      .stack {
        inset-block-end: var(--space-5);
        inset-inline-end: var(--space-5);
        inline-size: min(24rem, calc(100vw - 2rem));
        display: flex;
        flex-direction: column;
        align-items: stretch;
        gap: var(--space-2);
        pointer-events: none;
        transition: none; /* die Meldungen bewegen sich selbst, der Stapel nicht */
      }
      .stack:not(:popover-open) {
        display: none;
      }
      .toast {
        display: grid;
        grid-template-columns: auto 1fr auto;
        gap: var(--space-3);
        align-items: start;
        padding: var(--space-3) var(--space-2) var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow:
          inset 3px 0 0 var(--t),
          var(--elev-2);
        pointer-events: auto;
        animation: toast-in var(--dur-2) var(--ease);
      }
      .toast[data-tone='ok'] {
        --t: var(--ok);
      }
      .toast[data-tone='info'] {
        --t: var(--info);
      }
      .toast[data-tone='warn'] {
        --t: var(--warn);
      }
      .toast[data-tone='crit'] {
        --t: var(--crit);
      }
      .mark {
        display: inline-grid;
        margin-block-start: 0.15rem;
      }
      .info-mark {
        color: var(--info);
      }
      .info-mark .icon {
        inline-size: 1.15rem;
        block-size: 1.15rem;
      }
      .title {
        font-weight: var(--fw-semibold);
      }
      .message {
        margin: 0.1rem 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .action {
        margin-block-start: var(--space-2);
        padding: 0;
        border: 0;
        background: none;
        color: var(--accent-text);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
        cursor: pointer;
      }
      .action:hover {
        text-decoration: underline;
      }
      .close {
        display: grid;
        place-items: center;
        inline-size: 1.75rem;
        block-size: 1.75rem;
        border: 0;
        border-radius: var(--radius-s);
        background: transparent;
        color: var(--text-2);
        cursor: pointer;
      }
      .close:hover {
        background: var(--surface-2);
        color: var(--text-1);
      }
      .more {
        align-self: flex-end;
        padding: 0.1rem var(--space-3);
        border-radius: var(--radius-pill);
        background: var(--surface-overlay);
        color: var(--text-2);
        box-shadow: var(--elev-2);
        font-size: var(--fs-xs);
      }
      .more.waiting {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        color: var(--text-1);
      }
      .more.waiting::before {
        content: '';
        inline-size: 0.45rem;
        block-size: 0.45rem;
        border-radius: 50%;
        background: var(--accent);
      }
      @media (max-width: 40rem) {
        .stack {
          inset-inline: var(--space-3);
          inset-block-end: var(--space-3);
          inline-size: auto;
        }
      }
      @keyframes toast-in {
        from {
          opacity: 0;
          transform: translateY(0.75rem);
        }
      }
    `,
  ];

  declare closeLabel: string;
  /** Text für die verdeckten Meldungen über dem Stapel, etwa „+2 weitere“. */
  declare moreText: (count: number) => string;
  /** Text für die zurückgehaltenen Meldungen, etwa „1 Meldung wartet“. */
  declare waitingText: (count: number) => string;
  declare _items: ToastItem[];
  /** Meldungen, die warten, bis kein Fenster mehr offen ist. */
  declare _held: ToastItem[];
  /** Laufende Uhren je Meldung mit ihrem Ablaufzeitpunkt. */
  private readonly timers = new Map<number, { timer: number; due: number }>();
  /** Verbleibende Zeit der Meldungen, solange Hover oder Fokus den Ablauf anhalten. */
  private readonly paused = new Map<number, number>();
  private readonly leaving = new Set<number>();
  private avoidFrame = 0;
  private hovered = false;
  private focused = false;
  private shown = 0;

  constructor() {
    super();
    this.closeLabel = '';
    this.moreText = (count) => `+${count}`;
    this.waitingText = (count) => String(count);
    this._items = [];
    this._held = [];
  }

  override connectedCallback(): void {
    super.connectedCallback();
    window.addEventListener('lcm-toast', this.onToastEvent as EventListener);
    window.addEventListener(MODAL_CHANGE, this.onModalChange);
    window.addEventListener(BARS_CHANGE, this.avoidBars);
    window.addEventListener('resize', this.avoidBars);
    window.addEventListener('scroll', this.avoidBars, { capture: true, passive: true });
  }

  override disconnectedCallback(): void {
    window.removeEventListener('lcm-toast', this.onToastEvent as EventListener);
    window.removeEventListener(MODAL_CHANGE, this.onModalChange);
    window.removeEventListener(BARS_CHANGE, this.avoidBars);
    window.removeEventListener('resize', this.avoidBars);
    window.removeEventListener('scroll', this.avoidBars, { capture: true });
    cancelAnimationFrame(this.avoidFrame);
    this.timers.forEach(({ timer }) => window.clearTimeout(timer));
    this.timers.clear();
    super.disconnectedCallback();
  }

  private readonly onToastEvent = (event: CustomEvent<ToastOptions>): void => {
    this.show(event.detail);
  };

  /** Ist kein Fenster mehr offen, kommen die zurückgehaltenen Meldungen in ihrer Reihenfolge nach. */
  private readonly onModalChange = (): void => {
    if (anyModalOpen() || this._held.length === 0) return;
    const held = this._held;
    this._held = [];
    this._items = [...this._items, ...held];
    held.forEach((item) => this.start(item));
  };

  /** Eine Leiste kam, ging oder wanderte: den Stapel einmal pro Bild neu ausrichten. */
  private readonly avoidBars = (): void => {
    if (this.shown === 0) return;
    cancelAnimationFrame(this.avoidFrame);
    this.avoidFrame = requestAnimationFrame(() => this.liftStack());
  };

  /** Hebt den Stapel über Speicherleisten, die er verdecken würde; sonst bleibt er unten. */
  private liftStack(): void {
    const stack = this.renderRoot.querySelector<HTMLElement>('.stack');
    if (!stack?.matches(':popover-open')) return;
    stack.style.removeProperty('inset-block-end');
    const bars = shownBars().map((bar) => bar.getBoundingClientRect());
    const lift = liftOver(stack.getBoundingClientRect(), bars, window.innerHeight);
    if (lift > 0) stack.style.insetBlockEnd = `${lift}px`;
  }

  show(options: ToastOptions): number {
    const item = { ...options, id: nextId++ };
    if (anyModalOpen() && options.tone !== 'crit' && !options.immediate) {
      this._held = [...this._held, item];
      return item.id;
    }
    this._items = [...this._items, item];
    this.start(item);
    return item.id;
  }

  /** Blendet die Meldung kurz aus und entfernt sie dann; schon beim Ausblenden nimmt sie keine Eingaben mehr an. */
  dismiss(id: number): void {
    this.disarm(id);
    this.paused.delete(id);
    if (this.leaving.has(id)) return;
    this.leaving.add(id);
    const element = this.renderRoot.querySelector<HTMLElement>(`.toast[data-id="${id}"]`);
    if (element) element.inert = true;
    void (element ? fadeOut(element) : Promise.resolve()).then(() => {
      this.leaving.delete(id);
      this._items = this._items.filter((item) => item.id !== id);
    });
  }

  private start(item: ToastItem): void {
    const duration = toastDuration(item);
    if (duration <= 0) return;
    if (this.hovered || this.focused) this.paused.set(item.id, duration);
    else this.arm(item.id, duration);
  }

  private arm(id: number, duration: number): void {
    if (this.leaving.has(id)) return;
    this.timers.set(id, { timer: window.setTimeout(() => this.dismiss(id), duration), due: Date.now() + duration });
  }

  private disarm(id: number): void {
    window.clearTimeout(this.timers.get(id)?.timer);
    this.timers.delete(id);
  }

  /** Hover oder Fokus im Stapel: alle Uhren anhalten und die Restzeit merken. */
  private pause(): void {
    for (const [id, { due }] of this.timers) {
      this.paused.set(id, Math.max(due - Date.now(), RESUME_MIN));
      this.disarm(id);
    }
  }

  private resume(): void {
    if (this.hovered || this.focused) return;
    for (const [id, rest] of this.paused) this.arm(id, rest);
    this.paused.clear();
  }

  private readonly onPointerEnter = (): void => {
    this.hovered = true;
    this.pause();
  };

  private readonly onPointerLeave = (): void => {
    this.hovered = false;
    this.resume();
  };

  private readonly onFocusIn = (): void => {
    this.focused = true;
    this.pause();
  };

  private readonly onFocusOut = (event: FocusEvent): void => {
    const stack = event.currentTarget as HTMLElement;
    if (event.relatedTarget instanceof Node && stack.contains(event.relatedTarget)) return;
    this.focused = false;
    this.resume();
  };

  override updated(): void {
    const stack = this.renderRoot.querySelector<HTMLElement>('.stack');
    if (!stack) return;
    const open = stack.matches(':popover-open');
    const size = this._items.length + this._held.length;
    const added = size > this.shown;
    this.shown = size;
    if (size === 0) {
      if (open) stack.hidePopover();
      return;
    }
    // Die oberste Ebene stapelt nach Öffnungsreihenfolge: Ist seit dem letzten
    // Hinweis ein Fenster aufgegangen, läge der Stapel dahinter. Neu öffnen
    // holt ihn wieder nach oben.
    if (open && added) stack.hidePopover();
    if (!open || added) stack.showPopover();
    this.avoidBars();
  }

  #mark(tone: ToastOptions['tone']) {
    if (tone === 'info') return html`<span class="mark info-mark">${icon('info')}</span>`;
    return html`<lcm-ampel class="mark" level=${LEVEL[tone]} size="m"></lcm-ampel>`;
  }

  #toast(item: ToastItem) {
    return html`<div class="toast" data-id=${item.id} data-tone=${item.tone} role=${item.tone === 'crit' ? 'alert' : 'status'}>
      ${this.#mark(item.tone)}
      <div>
        <div class="title">${item.title}</div>
        ${item.message ? html`<p class="message">${item.message}</p>` : nothing}
        ${item.actionLabel
          ? html`<button class="action" type="button" @click=${() => (item.onAction?.(), this.dismiss(item.id))}>${item.actionLabel}</button>`
          : nothing}
      </div>
      <button class="close" type="button" aria-label=${this.closeLabel} @click=${() => this.dismiss(item.id)}>${icon('x')}</button>
    </div>`;
  }

  override render() {
    const hidden = Math.max(0, this._items.length - VISIBLE_TOASTS);
    const visible = this._items.slice(hidden);
    const held = this._held.length;
    return html`<div
      class="stack"
      popover="manual"
      aria-live="polite"
      @pointerenter=${this.onPointerEnter}
      @pointerleave=${this.onPointerLeave}
      @focusin=${this.onFocusIn}
      @focusout=${this.onFocusOut}
    >
      ${hidden > 0 ? html`<div class="more">${this.moreText(hidden)}</div>` : nothing}
      ${repeat(visible, (item) => item.id, (item) => this.#toast(item))}
      ${held > 0 ? html`<div class="more waiting">${this.waitingText(held)}</div>` : nothing}
    </div>`;
  }
}
customElements.define('lcm-toaster', LcmToaster);
