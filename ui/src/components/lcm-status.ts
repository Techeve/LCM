import { LitElement, css, html, nothing } from 'lit';
import './lcm-ampel.js';
import { placeFloating } from './floating.js';
import { icon } from './icons.js';
import { baseStyles, levelStyles, uid, type StatusLevel } from './shared.js';

/**
 * <lcm-status level="warn" label="Warnung"> – Ampel mit Text.
 * Mit Inhalt im Slot `insight` wird sie zum Knopf, der ein Insight-Popover öffnet
 * (kurze Erklärung und passende Aktion). `variant="pill"` setzt sie auf eine Fläche.
 */
export class LcmStatus extends LitElement {
  static override properties = {
    level: { type: String, reflect: true },
    label: { type: String },
    size: { type: String, reflect: true },
    variant: { type: String, reflect: true },
    insightLabel: { type: String, attribute: 'insight-label' },
    _hasInsight: { state: true },
  };
  static override styles = [
    baseStyles,
    levelStyles,
    css`
      :host {
        display: inline-flex;
        vertical-align: middle;
        max-inline-size: 100%;
      }
      .trigger {
        display: inline-flex;
        align-items: center;
        gap: 0.5em;
        min-inline-size: 0;
        padding: 0;
        border: 0;
        background: none;
        text-align: start;
        cursor: default;
        color: var(--text-1);
      }
      button.trigger {
        cursor: pointer;
        margin-inline: -0.4em;
        padding: 0.15em 0.4em;
        border-radius: var(--radius-pill);
        transition: background var(--dur-1) var(--ease);
      }
      button.trigger:hover {
        background: var(--surface-2);
      }
      .text {
        min-inline-size: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        font-weight: var(--fw-medium);
      }
      :host([size='l']) .text {
        font-size: var(--fs-l);
      }
      :host([variant='pill']) .trigger {
        padding: 0.1em 0.6em 0.1em 0.45em;
        margin: 0;
        font-size: var(--fs-s);
        border-radius: var(--radius-pill);
        background: var(--c-soft);
        color: var(--c-text);
      }
      :host([variant='pill']) button.trigger:hover {
        background: color-mix(in srgb, var(--c-soft) 80%, var(--c));
      }
      .chev {
        inline-size: 1em;
        block-size: 1em;
        opacity: 0.7;
      }
      .insight {
        inline-size: min(22rem, calc(100vw - 16px));
        padding: var(--space-4);
        border: 1px solid transparent;
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow: var(--elev-2);
        font-size: var(--fs-s);
        line-height: 1.5;
        text-align: start;
        white-space: normal;
      }
      .insight-head {
        display: flex;
        gap: var(--space-2);
        align-items: center;
        margin-block-end: var(--space-2);
        color: var(--c-text);
        font-weight: var(--fw-semibold);
      }
    `,
  ];

  declare level: StatusLevel;
  declare label: string;
  declare size: 'm' | 'l';
  declare variant: 'inline' | 'pill';
  declare insightLabel: string;
  declare _hasInsight: boolean;
  private readonly popoverId = uid('insight');

  constructor() {
    super();
    this.level = 'unknown';
    this.label = '';
    this.size = 'm';
    this.variant = 'inline';
    this.insightLabel = '';
    this._hasInsight = false;
  }

  private onSlotChange(event: Event): void {
    this._hasInsight = (event.target as HTMLSlotElement).assignedNodes().length > 0;
  }

  private onToggle(event: Event): void {
    const popover = event.target as HTMLElement;
    const trigger = this.renderRoot.querySelector('.trigger');
    if ((event as ToggleEvent).newState === 'open' && trigger) placeFloating(trigger, popover, { align: 'start' });
    this.requestUpdate();
  }

  override render() {
    const ampel = html`<lcm-ampel level=${this.level} size=${this.size === 'l' ? 'l' : 'm'}></lcm-ampel>`;
    const text = html`<span class="text">${this.label}</span>`;
    return html`
      ${this._hasInsight
        ? html`<button
            class="trigger"
            type="button"
            popovertarget=${this.popoverId}
            aria-haspopup="dialog"
            aria-label=${this.insightLabel ? `${this.label}: ${this.insightLabel}` : nothing}
          >
            ${ampel}${text}<span class="chev">${icon('chevron-down')}</span>
          </button>`
        : html`<span class="trigger">${ampel}${text}</span>`}
      <div id=${this.popoverId} popover="auto" class="insight" role="dialog" aria-label=${this.label} @toggle=${this.onToggle}>
        <slot name="insight" @slotchange=${this.onSlotChange}></slot>
      </div>
    `;
  }
}
customElements.define('lcm-status', LcmStatus);
