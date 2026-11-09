import { LitElement, css, html, nothing } from 'lit';
import { icon } from './icons.js';
import { baseStyles, emit } from './shared.js';

/**
 * <lcm-card heading="…" subtitle="…"> – Karte auf geschichteter Fläche.
 * Slots: Standard (Inhalt), `actions` (rechts im Kopf), `footer`. `variant="inset"` setzt sie eine Stufe tiefer.
 * Mit `collapsible` klappt der Kopf den Inhalt ein und aus; `open` sagt, ob er offen ist. Meldet `lcm-toggle` { open }.
 */
export class LcmCard extends LitElement {
  static override properties = {
    heading: { type: String },
    subtitle: { type: String },
    variant: { type: String, reflect: true },
    level: { type: Number },
    collapsible: { type: Boolean, reflect: true },
    open: { type: Boolean, reflect: true },
    _hasFooter: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        border: 1px solid var(--line-muted);
        border-radius: var(--radius-m);
        background: var(--surface-1);
        color: var(--text-1);
      }
      :host([variant='inset']) {
        background: var(--surface-2);
      }
      header {
        display: flex;
        flex-wrap: wrap;
        align-items: flex-start;
        justify-content: space-between;
        gap: var(--space-4);
        padding: var(--space-card) var(--space-card) 0;
      }
      .titles {
        min-inline-size: 0;
      }
      :host([collapsible]) .titles {
        flex: 1;
        cursor: pointer;
      }
      :host([collapsible]:not([open])) header {
        padding-block-end: var(--space-card);
      }
      .toggle {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        padding: 0;
        border: 0;
        background: none;
        color: inherit;
        font: inherit;
        letter-spacing: inherit;
        text-align: start;
        cursor: pointer;
      }
      .toggle svg {
        flex: none;
        inline-size: 1rem;
        block-size: 1rem;
        color: var(--text-2);
        transform: rotate(-90deg);
        transition: transform var(--dur-1) var(--ease);
      }
      :host([open]) .toggle svg {
        transform: none;
      }
      .heading {
        margin: 0;
        font-size: var(--fs-l);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        line-height: 1.3;
      }
      .subtitle {
        margin: 0.15rem 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .body {
        padding: var(--space-card);
      }
      header + .body {
        padding-block-start: var(--space-4);
      }
      footer {
        padding: var(--space-3) var(--space-card);
        border-block-start: 1px solid var(--line);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .actions {
        display: flex;
        align-items: center;
        gap: var(--space-2);
      }
    `,
  ];

  declare heading: string;
  declare subtitle: string;
  declare variant: 'default' | 'inset' | 'flat';
  declare level: number;
  declare collapsible: boolean;
  declare open: boolean;
  declare _hasFooter: boolean;

  constructor() {
    super();
    this.heading = '';
    this.subtitle = '';
    this.variant = 'default';
    this.level = 3;
    this.collapsible = false;
    this.open = false;
    this._hasFooter = false;
  }

  #toggle(): void {
    this.open = !this.open;
    emit(this, 'lcm-toggle', { open: this.open });
  }

  #heading() {
    if (!this.collapsible) return this.heading;
    return html`<button class="toggle" type="button" aria-expanded=${String(this.open)}>${icon('chevron-down')}${this.heading}</button>`;
  }

  override render() {
    const hasHead = this.heading !== '';
    const closed = this.collapsible && !this.open;
    return html`
      ${hasHead
        ? html`<header>
            <div class="titles" @click=${this.collapsible ? () => this.#toggle() : nothing}>
              <div class="heading" role="heading" aria-level=${this.level}>${this.#heading()}</div>
              ${this.subtitle ? html`<p class="subtitle">${this.subtitle}</p>` : nothing}
            </div>
            <div class="actions"><slot name="actions"></slot></div>
          </header>`
        : nothing}
      <div class="body" ?hidden=${closed}><slot></slot></div>
      <footer ?hidden=${closed || !this._hasFooter}>
        <slot name="footer" @slotchange=${(e: Event) => (this._hasFooter = (e.target as HTMLSlotElement).assignedNodes().length > 0)}></slot>
      </footer>
    `;
  }
}
customElements.define('lcm-card', LcmCard);
