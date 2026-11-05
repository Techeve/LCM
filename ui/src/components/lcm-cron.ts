import { LitElement, css, html, nothing } from 'lit';
import { fieldStyles, fieldWidthProperty, type FieldWidth } from './field-styles.js';
import { nextRuns, parseCron } from './cron.js';
import { LIMITS } from '../lib/validate.js';
import { baseStyles, emit } from './shared.js';

/** Zeichen je Feld, damit der ganze Ausdruck mit vier Leerzeichen die Grenze `LIMITS.cron` nicht übersteigt. */
const FIELD_MAX = Math.floor((LIMITS.cron - 4) / 5);

export interface CronPreset {
  label: string;
  value: string;
}

export interface CronLabels {
  presets: string;
  fields: [string, string, string, string, string];
  expression: string;
  next: string;
  invalid: string;
}

/**
 * <lcm-cron .value=${'30 3 * * 0'} .presets=${[…]} .labels=${…} description="…"> – Cron-Baukasten (Skizze):
 * Vorlagen, fünf Felder, Ausdruck, menschenlesbare Beschreibung (kommt vom Aufrufer) und die nächsten Läufe.
 * Meldet `lcm-change` { value }. `width` (s, m, l, full; Vorgabe l) begrenzt den ganzen Baukasten, siehe `FieldWidth`.
 */
export class LcmCron extends LitElement {
  static override properties = {
    width: fieldWidthProperty,
    value: { type: String },
    presets: { attribute: false },
    labels: { attribute: false },
    description: { type: String },
    locale: { type: String },
    now: { attribute: false },
  };
  static override styles = [
    baseStyles,
    fieldStyles,
    css`
      :host {
        --field-w: 30rem;
      }
      .cron {
        inline-size: var(--field-w);
        max-inline-size: 100%;
      }
      .cron .control {
        inline-size: auto;
      }
      .grid {
        display: grid;
        grid-template-columns: repeat(5, minmax(0, 1fr));
        gap: var(--space-2);
      }
      .grid input {
        inline-size: 100%;
        font-family: var(--font-mono);
        text-align: center;
      }
      .grid .control {
        padding-inline: var(--space-2);
      }
      .presets {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin-block-end: var(--space-3);
      }
      .chip {
        padding: 0.25rem 0.75rem;
        border: 0;
        border-radius: var(--radius-pill);
        background: var(--surface-2);
        color: var(--text-1);
        font-size: var(--fs-s);
        cursor: pointer;
      }
      .chip:hover {
        background: var(--surface-3);
      }
      .chip[aria-pressed='true'] {
        background: var(--accent-soft);
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
      }
      .result {
        margin-block-start: var(--space-3);
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        font-size: var(--fs-s);
      }
      code {
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        color: var(--text-1);
      }
      .desc {
        margin: 0.25rem 0 0;
        color: var(--text-1);
        font-weight: var(--fw-medium);
      }
      ol {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-1) var(--space-4);
        margin: var(--space-2) 0 0;
        padding: 0;
        color: var(--text-2);
        list-style: none;
        font-variant-numeric: tabular-nums;
      }
      .head {
        color: var(--text-2);
        font-weight: var(--fw-medium);
      }
    `,
  ];

  /** Breite der Eingabefläche nach Inhalt, siehe `FieldWidth`. */
  declare width: FieldWidth | undefined;
  declare value: string;
  declare presets: CronPreset[];
  declare labels: CronLabels;
  declare description: string;
  declare locale: string;
  declare now: Date | undefined;

  constructor() {
    super();
    this.value = '0 3 * * *';
    this.presets = [];
    this.labels = { presets: '', fields: ['', '', '', '', ''], expression: '', next: '', invalid: '' };
    this.description = '';
    this.locale = 'de';
    this.now = undefined;
  }

  private parts(): string[] {
    const parts = this.value.trim().split(/\s+/);
    while (parts.length < 5) parts.push('*');
    return parts.slice(0, 5);
  }

  private setValue(value: string): void {
    this.value = value;
    emit(this, 'lcm-change', { value });
  }

  private setField(index: number, text: string): void {
    const parts = this.parts();
    parts[index] = text.trim() || '*';
    this.setValue(parts.join(' '));
  }

  private renderRuns() {
    const runs = nextRuns(this.value, this.now ?? new Date(), 3);
    const format = new Intl.DateTimeFormat(this.locale, { weekday: 'short', day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
    return html`<span class="head">${this.labels.next}</span>
      <ol>${runs.map((d) => html`<li>${format.format(d)}</li>`)}</ol>`;
  }

  override render() {
    const valid = parseCron(this.value) !== null;
    return html`<div class="field cron">
      <span class="label">${this.labels.presets}</span>
      <div class="presets" role="group" aria-label=${this.labels.presets}>
        ${this.presets.map((p) => html`<button class="chip" type="button" aria-pressed=${String(p.value === this.value)} @click=${() => this.setValue(p.value)}>${p.label}</button>`)}
      </div>
      <div class="grid">
        ${this.parts().map(
          (part, i) => html`<div class="field">
            <label for=${`f${i}`}>${this.labels.fields[i]}</label>
            <div class="control"><input id=${`f${i}`} maxlength=${FIELD_MAX} .value=${part} aria-invalid=${valid ? nothing : 'true'} @change=${(e: Event) => this.setField(i, (e.target as HTMLInputElement).value)} /></div>
          </div>`,
        )}
      </div>
      <div class="result">
        ${valid
          ? html`<span class="head">${this.labels.expression}</span> <code>${this.parts().join(' ')}</code>
              ${this.description ? html`<p class="desc">${this.description}</p>` : nothing} ${this.renderRuns()}`
          : html`<span class="error">${this.labels.invalid}</span>`}
      </div>
    </div>`;
  }
}
customElements.define('lcm-cron', LcmCron);
