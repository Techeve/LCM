import { LitElement, css, html, nothing } from 'lit';
import { fieldStyles, fieldWidthProperty, type FieldWidth } from './field-styles.js';
import { icon } from './icons.js';
import { baseStyles, emit, uid } from './shared.js';

/** Schätzt die Stärke von 0 (leer/sehr schwach) bis 4 (stark): Länge, Zeichenklassen, Wiederholungen. */
export function passwordStrength(value: string): number {
  if (value.length === 0) return 0;
  const classes = [/[a-z]/, /[A-Z]/, /\d/, /[^A-Za-z0-9]/].filter((re) => re.test(value)).length;
  const variety = new Set(value).size / value.length;
  let score = 0;
  if (value.length >= 8) score += 1;
  if (value.length >= 12) score += 1;
  if (classes >= 3) score += 1;
  if (value.length >= 16 && variety > 0.55) score += 1;
  if (variety < 0.4) score -= 1;
  return Math.max(0, Math.min(4, score));
}

const LEVELS = ['crit', 'crit', 'warn', 'ok', 'great'] as const;

/**
 * <lcm-password label="Passwort" .strengthLabels=${[…5 Texte…]} show-label="…" hide-label="…"> – Passwortfeld
 * mit Sichtbarkeitsknopf und Stärkeanzeige (Segmente plus Text). Meldet `lcm-input` { value, strength }.
 * Gibt es eine verbindliche Regel, setzt die Seite `score` (0 bis 4) und `problems` (fertige Texte der verletzten
 * Regeln); dann ersetzt `score` die einfache Schätzung, und die Gründe stehen unter dem Feld.
 * `width` (xs, s, m, l, full; Vorgabe m) gilt für Feld und Stärkeanzeige, siehe `FieldWidth`.
 */
export class LcmPassword extends LitElement {
  static override properties = {
    width: fieldWidthProperty,
    label: { type: String },
    value: { type: String },
    hint: { type: String },
    strengthTitle: { type: String, attribute: 'strength-title' },
    strengthLabels: { attribute: false },
    showLabel: { type: String, attribute: 'show-label' },
    hideLabel: { type: String, attribute: 'hide-label' },
    maxlength: { type: Number },
    score: { type: Number },
    problems: { attribute: false },
    _visible: { state: true },
  };
  static override styles = [
    baseStyles,
    fieldStyles,
    css`
      .meter {
        inline-size: var(--field-w);
        max-inline-size: 100%;
        display: flex;
        align-items: center;
        gap: var(--space-3);
        margin-block-start: var(--space-1);
      }
      .segments {
        display: grid;
        grid-template-columns: repeat(4, 1fr);
        gap: 4px;
        flex: 1;
      }
      .seg {
        block-size: 0.3rem;
        border-radius: var(--radius-pill);
        background: var(--surface-3);
        transition: background var(--dur-2) var(--ease);
      }
      .seg.on.crit {
        background: var(--crit);
      }
      .seg.on.warn {
        background: var(--warn);
      }
      .seg.on.ok {
        background: var(--ok);
      }
      .seg.on.great {
        background: var(--great);
      }
      .strength {
        min-inline-size: 7rem;
        color: var(--text-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
        text-align: end;
      }
      ul {
        display: grid;
        gap: var(--space-1);
        margin: var(--space-2) 0 0;
        padding-inline-start: 1.2rem;
        color: var(--warn-text);
        font-size: var(--fs-s);
      }
      input {
        font-family: var(--font-mono);
        letter-spacing: 0.02em;
      }
    `,
  ];

  /** Breite der Eingabefläche nach Inhalt, siehe `FieldWidth`. */
  declare width: FieldWidth | undefined;
  declare label: string;
  declare value: string;
  declare hint: string;
  declare strengthTitle: string;
  declare strengthLabels: string[];
  declare showLabel: string;
  declare hideLabel: string;
  /** Grenze in Zeichen. */
  declare maxlength: number | undefined;
  /** Stärke aus einer verbindlichen Regel; ohne Wert schätzt die Komponente selbst. */
  declare score: number | undefined;
  declare problems: string[];
  declare _visible: boolean;
  private readonly inputId = uid('password');

  constructor() {
    super();
    this.label = '';
    this.value = '';
    this.hint = '';
    this.strengthTitle = '';
    this.strengthLabels = [];
    this.showLabel = '';
    this.hideLabel = '';
    this.problems = [];
    this._visible = false;
  }

  override render() {
    const strength = this.score ?? passwordStrength(this.value);
    const level = LEVELS[strength]!;
    return html`<div class="field">
      <label for=${this.inputId}>${this.label}</label>
      <div class="control">
        <input
          id=${this.inputId}
          type=${this._visible ? 'text' : 'password'}
          autocomplete="new-password"
          maxlength=${this.maxlength ?? nothing}
          .value=${this.value}
          @input=${(e: Event) => {
            this.value = (e.target as HTMLInputElement).value;
            emit(this, 'lcm-input', { value: this.value, strength: passwordStrength(this.value) });
          }}
        />
        <button class="icon-btn" type="button" aria-pressed=${String(this._visible)} aria-label=${this._visible ? this.hideLabel : this.showLabel} @click=${() => (this._visible = !this._visible)}>
          ${icon(this._visible ? 'eye-off' : 'eye')}
        </button>
      </div>
      <div class="meter">
        <div class="segments" role="meter" aria-label=${this.strengthTitle} aria-valuemin="0" aria-valuemax="4" aria-valuenow=${strength} aria-valuetext=${this.strengthLabels[strength] ?? ''}>
          ${[1, 2, 3, 4].map((n) => html`<span class=${`seg ${strength >= n ? `on ${level}` : ''}`}></span>`)}
        </div>
        <span class="strength">${this.value ? this.strengthLabels[strength] : ''}</span>
      </div>
      ${this.hint ? html`<div class="hint">${this.hint}</div>` : nothing}
      ${this.problems.length ? html`<ul aria-live="polite">${this.problems.map((problem) => html`<li>${problem}</li>`)}</ul>` : nothing}
    </div>`;
  }
}
customElements.define('lcm-password', LcmPassword);
