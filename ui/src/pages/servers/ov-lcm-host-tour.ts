import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import { baseStyles, emit } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { deepQuery } from '../../lib/deep-query';
import { t } from '../../lib/i18n';

/** Ein Schritt der Einführung: Texte und die Elemente, auf die das Fenster zeigt. Der Zähler zählt alle Schritte durchgehend. */
export interface TourStep {
  key: string;
  title: string;
  text: string;
  badge?: string;
  /** Pfade der hervorzuhebenden Elemente (siehe `deepQuery`). */
  targets: readonly string[];
}

export interface Box {
  x: number;
  y: number;
  w: number;
  h: number;
}
export type Corner = 'right-top' | 'left-top' | 'right-bottom' | 'left-bottom';

const CORNERS: readonly Corner[] = ['right-top', 'left-top', 'right-bottom', 'left-bottom'];
/** Abstand des Fensters zum Rand und Höhe der Kopfleiste darüber (7rem), in Pixeln bei 16 px je rem. */
const EDGE = 20;
const TOP = 112;
const CLEARANCE = 12;

function boxAt(corner: Corner, panel: Pick<Box, 'w' | 'h'>, view: Pick<Box, 'w' | 'h'>): Box {
  const x = corner.startsWith('right') ? view.w - EDGE - panel.w : EDGE;
  const y = corner.endsWith('top') ? TOP : view.h - EDGE - panel.h;
  return { x, y, w: panel.w, h: panel.h };
}

function overlaps(a: Box, b: Box): boolean {
  return a.x < b.x + b.w + CLEARANCE && a.x + a.w + CLEARANCE > b.x && a.y < b.y + b.h + CLEARANCE && a.y + a.h + CLEARANCE > b.y;
}

/** Die Ecke, in der das Fenster das Ziel nicht verdeckt; die bisherige, solange sie frei ist, sonst die erste freie, sonst die bisherige. */
export function pickCorner(target: Box | null, panel: Pick<Box, 'w' | 'h'>, view: Pick<Box, 'w' | 'h'>, current: Corner): Corner {
  if (!target) return current;
  const free = (corner: Corner) => !overlaps(boxAt(corner, panel, view), target);
  if (free(current)) return current;
  return CORNERS.find(free) ?? current;
}

/** Das Rechteck, das alle Elemente umfasst; `null`, wenn es keines gibt oder es nichts misst. */
function union(elements: readonly Element[]): Box | null {
  const rects = elements.map((element) => element.getBoundingClientRect()).filter((rect) => rect.width > 0 && rect.height > 0);
  if (rects.length === 0) return null;
  const left = Math.min(...rects.map((rect) => rect.left));
  const top = Math.min(...rects.map((rect) => rect.top));
  return { x: left, y: top, w: Math.max(...rects.map((rect) => rect.right)) - left, h: Math.max(...rects.map((rect) => rect.bottom)) - top };
}

const same = (a: Box | null, b: Box | null) => a === b || (!!a && !!b && a.x === b.x && a.y === b.y && a.w === b.w && a.h === b.h);

/** Wie oft die Suche nach dem Ziel nach einem Wechsel schnell wiederholt wird (die Seite lädt Reiter und Fenster nach). */
const FAST_TRIES = 40;

/**
 * `<lcm-srv-lcm-tour .steps .step>`: Einführungsfenster am Rand des Fensters (auf schmalen Bildschirmen unten), über dem
 * Inhalt und ohne ihn zu verschieben. Es erklärt und führt, mehr nicht: keine Aktionsknöpfe, nur Weiter, Zurück und
 * Schließen. Zu jedem Schritt zeichnet es einen Rahmen um die Ziele (auch tief in den Komponenten der Seite), scrollt sie
 * ins Bild und rückt in eine Ecke, in der es sie nicht verdeckt. Meldet `lcm-tour-step` { step } bei Weiter und Zurück und
 * `lcm-tour-close` beim Schließen; welcher Schritt offen ist, hält der Besitzer.
 */
export class SrvLcmTour extends LitElement {
  static override properties = {
    steps: { attribute: false },
    step: { type: Number },
    _box: { state: true },
    _corner: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      .ring {
        position: fixed;
        z-index: 7;
        border: 2px solid var(--accent);
        border-radius: var(--radius-m);
        box-shadow: 0 0 0 4px var(--accent-soft, transparent);
        pointer-events: none;
      }
      .panel {
        position: fixed;
        z-index: 8;
        inset-block-start: 7rem;
        inset-inline-end: var(--space-5);
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        inline-size: min(23rem, calc(100vw - 2 * var(--space-5)));
        max-block-size: calc(100dvh - 8.5rem);
        padding: var(--space-card);
        overflow-y: auto;
        border: 1px solid var(--line-muted);
        border-radius: var(--radius-l);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow:
          0 1px 3px var(--shadow-near),
          0 12px 32px var(--shadow-xl);
      }
      .panel[data-corner='left-top'] {
        inset-inline: var(--space-5) auto;
      }
      .panel[data-corner='right-bottom'] {
        inset-block: auto var(--space-5);
      }
      .panel[data-corner='left-bottom'] {
        inset-block: auto var(--space-5);
        inset-inline: var(--space-5) auto;
      }
      .progress {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .dots {
        display: flex;
        gap: var(--space-1);
      }
      .dot {
        inline-size: 0.5rem;
        block-size: 0.5rem;
        border-radius: var(--radius-pill);
        background: var(--surface-3);
      }
      .dot[data-active] {
        background: var(--accent-solid);
      }
      h2 {
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        gap: var(--space-2);
        margin: 0;
        font-size: var(--fs-l);
      }
      p {
        margin: 0;
        color: var(--text-2);
      }
      .nav {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
      }
      .nav .spacer {
        margin-inline-start: auto;
      }
      .foot {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-1) var(--space-3);
      }
      .hint {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      @media (max-width: 40rem) {
        .panel,
        .panel[data-corner] {
          inset-block: auto var(--space-3);
          inset-inline: var(--space-3);
          inline-size: auto;
          max-block-size: 55dvh;
        }
      }
    `,
  ];

  declare steps: readonly TourStep[];
  declare step: number;
  declare _box: Box | null;
  declare _corner: Corner;

  #key: string | undefined;
  #found: Element[] = [];
  #tries = 0;
  #revealed = false;
  #timer: ReturnType<typeof setInterval> | undefined;
  readonly #measure = () => this.#follow();

  constructor() {
    super();
    this.steps = [];
    this.step = 0;
    this._box = null;
    this._corner = 'right-top';
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    addEventListener('scroll', this.#measure, { capture: true, passive: true });
    addEventListener('resize', this.#measure);
    this.#timer = setInterval(() => this.#search(), 150);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    removeEventListener('scroll', this.#measure, { capture: true });
    removeEventListener('resize', this.#measure);
    clearInterval(this.#timer);
  }

  override willUpdate(): void {
    // Der Besitzer reicht bei jedem Zeichnen neue Schritte herein; neu gesucht wird nur bei einem anderen Schritt.
    const key = this.steps[this.step]?.key;
    if (key === this.#key) return;
    this.#key = key;
    this.#found = [];
    this.#tries = 0;
    this.#revealed = false;
    this._box = null;
    this._corner = 'right-top';
  }

  override updated(): void {
    this.#placeRing();
    this.#search();
  }

  get #targets(): readonly string[] {
    return this.steps[this.step]?.targets ?? [];
  }

  get #narrow(): boolean {
    return typeof matchMedia === 'function' && matchMedia('(max-width: 40rem)').matches;
  }

  /** Sucht die Ziele, solange sie fehlen oder aus der Seite verschwunden sind, und zeigt sie dann. */
  #search(): void {
    if (this.#found.length > 0 && this.#found.every((element) => element.isConnected)) return this.#follow();
    this.#found = [];
    if (this.#targets.length === 0) return;
    // Nach den schnellen Versuchen nur noch gelegentlich: Das Ziel kann später durch eine Eingabe erscheinen.
    this.#tries += 1;
    if (this.#tries > FAST_TRIES && this.#tries % 8 !== 0) return;
    this.#found = this.#targets.map((path) => deepQuery(path)).filter((element): element is Element => element !== null);
    if (this.#found.length === 0) return;
    this.#reveal(this.#found[0]!);
    this.#follow();
  }

  /** Bringt das erste Ziel ins Bild, ohne zu springen, wenn es schon gut zu sehen ist. */
  #reveal(element: Element): void {
    if (this.#revealed) return;
    this.#revealed = true;
    // In einem Fenster scrollt nur dessen Inhalt, und zwar so wenig wie nötig; sonst verschwände das Ziel hinter dem Fuß des Fensters.
    if (element.closest('lcm-modal')) return element.scrollIntoView?.({ block: 'nearest' });
    const rect = element.getBoundingClientRect();
    const narrow = this.#narrow;
    const roomBelow = narrow ? innerHeight * 0.45 : innerHeight;
    if (rect.top >= TOP && rect.bottom <= roomBelow) return;
    const calm = typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches;
    const fits = rect.height < innerHeight - 2 * TOP;
    (element as HTMLElement).style?.setProperty('scroll-margin-block', `${TOP}px 2rem`);
    element.scrollIntoView?.({ block: narrow || !fits ? 'start' : 'center', behavior: calm ? 'auto' : 'smooth' });
  }

  /** Setzt Rahmen und Ecke nach dem aktuellen Ort der Ziele. */
  #follow(): void {
    const box = union(this.#found);
    if (!same(box, this._box)) this._box = box;
    const panel = this.renderRoot.querySelector<HTMLElement>('.panel')?.getBoundingClientRect();
    if (!panel || this.#narrow) return;
    const view = { w: innerWidth, h: innerHeight };
    const corner = pickCorner(box, { w: panel.width, h: panel.height }, view, this._corner);
    if (corner !== this._corner) this._corner = corner;
  }

  #go(step: number): void {
    emit(this, 'lcm-tour-step', { step });
  }

  #close(variant: 'primary' | 'ghost', label: string) {
    return html`<lcm-button variant=${variant} size=${variant === 'ghost' ? 'small' : 'm'} icon="check" data-action="close-intro" @click=${() => emit(this, 'lcm-tour-close', {})}>${label}</lcm-button>`;
  }

  #ring() {
    const box = this._box;
    if (!box || box.y + box.h < 0 || box.y > innerHeight) return nothing;
    return html`<div class="ring" data-field="tour-ring"></div>`;
  }

  /** Legt den Rahmen über das CSSOM: Ein Style-Attribut (auch das erste von `styleMap`) ließe die Inhaltssicherheitsrichtlinie nicht zu. */
  #placeRing(): void {
    const box = this._box;
    const ring = this.renderRoot.querySelector<HTMLElement>('.ring');
    if (!box || !ring) return;
    ring.style.setProperty('inset-inline-start', `${box.x - 4}px`);
    ring.style.setProperty('inset-block-start', `${box.y - 4}px`);
    ring.style.setProperty('inline-size', `${box.w + 8}px`);
    ring.style.setProperty('block-size', `${box.h + 8}px`);
  }

  override render() {
    const current = this.steps[this.step];
    if (!current) return nothing;
    const first = this.step === 0;
    const last = this.step === this.steps.length - 1;
    const progress = t('setup.intro.progress', { number: this.step + 1, total: this.steps.length });
    return html`${this.#ring()}
      <section class="panel" role="region" aria-label=${t('setup.intro.region')} data-field="tour" data-corner=${this._corner}>
        <div class="progress">
          <span data-field="tour-progress">${progress}</span>
          <span class="dots" aria-hidden="true">${this.steps.map((_, index) => html`<span class="dot" ?data-active=${index === this.step}></span>`)}</span>
        </div>
        <div aria-live="polite">
          <h2 data-field="tour-title">${current.title}${current.badge ? html`<lcm-badge tone="accent">${current.badge}</lcm-badge>` : nothing}</h2>
          <p data-field="tour-text">${current.text}</p>
        </div>
        <div class="nav">
          ${first ? nothing : html`<lcm-button variant="ghost" icon="chevron-left" data-action="tour-back" @click=${() => this.#go(this.step - 1)}>${t('setup.intro.back')}</lcm-button>`}
          <span class="spacer">
            ${last ? this.#close('primary', t('setup.intro.finish')) : html`<lcm-button variant="primary" icon="arrow-right" data-action="tour-next" @click=${() => this.#go(this.step + 1)}>${t('setup.intro.next')}</lcm-button>`}
          </span>
        </div>
        <div class="foot">${last ? nothing : this.#close('ghost', t('setup.intro.close'))}<span class="hint">${t('setup.intro.closeHint')}</span></div>
      </section>`;
  }
}
customElements.define('lcm-srv-lcm-tour', SrvLcmTour);
