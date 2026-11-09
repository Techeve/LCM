import { LitElement, css, html, nothing } from 'lit';
import './lcm-button.js';
import { baseStyles, emit } from './shared.js';

/**
 * <lcm-list-pager page="2" page-size="50" total="120" range-label="51–100 von 120" previous-label="Zurück" next-label="Weiter"> –
 * Seitenwechsel unter einer Liste. Der Text der Spanne kommt von außen, weil er je Sprache und Seite anders lautet.
 * Meldet `lcm-page` mit { page }. Ohne Einträge (`total` 0) bleibt die Leiste leer.
 */
export class LcmListPager extends LitElement {
  static override properties = {
    page: { type: Number },
    pageSize: { type: Number, attribute: 'page-size' },
    total: { type: Number },
    rangeLabel: { type: String, attribute: 'range-label' },
    previousLabel: { type: String, attribute: 'previous-label' },
    nextLabel: { type: String, attribute: 'next-label' },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .pager {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
        margin-block-start: var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .buttons {
        display: flex;
        gap: var(--space-2);
      }
    `,
  ];

  declare page: number;
  declare pageSize: number;
  declare total: number;
  declare rangeLabel: string;
  declare previousLabel: string;
  declare nextLabel: string;

  constructor() {
    super();
    this.page = 1;
    this.pageSize = 50;
    this.total = 0;
    this.rangeLabel = '';
    this.previousLabel = '';
    this.nextLabel = '';
  }

  #go(page: number): void {
    emit(this, 'lcm-page', { page });
  }

  override render() {
    if (this.total <= 0) return nothing;
    const last = this.page * this.pageSize >= this.total;
    return html`<div class="pager">
      <span>${this.rangeLabel}</span>
      <div class="buttons">
        <lcm-button size="small" icon="chevron-left" ?disabled=${this.page <= 1} @click=${() => this.#go(this.page - 1)}>${this.previousLabel}</lcm-button>
        <lcm-button size="small" ?disabled=${last} @click=${() => this.#go(this.page + 1)}>${this.nextLabel}</lcm-button>
      </div>
    </div>`;
  }
}
customElements.define('lcm-list-pager', LcmListPager);
