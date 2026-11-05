import { LitElement, css, html } from 'lit';
import { icon, type IconName } from './icons.js';
import { baseStyles } from './shared.js';

/**
 * <lcm-empty-state icon="server" heading="…" text="…" level="3"> – Leerzustand: erklärt die Seite, Slot `action` für den ersten Schritt.
 * `level` ist die Überschriftenebene (1 bis 3), damit die Gliederung der Seite stimmt.
 */
export class LcmEmptyState extends LitElement {
  static override properties = {
    icon: { type: String },
    heading: { type: String },
    text: { type: String },
    level: { type: Number },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        justify-items: center;
        gap: var(--space-3);
        padding: var(--space-7) var(--space-5);
        text-align: center;
      }
      .badge {
        display: grid;
        place-items: center;
        inline-size: 4rem;
        block-size: 4rem;
        border-radius: 50%;
        background: radial-gradient(circle at 30% 25%, var(--accent-soft), var(--surface-2));
        color: var(--accent-text);
        font-size: 1.75rem;
      }
      h1,
      h2,
      h3 {
        margin: var(--space-2) 0 0;
        font-size: var(--fs-l);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      p {
        max-inline-size: 34rem;
        margin: 0;
        color: var(--text-2);
      }
      .action {
        margin-block-start: var(--space-3);
        display: flex;
        gap: var(--space-2);
        flex-wrap: wrap;
        justify-content: center;
      }
    `,
  ];

  declare icon: IconName;
  declare heading: string;
  declare text: string;
  declare level: 1 | 2 | 3;

  constructor() {
    super();
    this.icon = 'server';
    this.heading = '';
    this.text = '';
    this.level = 3;
  }

  private renderHeading() {
    if (this.level === 1) return html`<h1>${this.heading}</h1>`;
    return this.level === 2 ? html`<h2>${this.heading}</h2>` : html`<h3>${this.heading}</h3>`;
  }

  override render() {
    return html`<span class="badge">${icon(this.icon)}</span>
      ${this.renderHeading()}
      <p>${this.text}</p>
      <div class="action"><slot name="action"></slot></div>`;
  }
}
customElements.define('lcm-empty-state', LcmEmptyState);
