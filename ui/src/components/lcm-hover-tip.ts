import { LitElement, css, html, nothing } from 'lit';
import { HoverCard } from './floating.js';
import { icon, type IconName } from './icons.js';
import { baseStyles, uid } from './shared.js';

/**
 * <lcm-hover-tip label="Konsole offen" icon="terminal" tone="info">Text</lcm-hover-tip> – kleines Kennzeichen in einer
 * Tabellenzeile; die Einzelheiten stehen in der Blase (Überfahren oder Tastaturfokus). Ohne `icon` ist der Inhalt des
 * Slots `trigger` der Auslöser (etwa die Ampel). `tone`: info, warn, crit, muted, plain.
 *
 * Info-i neben einer Beschriftung: `<lcm-hover-tip icon="info" tone="plain" label="Mehr zu …">Erklärung</lcm-hover-tip>`
 * – nur das Symbol, dezent in `--text-3`, beim Überfahren und bei Fokus in `--accent`.
 */
export class LcmHoverTip extends LitElement {
  static override properties = {
    label: { type: String },
    icon: { type: String },
    tone: { type: String, reflect: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
        vertical-align: middle;
        --chip-bg: var(--chip-muted-bg);
        --chip-fg: var(--chip-muted-fg);
      }
      :host([tone='info']) {
        --chip-bg: var(--info-soft);
        --chip-fg: var(--info-text);
      }
      :host([tone='warn']) {
        --chip-bg: var(--warn-soft);
        --chip-fg: var(--warn-text);
      }
      :host([tone='crit']) {
        --chip-bg: var(--crit-soft);
        --chip-fg: var(--crit-text);
      }
      :host([tone='plain']) {
        --chip-bg: transparent;
        --chip-fg: var(--text-3);
      }
      :host([tone='plain']) .trigger:hover,
      :host([tone='plain']) .trigger:focus-visible {
        --chip-fg: var(--accent);
      }
      :host([tone='plain']) .chip svg {
        inline-size: 1rem;
        block-size: 1rem;
      }
      .trigger {
        display: inline-flex;
        align-items: center;
        border-radius: var(--radius-pill);
        cursor: default;
      }
      .chip {
        display: inline-grid;
        place-items: center;
        inline-size: 1.375rem;
        block-size: 1.375rem;
        border-radius: var(--radius-pill);
        background: var(--chip-bg);
        color: var(--chip-fg);
      }
      .chip svg {
        inline-size: 0.875rem;
        block-size: 0.875rem;
      }
      .bubble {
        inline-size: max-content;
        max-inline-size: min(24rem, calc(100vw - 16px));
        margin: 0;
        padding: var(--space-3) var(--space-4);
        border: 1px solid transparent;
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow: var(--elev-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-regular);
        line-height: 1.5;
        white-space: normal;
        text-align: start;
      }
    `,
  ];

  declare label: string;
  declare icon: IconName | '';
  declare tone: string;

  readonly #bubbleId = uid('tip');
  readonly hover = new HoverCard(this, {
    popover: () => this.renderRoot.querySelector<HTMLElement>('.bubble'),
    anchor: () => this.renderRoot.querySelector('.trigger'),
    openDelay: 250,
  });

  constructor() {
    super();
    this.label = '';
    this.icon = '';
    this.tone = 'muted';
  }

  override render() {
    return html`<span class="trigger" tabindex="0" role=${this.icon ? 'img' : nothing} aria-label=${this.label || nothing} aria-describedby=${this.#bubbleId}
        >${this.icon ? html`<span class="chip">${icon(this.icon)}</span>` : html`<slot name="trigger"></slot>`}</span
      >
      <div id=${this.#bubbleId} class="bubble" popover="manual" role="tooltip"><slot></slot></div>`;
  }
}
customElements.define('lcm-hover-tip', LcmHoverTip);
