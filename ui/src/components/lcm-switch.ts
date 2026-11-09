import { LitElement, css, html, nothing } from 'lit';
import { HoverCard } from './floating.js';
import { baseStyles, emit, uid } from './shared.js';

/**
 * <lcm-switch label="…" description="…" .checked=${…} on-text="…" off-text="…"> – Schalter (role=switch).
 * Mit `on-text`/`off-text` erscheint beim Überfahren eine Erklärblase: was „an“ und was „aus“ bewirkt.
 * Meldet `lcm-change` { checked }.
 */
export class LcmSwitch extends LitElement {
  static override properties = {
    label: { type: String },
    description: { type: String },
    checked: { type: Boolean, reflect: true },
    disabled: { type: Boolean, reflect: true },
    onText: { type: String, attribute: 'on-text' },
    offText: { type: String, attribute: 'off-text' },
    onLabel: { type: String, attribute: 'on-label' },
    offLabel: { type: String, attribute: 'off-label' },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .row {
        display: flex;
        align-items: flex-start;
        gap: var(--space-3);
        inline-size: 100%;
        padding: 0;
        border: 0;
        background: none;
        text-align: start;
        cursor: pointer;
      }
      .row:disabled {
        opacity: 0.55;
        cursor: not-allowed;
      }
      .track {
        position: relative;
        flex: none;
        inline-size: 2.5rem;
        block-size: 1.5rem;
        margin-block-start: 0.05rem;
        border-radius: var(--radius-pill);
        background: var(--surface-3);
        box-shadow: inset 0 0 0 1px var(--line-strong);
        transition: background var(--dur-2) var(--ease);
      }
      .thumb {
        position: absolute;
        inset-block-start: 3px;
        inset-inline-start: 3px;
        inline-size: 1.125rem;
        block-size: 1.125rem;
        border-radius: 50%;
        background: var(--text-2);
        transition:
          transform var(--dur-2) var(--ease),
          background var(--dur-2) var(--ease);
      }
      :host([checked]) .track {
        background: var(--accent);
        box-shadow: none;
      }
      :host([checked]) .thumb {
        /* Weg des Knopfs: Spurbreite minus Knopf minus beide Ränder. */
        transform: translateX(calc(2.5rem - 1.125rem - 6px));
        background: var(--text-on-accent);
      }
      .texts {
        display: flex;
        flex-direction: column;
        min-inline-size: 0;
      }
      .label {
        color: var(--text-1);
        font-size: var(--fs-m);
        font-weight: var(--fw-medium);
      }
      .desc {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .bubble {
        inline-size: min(22rem, calc(100vw - 16px));
        padding: var(--space-3) var(--space-4);
        border: 1px solid transparent;
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-2);
        box-shadow: var(--elev-2);
        font-size: var(--fs-s);
        line-height: 1.5;
      }
      .bubble p {
        margin: 0;
      }
      .bubble p + p {
        margin-block-start: var(--space-2);
      }
      .bubble strong {
        color: var(--text-1);
      }
    `,
  ];

  declare label: string;
  declare description: string;
  declare checked: boolean;
  declare disabled: boolean;
  declare onText: string;
  declare offText: string;
  declare onLabel: string;
  declare offLabel: string;
  private readonly bubbleId = uid('switch-help');
  readonly hover = new HoverCard(this, {
    popover: () => this.renderRoot.querySelector<HTMLElement>('.bubble'),
    anchor: () => this.renderRoot.querySelector('.track'),
  });

  constructor() {
    super();
    this.label = '';
    this.description = '';
    this.checked = false;
    this.disabled = false;
    this.onText = '';
    this.offText = '';
    this.onLabel = '';
    this.offLabel = '';
  }

  private toggle(): void {
    this.checked = !this.checked;
    emit(this, 'lcm-change', { checked: this.checked });
  }

  override render() {
    const help = this.onText || this.offText;
    return html`<button
        class="row"
        type="button"
        role="switch"
        aria-checked=${String(this.checked)}
        aria-describedby=${help ? this.bubbleId : nothing}
        ?disabled=${this.disabled}
        @click=${this.toggle}
      >
        <span class="track"><span class="thumb"></span></span>
        <span class="texts"><span class="label">${this.label}</span>${this.description ? html`<span class="desc">${this.description}</span>` : nothing}</span>
      </button>
      ${help
        ? html`<div id=${this.bubbleId} class="bubble" popover="manual" role="tooltip">
            ${this.onText ? html`<p><strong>${this.onLabel}</strong> ${this.onText}</p>` : nothing}
            ${this.offText ? html`<p><strong>${this.offLabel}</strong> ${this.offText}</p>` : nothing}
          </div>`
        : nothing}`;
  }
}
customElements.define('lcm-switch', LcmSwitch);
