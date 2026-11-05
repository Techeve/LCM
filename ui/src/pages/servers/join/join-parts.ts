import { LitElement, css, html, nothing } from 'lit';
import '../../../components/lcm-button';
import '../../../components/lcm-command';
import '../../../components/lcm-notice';
import '../../../components/lcm-status';
import { baseStyles, emit } from '../../../components/shared';
import { I18nController } from '../../../lib/controllers';
import { t } from '../../../lib/i18n';
import { formStyles } from '../../account/form-styles';
import type { FieldProblem } from './join-model';

/** Text zu einem Feldfehler, nur nach dem ersten Versuch weiterzugehen. */
export function problemText(problem: FieldProblem | undefined, tried: boolean): string {
  if (!tried || !problem) return '';
  if (problem === 'port') return t('joinWizard.problem.port');
  if (problem === 'host') return t('joinWizard.problem.host');
  if (problem === 'long') return t('joinWizard.problem.long');
  if (problem === 'line') return t('check.oneLine');
  return t('joinWizard.problem.required');
}

/** <lcm-join-steps .steps=${['Verbindung', …]} current="1"> – Schrittanzeige des Assistenten. */
export class LcmJoinSteps extends LitElement {
  static override properties = { steps: { attribute: false }, current: { type: Number } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      ol {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2) var(--space-5);
        margin: 0;
        padding: 0;
        list-style: none;
        counter-reset: step;
      }
      li {
        display: inline-flex;
        gap: var(--space-2);
        align-items: center;
        color: var(--text-3);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
        counter-increment: step;
      }
      li::before {
        content: counter(step);
        display: grid;
        place-items: center;
        inline-size: 1.6rem;
        block-size: 1.6rem;
        border-radius: 50%;
        background: var(--surface-3);
        color: var(--text-2);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
      }
      li.done {
        color: var(--text-2);
      }
      li.done::before {
        content: '✓';
        background: var(--ok-soft);
        color: var(--ok-text);
      }
      li.now {
        color: var(--text-1);
      }
      li.now::before {
        background: var(--accent-solid);
        color: var(--text-on-accent);
      }
    `,
  ];

  declare steps: string[];
  declare current: number;

  constructor() {
    super();
    this.steps = [];
    this.current = 0;
  }

  override render() {
    return html`<ol>
      ${this.steps.map(
        (label, index) =>
          html`<li class=${index < this.current ? 'done' : index === this.current ? 'now' : ''} aria-current=${index === this.current ? 'step' : 'false'}>
            ${label}
          </li>`,
      )}
    </ol>`;
  }
}
customElements.define('lcm-join-steps', LcmJoinSteps);

/**
 * <lcm-join-fingerprint heading="…" key-type="ssh-ed25519" fingerprint="SHA256:…" hint="…" verify-command="…"
 * confirm-label="…"> – Fingerprint des Ziels groß zum Vergleichen, mit Befehl für die Gegenprobe. Erst die Bestätigung
 * führt weiter; danach prüft das Backend bei jeder Verbindung genau diesen Wert. Meldet `lcm-back` und `lcm-confirm`.
 */
export class LcmJoinFingerprint extends LitElement {
  static override properties = {
    heading: { type: String },
    keyType: { type: String, attribute: 'key-type' },
    fingerprint: { type: String },
    hint: { type: String },
    verifyLabel: { type: String, attribute: 'verify-label' },
    verifyCommand: { type: String, attribute: 'verify-command' },
    confirmLabel: { type: String, attribute: 'confirm-label' },
    busy: { type: Boolean },
  };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
      }
      .print {
        display: grid;
        gap: var(--space-1);
        padding: var(--space-4);
        border-radius: var(--radius-l);
        background: var(--surface-1);
        box-shadow: inset 0 0 0 1px var(--line-strong);
      }
      .type {
        color: var(--text-2);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
      }
      .value {
        font-family: var(--font-mono);
        font-size: var(--fs-l);
        font-weight: var(--fw-medium);
        letter-spacing: 0.02em;
        overflow-wrap: anywhere;
        user-select: all;
      }
    `,
  ];

  declare heading: string;
  declare keyType: string;
  declare fingerprint: string;
  declare hint: string;
  declare verifyLabel: string;
  declare verifyCommand: string;
  declare confirmLabel: string;
  declare busy: boolean;

  constructor() {
    super();
    this.heading = '';
    this.keyType = '';
    this.fingerprint = '';
    this.hint = '';
    this.verifyLabel = '';
    this.verifyCommand = '';
    this.confirmLabel = '';
    this.busy = false;
    new I18nController(this);
  }

  override render() {
    return html`<div class="stack">
      <h2>${this.heading}</h2>
      <div class="print">
        ${this.keyType ? html`<span class="type">${this.keyType}</span>` : nothing}
        <span class="value">${this.fingerprint}</span>
      </div>
      ${this.verifyCommand
        ? html`<lcm-command
            label=${this.verifyLabel}
            .value=${this.verifyCommand}
            copy-label=${t('joinWizard.copy')}
            copied-label=${t('joinWizard.copied')}
          ></lcm-command>`
        : nothing}
      <lcm-notice tone="warn">${this.hint}</lcm-notice>
      <div class="actions">
        <lcm-button variant="ghost" icon="chevron-left" ?disabled=${this.busy} @click=${() => emit(this, 'lcm-back')}>${t('joinWizard.back')}</lcm-button>
        <lcm-button variant="primary" icon="check" ?busy=${this.busy} @click=${() => emit(this, 'lcm-confirm')}>${this.confirmLabel}</lcm-button>
      </div>
    </div>`;
  }
}
customElements.define('lcm-join-fingerprint', LcmJoinFingerprint);

/**
 * <lcm-join-progress heading="…" .items=${[…]}> – läuft gerade: was LCM auf dem Ziel tut. Das Backend meldet keine
 * Zwischenschritte, deshalb läuft der Block als Ganzes, nicht Punkt für Punkt.
 */
export class LcmJoinProgress extends LitElement {
  static override properties = { heading: { type: String }, items: { attribute: false } };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: block;
      }
      ul {
        display: grid;
        gap: var(--space-2);
        margin: 0;
        padding-inline-start: 1.2rem;
        color: var(--text-2);
        line-height: 1.5;
      }
    `,
  ];

  declare heading: string;
  declare items: string[];

  constructor() {
    super();
    this.heading = '';
    this.items = [];
  }

  override render() {
    return html`<div class="stack" role="status" aria-live="polite">
      <lcm-status level="running" label=${this.heading} variant="pill"></lcm-status>
      <ul>
        ${this.items.map((item) => html`<li>${item}</li>`)}
      </ul>
    </div>`;
  }
}
customElements.define('lcm-join-progress', LcmJoinProgress);
