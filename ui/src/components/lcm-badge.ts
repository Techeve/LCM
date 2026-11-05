import { LitElement, css, html } from 'lit';
import { baseStyles } from './shared.js';

/** <lcm-badge tone="warn">3</lcm-badge> – kleine Marke für Zähler und Kurzinfos. */
export class LcmBadge extends LitElement {
  static override properties = { tone: { type: String, reflect: true } };
  static override styles = [
    baseStyles,
    css`
      :host {
        --b-bg: var(--surface-3);
        --b-fg: var(--text-1);
        display: inline-flex;
        align-items: center;
        min-inline-size: 1.4rem;
        padding: 0.05rem 0.5rem;
        border-radius: var(--radius-pill);
        background: var(--b-bg);
        color: var(--b-fg);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        font-variant-numeric: tabular-nums;
        line-height: 1.5;
        justify-content: center;
      }
      :host([tone='accent']) {
        --b-bg: var(--accent-soft);
        --b-fg: var(--accent-text);
      }
      :host([tone='ok']) {
        --b-bg: var(--ok-soft);
        --b-fg: var(--ok-text);
      }
      :host([tone='warn']) {
        --b-bg: var(--warn-soft);
        --b-fg: var(--warn-text);
      }
      :host([tone='crit']) {
        --b-bg: var(--crit-soft);
        --b-fg: var(--crit-text);
      }
      :host([tone='info']) {
        --b-bg: var(--info-soft);
        --b-fg: var(--info-text);
      }
    `,
  ];

  declare tone: 'neutral' | 'accent' | 'ok' | 'warn' | 'crit' | 'info';

  constructor() {
    super();
    this.tone = 'neutral';
  }

  override render() {
    return html`<slot></slot>`;
  }
}
customElements.define('lcm-badge', LcmBadge);
