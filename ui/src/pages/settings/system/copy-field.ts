import { LitElement, css, html, nothing } from 'lit';
import '../../../components/lcm-button';
import { baseStyles } from '../../../components/shared';
import { copyText } from '../../../lib/clipboard';
import { I18nController } from '../../../lib/controllers';
import { t } from '../../../lib/i18n';

const COPIED_MS = 2000;

/**
 * <lcm-copy-field label="…" value="…" ?block> – schreibgeschützter Wert in Festbreitenschrift mit Kopierknopf.
 * Mit `block` steht der Wert mehrzeilig (Schlüssel, Konfigurationsbeispiele), sonst in einer Zeile.
 */
export class LcmCopyField extends LitElement {
  static override properties = {
    label: { type: String },
    value: { type: String },
    block: { type: Boolean },
    _copied: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .label {
        display: block;
        margin-block-end: var(--space-1);
        color: var(--text-1);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .box {
        position: relative;
        display: flex;
        align-items: flex-start;
        gap: var(--space-3);
        padding: var(--space-3) var(--space-3) var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        box-shadow: inset 0 0 0 1px var(--line);
      }
      .value {
        flex: 1;
        min-inline-size: 0;
        margin: 0;
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        line-height: 1.5;
        color: var(--text-1);
        user-select: all;
        overflow-wrap: anywhere;
        white-space: pre-wrap;
      }
      :host(:not([block])) .value {
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
        overflow-wrap: normal;
      }
    `,
  ];

  declare label: string;
  declare value: string;
  declare block: boolean;
  declare _copied: boolean;
  #timer = 0;

  constructor() {
    super();
    this.label = '';
    this.value = '';
    this.block = false;
    this._copied = false;
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    window.clearTimeout(this.#timer);
  }

  async #copy(): Promise<void> {
    if (!(await copyText(this.value))) return;
    this._copied = true;
    window.clearTimeout(this.#timer);
    this.#timer = window.setTimeout(() => (this._copied = false), COPIED_MS);
  }

  override render() {
    return html`${this.label ? html`<span class="label">${this.label}</span>` : nothing}
      <div class="box">
        <pre class="value">${this.value}</pre>
        <lcm-button variant="ghost" size="small" icon=${this._copied ? 'check' : 'copy'} @click=${() => void this.#copy()}>
          ${this._copied ? t('settings.form.copied') : t('settings.form.copy')}
        </lcm-button>
      </div>`;
  }
}
customElements.define('lcm-copy-field', LcmCopyField);
