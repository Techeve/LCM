import { LitElement, css, html, nothing } from 'lit';
import { baseStyles } from '../../../components/shared';

/**
 * <lcm-settings-cell sub="Beschreibung">Name <lcm-badge>…</lcm-badge></lcm-settings-cell> – Zelle mit Hauptzeile und
 * gedämpfter Unterzeile für `lcm-data-table` (die Tabelle stylt eigene Zellen nicht). Lange Texte enden mit „…“.
 */
export class LcmSettingsCell extends LitElement {
  static override properties = { sub: { type: String }, mono: { type: Boolean, reflect: true } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
        max-inline-size: 15rem;
      }
      :host([mono]) {
        max-inline-size: 13rem;
      }
      .main {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: 0.1rem var(--space-2);
        min-inline-size: 0;
      }
      .main,
      .sub {
        overflow: hidden;
        text-overflow: ellipsis;
      }
      :host([mono]) .main {
        font-family: var(--font-mono);
        font-size: var(--fs-s);
      }
      .sub {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      ::slotted(a) {
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      ::slotted(a:hover) {
        text-decoration: underline;
      }
    `,
  ];

  declare sub: string;
  declare mono: boolean;

  constructor() {
    super();
    this.sub = '';
    this.mono = false;
  }

  override render() {
    return html`<div class="main"><slot></slot></div>
      ${this.sub ? html`<div class="sub" title=${this.sub}>${this.sub}</div>` : nothing}`;
  }
}
customElements.define('lcm-settings-cell', LcmSettingsCell);

/** <lcm-settings-actions>…Knöpfe…</lcm-settings-actions> – Knöpfe am Zeilenende, rechtsbündig mit Abstand. */
export class LcmSettingsActions extends LitElement {
  static override styles = css`
    :host {
      display: flex;
      justify-content: flex-end;
      align-items: center;
      gap: var(--space-1);
    }
  `;

  override render() {
    return html`<slot></slot>`;
  }
}
customElements.define('lcm-settings-actions', LcmSettingsActions);
