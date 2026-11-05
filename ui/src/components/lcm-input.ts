import { LitElement, html, nothing } from 'lit';
import { fieldStyles, fieldWidthProperty, type FieldWidth } from './field-styles.js';
import { icon, type IconName } from './icons.js';
import { baseStyles, emit, uid } from './shared.js';

/**
 * <lcm-input label="Name" .value=${…} hint="…" error="…"> – Textfeld mit Beschriftung, Hinweis und Fehler.
 * Meldet `lcm-input` (jeder Tastendruck) und `lcm-change` (Übernahme), je mit { value }.
 * `width` (xs, s, m, l, full; Vorgabe m) richtet die Breite nach dem Inhalt, siehe `FieldWidth`.
 * `maxlength` begrenzt die Eingabe in Zeichen (Grenzen in `lib/validate.ts`), `digits` lässt nur Ziffern zu.
 */
export class LcmInput extends LitElement {
  static override properties = {
    width: fieldWidthProperty,
    label: { type: String },
    value: { type: String },
    type: { type: String },
    placeholder: { type: String },
    hint: { type: String },
    error: { type: String },
    name: { type: String },
    icon: { type: String },
    autocomplete: { type: String },
    mono: { type: Boolean },
    disabled: { type: Boolean, reflect: true },
    required: { type: Boolean },
    invalid: { type: Boolean, reflect: true },
    maxlength: { type: Number },
    inputmode: { type: String },
    digits: { type: Boolean },
  };
  static override styles = [baseStyles, fieldStyles];

  /** Breite der Eingabefläche nach Inhalt, siehe `FieldWidth`. */
  declare width: FieldWidth | undefined;
  declare label: string;
  declare value: string;
  declare type: string;
  declare placeholder: string;
  declare hint: string;
  declare error: string;
  declare name: string;
  declare icon: IconName | '';
  declare autocomplete: string;
  declare mono: boolean;
  declare disabled: boolean;
  declare required: boolean;
  declare invalid: boolean;
  declare maxlength: number | undefined;
  declare inputmode: string;
  declare digits: boolean;
  private readonly inputId = uid('input');

  constructor() {
    super();
    this.label = '';
    this.value = '';
    this.type = 'text';
    this.placeholder = '';
    this.hint = '';
    this.error = '';
    this.name = '';
    this.icon = '';
    this.autocomplete = 'off';
    this.mono = false;
    this.disabled = false;
    this.required = false;
    this.invalid = false;
    this.inputmode = '';
    this.digits = false;
  }

  override willUpdate(): void {
    this.invalid = this.error !== '';
  }

  override focus(): void {
    this.renderRoot.querySelector('input')?.focus();
  }

  override render() {
    const describedBy = this.error ? `${this.inputId}-e` : this.hint ? `${this.inputId}-h` : nothing;
    return html`<div class="field">
      <label for=${this.inputId}>${this.label}</label>
      <div class="control">
        ${this.icon ? html`<span class="adorn">${icon(this.icon)}</span>` : nothing}
        <input
          id=${this.inputId}
          name=${this.name || nothing}
          type=${this.type}
          class=${this.mono ? 'mono' : ''}
          placeholder=${this.placeholder || nothing}
          autocomplete=${this.autocomplete}
          maxlength=${this.maxlength ?? nothing}
          inputmode=${this.inputmode || (this.digits ? 'numeric' : nothing)}
          .value=${this.value}
          ?disabled=${this.disabled}
          ?required=${this.required}
          aria-invalid=${this.invalid ? 'true' : nothing}
          aria-describedby=${describedBy}
          @input=${(e: Event) => {
            const input = e.target as HTMLInputElement;
            if (this.digits) input.value = input.value.replace(/\D+/g, '');
            this.value = input.value;
            emit(this, 'lcm-input', { value: this.value });
          }}
          @change=${() => emit(this, 'lcm-change', { value: this.value })}
        />
        <slot name="adorn"></slot>
      </div>
      ${this.error
        ? html`<div class="error" id=${`${this.inputId}-e`}>${this.error}</div>`
        : this.hint
          ? html`<div class="hint" id=${`${this.inputId}-h`}>${this.hint}</div>`
          : nothing}
    </div>`;
  }
}
customElements.define('lcm-input', LcmInput);
