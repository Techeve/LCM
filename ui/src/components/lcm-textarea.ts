import { LitElement, css, html, nothing } from 'lit';
import { fieldStyles, fieldWidthProperty, type FieldWidth } from './field-styles.js';
import { baseStyles, emit, uid } from './shared.js';

/**
 * <lcm-textarea label="Nameserver" .value=${…} rows="5" mono hint="…" error="…"> – mehrzeiliges Textfeld
 * im Look der anderen Felder. Meldet `lcm-input` (jeder Tastendruck) und `lcm-change` (Übernahme), je mit { value }.
 * Anders als die einzeiligen Felder ist die Vorgabe `full`; `width` (m, l) begrenzt bei Bedarf, siehe `FieldWidth`.
 */
export class LcmTextarea extends LitElement {
  static override properties = {
    width: fieldWidthProperty,
    label: { type: String },
    value: { type: String },
    placeholder: { type: String },
    hint: { type: String },
    error: { type: String },
    rows: { type: Number },
    mono: { type: Boolean },
    disabled: { type: Boolean, reflect: true },
    invalid: { type: Boolean, reflect: true },
    maxlength: { type: Number },
  };
  static override styles = [
    baseStyles,
    fieldStyles,
    css`
      :host {
        --field-w: 100%;
      }
      .control {
        align-items: stretch;
        padding-block: var(--space-2);
      }
      textarea {
        flex: 1;
        min-inline-size: 0;
        padding: 0;
        border: 0;
        background: transparent;
        color: inherit;
        font: inherit;
        font-size: var(--fs-m);
        line-height: 1.5;
        outline: none;
        resize: vertical;
      }
      textarea.mono {
        font-family: var(--font-mono);
        font-size: var(--fs-s);
      }
      textarea::placeholder {
        color: var(--text-3);
      }
    `,
  ];

  /** Breite der Eingabefläche nach Inhalt, siehe `FieldWidth`. */
  declare width: FieldWidth | undefined;
  declare label: string;
  declare value: string;
  declare placeholder: string;
  declare hint: string;
  declare error: string;
  declare rows: number;
  declare mono: boolean;
  declare disabled: boolean;
  declare invalid: boolean;
  /** Grenze in Zeichen, siehe `LIMITS` in `lib/validate.ts`. */
  declare maxlength: number | undefined;
  private readonly areaId = uid('textarea');

  constructor() {
    super();
    this.label = '';
    this.value = '';
    this.placeholder = '';
    this.hint = '';
    this.error = '';
    this.rows = 4;
    this.mono = false;
    this.disabled = false;
    this.invalid = false;
  }

  override willUpdate(): void {
    this.invalid = this.error !== '';
  }

  override focus(): void {
    this.renderRoot.querySelector('textarea')?.focus();
  }

  override render() {
    const describedBy = this.error ? `${this.areaId}-e` : this.hint ? `${this.areaId}-h` : nothing;
    return html`<div class="field">
      <label for=${this.areaId}>${this.label}</label>
      <div class="control">
        <textarea
          id=${this.areaId}
          class=${this.mono ? 'mono' : ''}
          rows=${this.rows}
          maxlength=${this.maxlength ?? nothing}
          placeholder=${this.placeholder || nothing}
          spellcheck="false"
          .value=${this.value}
          ?disabled=${this.disabled}
          aria-invalid=${this.invalid ? 'true' : nothing}
          aria-describedby=${describedBy}
          @input=${(e: Event) => {
            this.value = (e.target as HTMLTextAreaElement).value;
            emit(this, 'lcm-input', { value: this.value });
          }}
          @change=${() => emit(this, 'lcm-change', { value: this.value })}
        ></textarea>
      </div>
      ${this.error
        ? html`<div class="error" id=${`${this.areaId}-e`}>${this.error}</div>`
        : this.hint
          ? html`<div class="hint" id=${`${this.areaId}-h`}>${this.hint}</div>`
          : nothing}
    </div>`;
  }
}
customElements.define('lcm-textarea', LcmTextarea);
