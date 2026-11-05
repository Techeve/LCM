import { LitElement, css, html, nothing } from 'lit';
import { icon } from './icons.js';
import { baseStyles } from './shared.js';

/**
 * <lcm-command label="Agent installieren" .value=${'sudo apt install lcm-agent'} copy-label="Kopieren"
 * copied-label="Kopiert"> – ein Befehl zum Abtippen oder Kopieren: Monospace, umbricht lange Zeilen, Knopf rechts.
 * Ohne Zwischenablage (kein HTTPS) bleibt der Text markierbar.
 */
export class LcmCommand extends LitElement {
  static override properties = {
    label: { type: String },
    value: { type: String },
    copyLabel: { type: String, attribute: 'copy-label' },
    copiedLabel: { type: String, attribute: 'copied-label' },
    _copied: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        gap: var(--space-1);
        min-inline-size: 0;
      }
      .label {
        color: var(--text-1);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .box {
        display: flex;
        gap: var(--space-2);
        align-items: flex-start;
        padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
        border-radius: var(--radius-m);
        background: var(--surface-1);
        box-shadow: inset 0 0 0 1px var(--line);
      }
      code {
        flex: 1;
        min-inline-size: 0;
        padding-block: 0.3rem;
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        line-height: 1.5;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
        user-select: all;
      }
      button {
        display: inline-flex;
        flex: none;
        gap: var(--space-1);
        align-items: center;
        padding: var(--space-1) var(--space-2);
        border: 0;
        border-radius: var(--radius-s);
        background: transparent;
        color: var(--text-2);
        font: inherit;
        font-size: var(--fs-s);
        cursor: pointer;
      }
      button:hover {
        background: var(--surface-3);
        color: var(--text-1);
      }
      button.done {
        color: var(--ok-text);
      }
    `,
  ];

  declare label: string;
  declare value: string;
  declare copyLabel: string;
  declare copiedLabel: string;
  declare _copied: boolean;
  #timer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    super();
    this.label = '';
    this.value = '';
    this.copyLabel = '';
    this.copiedLabel = '';
    this._copied = false;
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    clearTimeout(this.#timer);
  }

  async #copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(this.value);
    } catch {
      return; // ohne Zwischenablage-Recht: der Text ist markierbar
    }
    this._copied = true;
    clearTimeout(this.#timer);
    this.#timer = setTimeout(() => (this._copied = false), 2000);
  }

  override render() {
    return html`${this.label ? html`<span class="label">${this.label}</span>` : nothing}
      <div class="box">
        <code>${this.value}</code>
        <button type="button" class=${this._copied ? 'done' : ''} @click=${this.#copy}>
          ${icon(this._copied ? 'check' : 'copy')}<span>${this._copied ? this.copiedLabel : this.copyLabel}</span>
        </button>
      </div>`;
  }
}
customElements.define('lcm-command', LcmCommand);
