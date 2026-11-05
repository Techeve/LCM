import { LitElement, css, html } from 'lit';
import { baseStyles } from './shared.js';

/**
 * <lcm-meter value="72" label="Speicher belegt"> – schmaler Balken mit Prozentwert (Auslastung, Speicher).
 * Färbt sich ab 75 % gelb und ab 90 % rot; `level` überschreibt das. `label` ist die Beschreibung für Screenreader.
 */
export class LcmMeter extends LitElement {
  static override properties = {
    value: { type: Number },
    label: { type: String },
    level: { type: String },
    hideValue: { type: Boolean, attribute: 'hide-value' },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        min-inline-size: 7rem;
        vertical-align: middle;
      }
      .track {
        flex: 1;
        block-size: 0.4rem;
        border-radius: var(--radius-pill);
        background: var(--surface-3);
        overflow: hidden;
      }
      svg {
        display: block;
        inline-size: 100%;
        block-size: 100%;
      }
      rect {
        fill: var(--accent);
      }
      :host([data-tone='warn']) rect {
        fill: var(--warn);
      }
      :host([data-tone='crit']) rect {
        fill: var(--crit);
      }
      .value {
        min-inline-size: 2.75rem;
        color: var(--text-2);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        font-variant-numeric: tabular-nums;
        text-align: end;
      }
    `,
  ];

  declare value: number;
  declare label: string;
  declare level: '' | 'ok' | 'warn' | 'crit';
  declare hideValue: boolean;

  constructor() {
    super();
    this.value = 0;
    this.label = '';
    this.level = '';
    this.hideValue = false;
  }

  override willUpdate(): void {
    const tone = this.level || (this.value >= 90 ? 'crit' : this.value >= 75 ? 'warn' : 'ok');
    this.dataset.tone = tone;
  }

  override render() {
    const value = Math.min(Math.max(this.value, 0), 100);
    const text = new Intl.NumberFormat(document.documentElement.lang || undefined, { style: 'percent' }).format(value / 100);
    return html`<div class="track" role="meter" aria-label=${this.label} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${value} aria-valuetext=${text}>
        <svg viewBox="0 0 100 1" preserveAspectRatio="none" aria-hidden="true"><rect x="0" y="0" width=${value} height="1"></rect></svg>
      </div>
      ${this.hideValue ? '' : html`<span class="value">${text}</span>`}`;
  }
}
customElements.define('lcm-meter', LcmMeter);
