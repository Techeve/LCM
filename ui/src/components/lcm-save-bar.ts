import { LitElement, css, html, nothing, type PropertyValues } from 'lit';
import { prefersReducedMotion } from '../lib/motion.js';
import { unsaved } from '../lib/unsaved.js';
import './lcm-ampel.js';
import './lcm-button.js';
import { barHidden, barShown } from './bar-watch.js';
import { baseStyles, emit } from './shared.js';

/** Was die Leiste über das Feld wissen muss, das den Fehler trägt. */
export interface SaveBarField {
  /** Beschriftung des Felds, sie steht in der Meldung. */
  label: string;
  /** Liefert das Feld zum Hinspringen; ohne Rückgabe entfällt „Zum Feld“. */
  element: () => HTMLElement | null | undefined;
}

export interface SaveBarLabels {
  region: string;
  /** Leer: kein Hinweis auf Ungespeichertes (etwa wenn der Knopf ohnehin daneben steht). */
  unsaved: string;
  /** Kurzer Zusatz hinter „ungespeichert“, etwa das Tastenkürzel; leer entfällt er. */
  hint: string;
  save: string;
  /** Leer: kein Verwerfen-Knopf. */
  discard: string;
  jump: string;
  /** Meldung bei einem Feldfehler, `{field}` wird ersetzt. */
  fieldError: string;
}

/**
 * <lcm-save-bar .labels=${…} ?dirty ?busy error="…" .field=${…}> – die Speicherleiste jeder Seite, die ohne
 * Fenster speichert. Behebbares steht am Auslöser: Speicherfehler stehen hier neben dem Knopf, nie als Toast und
 * nie oben auf der Seite. Bei einem Feldfehler nennt sie das Feld und bietet „Zum Feld“ (scrollt und setzt den
 * Fokus hinein); den Fehlertext trägt das Feld selbst. Der Erfolg kommt danach als kurzer Toast.
 * Die Leiste erscheint, sobald es etwas zu speichern oder zu melden gibt; mit `always` steht sie immer (kurze
 * Karten, in denen sonst der Knopf fehlte), mit `sticky` klebt sie am unteren Rand langer Seiten.
 * Solange `dirty` steht, fragt der Router vor einem Seitenwechsel nach (`lib/unsaved`).
 * Ereignisse: `lcm-save`, `lcm-discard`.
 */
export class LcmSaveBar extends LitElement {
  static override properties = {
    dirty: { type: Boolean },
    busy: { type: Boolean },
    error: { type: String },
    field: { attribute: false },
    labels: { attribute: false },
    sticky: { type: Boolean, reflect: true },
    always: { type: Boolean },
    disabled: { type: Boolean },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      :host([sticky]) {
        position: sticky;
        inset-block-end: var(--space-4);
        z-index: var(--z-sticky);
      }
      .bar {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-4);
        font-size: var(--fs-s);
      }
      :host([sticky]) .bar {
        padding: var(--space-3) var(--space-3) var(--space-3) var(--space-4);
        border-radius: var(--radius-l);
        background: var(--surface-overlay);
        box-shadow:
          var(--ring-card),
          var(--elev-2);
      }
      :host([sticky]) .bar.error {
        background: color-mix(in srgb, var(--crit-soft) 70%, var(--surface-overlay));
        box-shadow:
          inset 3px 0 0 var(--crit),
          var(--ring-card),
          var(--elev-2);
      }
      .state {
        flex: 1 1 14rem;
        display: flex;
        align-items: flex-start;
        gap: var(--space-2);
        min-inline-size: 0;
      }
      .state:empty {
        display: none;
      }
      .title {
        font-weight: var(--fw-medium);
      }
      .error .title {
        color: var(--crit-text);
        font-weight: var(--fw-semibold);
      }
      .error .text {
        overflow-wrap: anywhere;
      }
      .hint {
        margin-inline-start: var(--space-2);
        color: var(--text-2);
      }
      lcm-ampel {
        margin-block-start: 0.2rem;
      }
      .dot {
        flex: none;
        inline-size: 0.5rem;
        block-size: 0.5rem;
        margin-block-start: 0.45rem;
        border-radius: 50%;
        background: var(--warn);
      }
      .buttons {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin-inline-start: auto;
      }
      :host(:not([sticky])) .buttons {
        order: -1;
        margin-inline-start: 0;
      }
    `,
  ];

  declare dirty: boolean;
  declare busy: boolean;
  /** Speicherfehler ohne Feld (Text vom Server) oder Grund bei einem Feldfehler. */
  declare error: string;
  /** Das Feld, an dem der Fehler hängt; dann nennt die Leiste es und bietet „Zum Feld“. */
  declare field: SaveBarField | null;
  declare labels: SaveBarLabels;
  declare sticky: boolean;
  declare always: boolean;
  /** Speichern geht gerade nicht (etwa Pflichtfeld leer), unabhängig von `dirty`. */
  declare disabled: boolean;

  constructor() {
    super();
    this.dirty = false;
    this.busy = false;
    this.error = '';
    this.field = null;
    this.labels = { region: '', unsaved: '', hint: '', save: '', discard: '', jump: '', fieldError: '' };
    this.sticky = false;
    this.always = false;
    this.disabled = false;
  }

  /** Scrollt zum Feld mit dem Fehler und setzt den Fokus hinein. */
  jump(): void {
    const element = this.field?.element();
    if (!element) return;
    element.scrollIntoView({ block: 'center', behavior: prefersReducedMotion() ? 'auto' : 'smooth' });
    element.focus({ preventScroll: true });
  }

  #state() {
    const labels = this.labels;
    if (this.field) {
      return html`<lcm-ampel level="crit" size="m"></lcm-ampel>
        <span role="alert"><span class="title">${labels.fieldError.replace('{field}', this.field.label)}</span>${this.error ? html` <span class="text">${this.error}</span>` : nothing}</span>`;
    }
    if (this.error) return html`<lcm-ampel level="crit" size="m"></lcm-ampel><span class="text title" role="alert">${this.error}</span>`;
    if (this.dirty && labels.unsaved) return html`<span class="dot"></span><span role="status"><span class="title">${labels.unsaved}</span>${labels.hint ? html`<span class="hint">${labels.hint}</span>` : nothing}</span>`;
    return nothing;
  }

  override updated(changed: PropertyValues): void {
    if (changed.has('dirty')) unsaved.set(this, this.dirty);
    // Toasts weichen einer sichtbaren Leiste aus, damit sie den Speichern-Knopf nicht verdecken.
    if (this.renderRoot.querySelector('.bar')) barShown(this);
    else barHidden(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    unsaved.set(this, this.dirty);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
    barHidden(this);
  }

  override render() {
    const failed = !!this.error || !!this.field;
    if (!this.always && !this.dirty && !failed) return nothing;
    const labels = this.labels;
    return html`<div class=${failed ? 'bar error' : 'bar'} role="region" aria-label=${labels.region}>
      <div class="state">${this.#state()}</div>
      <div class="buttons">
        ${this.field?.element()
          ? html`<lcm-button size="small" variant="outline" icon="arrow-right" @click=${() => this.jump()}>${labels.jump}</lcm-button>`
          : nothing}
        ${labels.discard
          ? html`<lcm-button variant="ghost" ?disabled=${this.busy || !this.dirty} @click=${() => emit(this, 'lcm-discard')}>${labels.discard}</lcm-button>`
          : nothing}
        <lcm-button variant="primary" ?busy=${this.busy} ?disabled=${!this.dirty || this.disabled} @click=${() => emit(this, 'lcm-save')}>${labels.save}</lcm-button>
      </div>
    </div>`;
  }
}
customElements.define('lcm-save-bar', LcmSaveBar);
