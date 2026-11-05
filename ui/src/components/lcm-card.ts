import { LitElement, css, html, nothing } from 'lit';
import { baseStyles } from './shared.js';

/**
 * <lcm-card heading="…" subtitle="…"> – Karte auf geschichteter Fläche.
 * Slots: Standard (Inhalt), `actions` (rechts im Kopf), `footer`. `variant="inset"` setzt sie eine Stufe tiefer.
 */
export class LcmCard extends LitElement {
  static override properties = {
    heading: { type: String },
    subtitle: { type: String },
    variant: { type: String, reflect: true },
    level: { type: Number },
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
  declare _hasFooter: boolean;

  constructor() {
    super();
    this.heading = '';
    this.subtitle = '';
    this.variant = 'default';
    this.level = 3;
    this._hasFooter = false;
  }

  override render() {
    const hasHead = this.heading !== '';
    return html`
      ${hasHead
        ? html`<header>
            <div class="titles">
              <div class="heading" role="heading" aria-level=${this.level}>${this.heading}</div>
              ${this.subtitle ? html`<p class="subtitle">${this.subtitle}</p>` : nothing}
            </div>
            <div class="actions"><slot name="actions"></slot></div>
          </header>`
        : nothing}
      <div class="body"><slot></slot></div>
      <footer ?hidden=${!this._hasFooter}>
        <slot name="footer" @slotchange=${(e: Event) => (this._hasFooter = (e.target as HTMLSlotElement).assignedNodes().length > 0)}></slot>
      </footer>
    `;
  }
}
customElements.define('lcm-card', LcmCard);
