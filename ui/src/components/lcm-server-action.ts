import { LitElement, css, html, nothing } from 'lit';
import { buttonStyles, type ButtonVariant } from './button-styles.js';
import { HoverCard } from './floating.js';
import { icon, type IconName } from './icons.js';
import './lcm-skeleton.js';
import { baseStyles, uid } from './shared.js';

/** Was beim Ausführen tatsächlich passiert. Kommt im echten LCM aus der Befehlsvorschau des Backends. */
export interface ActionPreview {
  commands?: string[];
  packages?: string[];
  reboot?: boolean;
  reversible?: boolean;
  duration?: string;
  docsHref?: string;
  loading?: boolean;
}

export interface ActionLabels {
  meaning: string;
  executes: string;
  commands: string;
  packages: string;
  reboot: string;
  reversible: string;
  duration: string;
  yes: string;
  no: string;
  docs: string;
  loading: string;
}

/**
 * <lcm-server-action label="Updates einspielen" help="…"> – Knopf für Aktionen, die auf Servern etwas auslösen.
 * `help` ist Pflicht: Nach 400 ms Überfahren oder bei Tastaturfokus erscheint die große Erklärblase
 * (Was passiert, was heißt das für dich, was wird ausgeführt). Ohne `help` wirft die Komponente einen Fehler.
 * Das Ereignis `lcm-help-open` signalisiert, dass die Vorschau (preview) jetzt geladen werden kann.
 */
export class LcmServerAction extends LitElement {
  static override properties = {
    label: { type: String },
    help: { type: String },
    quiet: { type: Boolean },
    meaning: { type: String },
    icon: { type: String },
    variant: { type: String, reflect: true },
    disabled: { type: Boolean, reflect: true },
    busy: { type: Boolean, reflect: true },
    preview: { attribute: false },
    labels: { attribute: false },
  };
  static override styles = [
    baseStyles,
    buttonStyles,
    css`
      :host {
        display: inline-flex;
      }
      .bubble {
        inline-size: min(26rem, calc(100vw - 16px));
        padding: var(--space-4) var(--space-4) var(--space-3);
        border: 1px solid transparent;
        border-radius: var(--radius-l);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow: var(--elev-2);
        font-size: var(--fs-s);
        line-height: 1.5;
        white-space: normal;
        text-align: start;
        font-weight: var(--fw-regular);
      }
      .bubble::before {
        content: '';
        position: absolute;
        inset-inline-start: calc(var(--arrow-x, 2rem) - 6px);
        inset-block-start: -6px;
        inline-size: 12px;
        block-size: 12px;
        background: var(--surface-overlay);
        transform: rotate(45deg);
        border-start-start-radius: 3px;
        clip-path: polygon(0 0, 100% 0, 0 100%);
      }
      .bubble[data-placement='top']::before {
        inset-block-start: auto;
        inset-block-end: -6px;
        transform: rotate(225deg);
      }
      .title {
        margin: 0 0 var(--space-2);
        color: var(--text-1);
        font-size: var(--fs-m);
        font-weight: var(--fw-semibold);
        font-family: var(--font-sans);
      }
      p {
        margin: 0;
        color: var(--text-2);
      }
      .meaning {
        margin-block-start: var(--space-2);
      }
      .meaning strong {
        color: var(--text-1);
        font-weight: var(--fw-semibold);
      }
      .exec {
        margin-block-start: var(--space-3);
        padding: var(--space-3);
        border-radius: var(--radius-m);
        background: var(--surface-2);
      }
      .exec h4 {
        margin: 0 0 var(--space-2);
        color: var(--text-2);
        font-family: var(--label-font);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        text-transform: var(--label-transform);
        letter-spacing: var(--label-spacing);
      }
      .code {
        margin: 0 0 var(--space-2);
        padding: var(--space-2) var(--space-3);
        border-radius: var(--radius-s);
        background: var(--surface-1);
        color: var(--text-1);
        font-family: var(--font-mono);
        font-size: 0.78rem;
        line-height: 1.6;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
      }
      dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-1) var(--space-4);
        margin: 0;
      }
      dt {
        color: var(--text-2);
      }
      dd {
        margin: 0;
        color: var(--text-1);
        font-weight: var(--fw-medium);
        overflow-wrap: anywhere;
      }
      dd.warn {
        color: var(--warn-text);
      }
      dd.ok {
        color: var(--ok-text);
      }
      dd.mono {
        font-family: var(--font-mono);
        font-size: 0.78rem;
      }
      .foot {
        margin-block-start: var(--space-3);
      }
      .foot a {
        display: inline-flex;
        align-items: center;
        gap: var(--space-1);
        color: var(--accent-text);
        font-weight: var(--fw-medium);
        text-decoration: none;
      }
      .foot a:hover {
        text-decoration: underline;
      }
      lcm-skeleton {
        --skeleton-h: 3rem;
      }
    `,
  ];

  declare label: string;
  declare help: string;
  /** Ohne Erklärblase – für Aktionen, deren Bestätigung ohnehin alles erklärt. */
  declare quiet: boolean;
  declare meaning: string;
  declare icon: IconName | '';
  declare variant: ButtonVariant;
  declare disabled: boolean;
  declare busy: boolean;
  declare preview: ActionPreview;
  declare labels: ActionLabels;

  private readonly bubbleId = uid('bubble');
  readonly hover = new HoverCard(this, {
    popover: () => this.renderRoot.querySelector<HTMLElement>('.bubble'),
    anchor: () => this.renderRoot.querySelector('.btn'),
    onOpen: () => this.dispatchEvent(new CustomEvent('lcm-help-open', { bubbles: true, composed: true })),
  });

  constructor() {
    super();
    this.label = '';
    this.help = '';
    this.quiet = false;
    this.meaning = '';
    this.icon = '';
    this.variant = 'secondary';
    this.disabled = false;
    this.busy = false;
    this.preview = {};
    this.labels = {
      meaning: '',
      executes: '',
      commands: '',
      packages: '',
      reboot: '',
      reversible: '',
      duration: '',
      yes: '',
      no: '',
      docs: '',
      loading: '',
    };
  }

  override firstUpdated(): void {
    if (!this.quiet && !this.help.trim()) {
      const message = `<lcm-server-action label="${this.label}">: das Attribut "help" ist Pflicht (Erklärblase).`;
      if (import.meta.env.DEV) throw new Error(message);
      console.error(message);
    }
  }

  private yesNo(value: boolean | undefined, warnWhenYes: boolean) {
    if (value === undefined) return nothing;
    const tone = value === warnWhenYes ? 'warn' : 'ok';
    return html`<dd class=${tone}>${value ? this.labels.yes : this.labels.no}</dd>`;
  }

  private row(term: string, value: unknown) {
    return html`<dt>${term}</dt>${value}`;
  }

  private renderExecution() {
    const p = this.preview;
    if (p.loading) return html`<div class="exec"><lcm-skeleton shape="block" label=${this.labels.loading}></lcm-skeleton></div>`;
    const hasAny = p.commands?.length || p.packages?.length || p.reboot !== undefined || p.reversible !== undefined || p.duration;
    if (!hasAny) return nothing;
    return html`<div class="exec">
      <h4>${this.labels.executes}</h4>
      ${p.commands?.length
        ? html`<div class="code" aria-label=${this.labels.commands}>${p.commands.join('\n')}</div>`
        : nothing}
      <dl>
        ${p.packages?.length ? this.row(this.labels.packages, html`<dd class="mono">${p.packages.join(', ')}</dd>`) : nothing}
        ${p.reboot !== undefined ? this.row(this.labels.reboot, this.yesNo(p.reboot, true)) : nothing}
        ${p.reversible !== undefined ? this.row(this.labels.reversible, this.yesNo(p.reversible, false)) : nothing}
        ${p.duration ? this.row(this.labels.duration, html`<dd>${p.duration}</dd>`) : nothing}
      </dl>
    </div>`;
  }

  override render() {
    return html`
      <button
        class=${`btn ${this.variant}${this.busy ? ' busy' : ''}`}
        type="button"
        ?disabled=${this.disabled}
        aria-describedby=${this.quiet ? nothing : this.bubbleId}
        aria-busy=${this.busy ? 'true' : nothing}
      >
        ${this.busy ? html`<span class="spinner" aria-hidden="true"></span>` : this.icon ? icon(this.icon) : nothing}${this.label}
      </button>
      ${this.quiet
        ? nothing
        : html`<div id=${this.bubbleId} class="bubble" popover="manual" role="tooltip">
            <p class="title">${this.label}</p>
            <p>${this.help}</p>
            ${this.meaning ? html`<p class="meaning"><strong>${this.labels.meaning}</strong> ${this.meaning}</p>` : nothing}
            ${this.renderExecution()}
            ${this.preview.docsHref
              ? html`<div class="foot"><a href=${this.preview.docsHref}>${this.labels.docs}${icon('arrow-right')}</a></div>`
              : nothing}
          </div>`}
    `;
  }
}
customElements.define('lcm-server-action', LcmServerAction);
