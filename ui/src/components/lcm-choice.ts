import { LitElement, css, html, nothing } from 'lit';
import { icon, type IconName } from './icons.js';
import { baseStyles, emit, uid } from './shared.js';

export interface ChoiceOption {
  id: string;
  label: string;
  /** Kurze Folge der Wahl, erscheint unter der Beschriftung (nur bei `variant="cards"`). */
  description?: string;
  icon?: IconName;
  disabled?: boolean;
}

/**
 * <lcm-choice label="Verbindungsart" .options=${[…]} value="ssh" variant="cards"> – Auswahl aus wenigen Möglichkeiten
 * als Radiogruppe: `cards` mit Symbol und Beschreibung, `segmented` als schmale Leiste. Mit `stacked` stehen die
 * Karten untereinander (lange Beschreibungen). Pfeiltasten wechseln (native Radios). Meldet `lcm-change` { value }.
 */
export class LcmChoice extends LitElement {
  static override properties = {
    label: { type: String },
    options: { attribute: false },
    value: { type: String },
    variant: { type: String, reflect: true },
    stacked: { type: Boolean, reflect: true },
    disabled: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      fieldset {
        margin: 0;
        padding: 0;
        border: 0;
        min-inline-size: 0;
      }
      legend {
        margin-block-end: var(--space-2);
        padding: 0;
        color: var(--text-1);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .options {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
        gap: var(--space-2);
      }
      :host([stacked]) .options {
        grid-template-columns: minmax(0, 1fr);
      }
      label {
        position: relative;
        display: grid;
        grid-template-columns: auto minmax(0, 1fr);
        gap: var(--space-1) var(--space-3);
        align-content: start;
        padding: var(--space-3) var(--space-4);
        border: 1px solid var(--line);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        cursor: pointer;
        transition:
          border-color var(--dur-1) var(--ease),
          background var(--dur-1) var(--ease);
      }
      label:hover {
        border-color: var(--line-strong);
      }
      label.on {
        border-color: var(--accent);
        background: var(--accent-soft);
        box-shadow: inset 0 0 0 1px var(--accent);
      }
      label.off {
        cursor: not-allowed;
        opacity: 0.55;
      }
      label:has(input:focus-visible) {
        outline: 2px solid var(--focus-ring);
        outline-offset: 2px;
      }
      input {
        position: absolute;
        opacity: 0;
        pointer-events: none;
      }
      .icon {
        grid-row: span 2;
        margin-block-start: 0.1rem;
        color: var(--text-2);
      }
      label.on .icon {
        color: var(--accent-text);
      }
      .title {
        font-weight: var(--fw-semibold);
      }
      .text {
        grid-column: 2;
        color: var(--text-2);
        font-size: var(--fs-s);
        line-height: 1.45;
      }
      label.no-icon {
        grid-template-columns: minmax(0, 1fr);
      }
      label.no-icon .text {
        grid-column: 1;
      }
      :host([variant='segmented']) .options {
        display: inline-flex;
        flex-wrap: wrap;
        gap: 2px;
        padding: var(--tab-track-pad, 3px);
        border-radius: var(--tab-radius, var(--radius-m));
        background: var(--tab-track-bg, var(--surface-1));
      }
      :host([variant='segmented']) label {
        display: inline-flex;
        gap: var(--space-2);
        align-items: center;
        padding: var(--space-1) var(--space-3);
        border: 0;
        border-radius: calc(var(--tab-radius, var(--radius-m)) - 2px);
        background: transparent;
        font-size: var(--fs-s);
      }
      :host([variant='segmented']) label.on {
        background: var(--tab-active-bg, var(--surface-2));
        color: var(--tab-active-fg, var(--text-1));
        box-shadow: var(--tab-active-shadow, var(--elev-1));
      }
      :host([variant='segmented']) .icon {
        grid-row: auto;
        margin: 0;
      }
    `,
  ];

  declare label: string;
  declare options: ChoiceOption[];
  declare value: string;
  declare variant: 'cards' | 'segmented';
  declare stacked: boolean;
  declare disabled: boolean;
  private readonly name = uid('choice');

  constructor() {
    super();
    this.label = '';
    this.options = [];
    this.value = '';
    this.variant = 'cards';
    this.stacked = false;
    this.disabled = false;
  }

  #select(option: ChoiceOption): void {
    if (option.id === this.value) return;
    this.value = option.id;
    emit(this, 'lcm-change', { value: option.id });
  }

  #option(option: ChoiceOption) {
    const on = option.id === this.value;
    const off = this.disabled || Boolean(option.disabled);
    const cards = this.variant !== 'segmented';
    const classes = [on ? 'on' : '', off ? 'off' : '', option.icon ? '' : 'no-icon'].join(' ');
    return html`<label class=${classes}>
      <input
        type="radio"
        name=${this.name}
        .checked=${on}
        ?disabled=${off}
        @change=${() => this.#select(option)}
      />
      ${option.icon ? icon(option.icon) : nothing}
      <span class="title">${option.label}</span>
      ${cards && option.description ? html`<span class="text">${option.description}</span>` : nothing}
    </label>`;
  }

  override render() {
    return html`<fieldset>
      ${this.label ? html`<legend>${this.label}</legend>` : nothing}
      <div class="options">${this.options.map((option) => this.#option(option))}</div>
    </fieldset>`;
  }
}
customElements.define('lcm-choice', LcmChoice);
