import { LitElement, css, html, nothing } from 'lit';
import { HoverCard } from './floating.js';
import { icon } from './icons.js';
import { baseStyles, uid } from './shared.js';

/**
 * <lcm-term definition="…">Kerndienst</lcm-term> – Fachbegriff mit Erklärung beim Überfahren oder Fokussieren.
 * Die Definition kommt im echten LCM aus dem gemeinsamen Glossar.
 */
export class LcmTerm extends LitElement {
  static override properties = {
    definition: { type: String },
    heading: { type: String },
    docsHref: { type: String, attribute: 'docs-href' },
    docsLabel: { type: String, attribute: 'docs-label' },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline;
      }
      .term {
        border-radius: 2px;
        text-decoration: underline dotted var(--text-3);
        text-decoration-thickness: 1.5px;
        text-underline-offset: 0.22em;
        cursor: help;
      }
      .term:hover {
        text-decoration-color: var(--accent);
      }
      .tip {
        inline-size: min(20rem, calc(100vw - 16px));
        padding: var(--space-3) var(--space-4);
        border: 1px solid transparent;
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-2);
        box-shadow: var(--elev-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-regular);
        line-height: 1.5;
        text-align: start;
        white-space: normal;
      }
      .tip strong {
        display: block;
        margin-block-end: var(--space-1);
        color: var(--text-1);
        font-weight: var(--fw-semibold);
      }
      a {
        display: inline-flex;
        align-items: center;
        gap: var(--space-1);
        margin-block-start: var(--space-2);
        color: var(--accent-text);
        font-weight: var(--fw-medium);
        text-decoration: none;
      }
      a:hover {
        text-decoration: underline;
      }
    `,
  ];

  declare definition: string;
  declare heading: string;
  declare docsHref: string;
  declare docsLabel: string;
  private readonly tipId = uid('term');
  readonly hover = new HoverCard(this, {
    popover: () => this.renderRoot.querySelector<HTMLElement>('.tip'),
    anchor: () => this.renderRoot.querySelector('.term'),
    openDelay: 250,
  });

  constructor() {
    super();
    this.definition = '';
    this.heading = '';
    this.docsHref = '';
    this.docsLabel = '';
  }

  override render() {
    return html`<span class="term" tabindex="0" aria-describedby=${this.tipId}><slot></slot></span>
      <span id=${this.tipId} class="tip" popover="manual" role="tooltip">
        ${this.heading ? html`<strong>${this.heading}</strong>` : nothing}${this.definition}
        ${this.docsHref ? html`<br /><a href=${this.docsHref}>${this.docsLabel}${icon('arrow-right')}</a>` : nothing}
      </span>`;
  }
}
customElements.define('lcm-term', LcmTerm);
