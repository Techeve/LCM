import { LitElement, css, html, nothing } from 'lit';
import { fieldStyles, fieldWidthProperty, type FieldWidth } from './field-styles.js';
import { icon } from './icons.js';
import { baseStyles, emit, uid } from './shared.js';

export interface Option {
  value: string;
  label: string;
  hint?: string;
}

/**
 * <lcm-select label="Gruppe" .options=${[…]} .value=${…} width="m"> – natives Auswahlfeld im LCM-Look. Meldet `lcm-change` { value }.
 * `width` (xs, s, m, l, full; Vorgabe m) richtet die Breite nach den Einträgen, siehe `FieldWidth`.
 */
export class LcmSelect extends LitElement {
  static override properties = {
    width: fieldWidthProperty,
    label: { type: String },
    options: { attribute: false },
    value: { type: String },
    hint: { type: String },
    disabled: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    fieldStyles,
    css`
      select {
        appearance: none;
        cursor: pointer;
        padding-inline-end: var(--space-5);
      }
      .chev {
        position: absolute;
        inset-inline-end: var(--space-3);
        pointer-events: none;
        color: var(--text-2);
      }
      .control {
        position: relative;
      }
      option {
        background: var(--surface-overlay);
        color: var(--text-1);
      }
    `,
  ];

  /** Breite der Eingabefläche nach Inhalt, siehe `FieldWidth`. */
  declare width: FieldWidth | undefined;
  declare label: string;
  declare options: Option[];
  declare value: string;
  declare hint: string;
  declare disabled: boolean;
  private readonly selectId = uid('select');

  constructor() {
    super();
    this.label = '';
    this.options = [];
    this.value = '';
    this.hint = '';
    this.disabled = false;
  }

  override render() {
    return html`<div class="field">
      <label for=${this.selectId}>${this.label}</label>
      <div class="control">
        <select
          id=${this.selectId}
          ?disabled=${this.disabled}
          @change=${(e: Event) => {
            this.value = (e.target as HTMLSelectElement).value;
            emit(this, 'lcm-change', { value: this.value });
          }}
        >
          ${this.options.map((o) => html`<option value=${o.value} ?selected=${o.value === this.value}>${o.label}</option>`)}
        </select>
        <span class="chev">${icon('chevron-down')}</span>
      </div>
      ${this.hint ? html`<div class="hint">${this.hint}</div>` : nothing}
    </div>`;
  }
}
customElements.define('lcm-select', LcmSelect);
