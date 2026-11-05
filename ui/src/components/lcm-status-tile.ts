import { LitElement, css, html, nothing } from 'lit';
import './lcm-ampel.js';
import { baseStyles, levelStyles, type StatusLevel } from './shared.js';

/**
 * <lcm-status-tile level="warn" label="Warnung" count="4" href="/servers?status=warn" hint="…"> –
 * Statuskachel fürs Dashboard: große Ampel, Zahl und Text; führt zur gefilterten Serverliste.
 */
export class LcmStatusTile extends LitElement {
  static override properties = {
    level: { type: String, reflect: true },
    label: { type: String },
    count: { type: Number },
    hint: { type: String },
    href: { type: String },
  };
  static override styles = [
    baseStyles,
    levelStyles,
    css`
      :host {
        display: block;
      }
      a {
        position: relative;
        display: grid;
        grid-template-columns: 1fr auto;
        align-items: start;
        align-content: start;
        gap: var(--space-1) var(--space-3);
        block-size: 100%;
        padding: var(--space-card);
        border: 1px solid transparent;
        border-radius: var(--radius-l);
        background:
          radial-gradient(18rem 9rem at 100% 0%, color-mix(in srgb, var(--c) 14%, transparent), transparent 70%),
          var(--surface-1);
        box-shadow:
          var(--ring-card),
          var(--elev-1);
        color: var(--text-1);
        text-decoration: none;
        transition:
          transform var(--dur-2) var(--ease),
          box-shadow var(--dur-2) var(--ease);
      }
      a:hover {
        transform: translateY(-2px);
        box-shadow:
          var(--ring-card),
          var(--elev-2);
      }
      .count {
        grid-column: 1;
        font-size: var(--fs-3xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        font-variant-numeric: tabular-nums;
        line-height: 1;
      }
      lcm-ampel {
        grid-column: 2;
        grid-row: 1 / span 2;
      }
      .label {
        grid-column: 1;
        color: var(--c-text);
        font-weight: var(--fw-semibold);
      }
      .hint {
        grid-column: 1 / -1;
        margin-block-start: var(--space-2);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare level: StatusLevel;
  declare label: string;
  declare count: number;
  declare hint: string;
  declare href: string;

  constructor() {
    super();
    this.level = 'ok';
    this.label = '';
    this.count = 0;
    this.hint = '';
    this.href = '#';
  }

  override render() {
    return html`<a href=${this.href}>
      <span class="count">${new Intl.NumberFormat(document.documentElement.lang || undefined).format(this.count)}</span>
      <lcm-ampel level=${this.level} size="xl"></lcm-ampel>
      <span class="label">${this.label}</span>
      ${this.hint ? html`<span class="hint">${this.hint}</span>` : nothing}
    </a>`;
  }
}
customElements.define('lcm-status-tile', LcmStatusTile);
