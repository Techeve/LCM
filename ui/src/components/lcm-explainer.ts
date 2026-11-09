import { LitElement, css, html, nothing } from 'lit';
import { icon } from './icons.js';
import { baseStyles, emit, safeStorage, uid } from './shared.js';

/**
 * <lcm-explainer topic="group-rules" heading="Wozu Regeln?" docs-href="…"> – zusammenklappbare Erklärspalte.
 * Der Text steht im Slot. Der Zustand wird je `topic` im Browser gemerkt (localStorage, mit try/catch).
 * Eingeklappt bleibt eine schmale Leiste mit der Überschrift. Meldet `lcm-explainer-toggle` mit { collapsed }.
 * Der gemerkte Zustand gilt schon beim ersten Zeichnen; die Breite wandert nur, wenn der Benutzer umschaltet.
 */
export class LcmExplainer extends LitElement {
  static override properties = {
    topic: { type: String },
    heading: { type: String },
    docsHref: { type: String, attribute: 'docs-href' },
    docsLabel: { type: String, attribute: 'docs-label' },
    collapseLabel: { type: String, attribute: 'collapse-label' },
    expandLabel: { type: String, attribute: 'expand-label' },
    collapsed: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        inline-size: 21rem;
        align-self: start;
        position: sticky;
        inset-block-start: var(--space-4);
      }
      :host([animated]) {
        transition: inline-size var(--dur-3) var(--ease);
      }
      :host([collapsed]) {
        inline-size: 3rem;
      }
      aside {
        position: relative;
        border-radius: var(--radius-l);
        background: linear-gradient(160deg, var(--accent-soft), var(--surface-2) 70%);
        color: var(--text-2);
        font-size: var(--fs-s);
        line-height: 1.6;
        overflow: hidden;
      }
      .head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-2);
        padding: var(--space-4) var(--space-3) 0 var(--space-5);
      }
      h2 {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        margin: 0;
        color: var(--accent-text);
        font-size: var(--fs-m);
        font-weight: var(--head-weight);
      }
      .toggle {
        display: grid;
        place-items: center;
        inline-size: 2rem;
        block-size: 2rem;
        border: 0;
        border-radius: var(--radius-s);
        background: transparent;
        color: var(--text-2);
        cursor: pointer;
      }
      .toggle:hover {
        background: color-mix(in srgb, var(--text-1) 8%, transparent);
        color: var(--text-1);
      }
      .body {
        padding: var(--space-3) var(--space-5) var(--space-5);
      }
      ::slotted(p) {
        margin: 0 0 var(--space-3);
      }
      ::slotted(ul) {
        margin: 0 0 var(--space-3);
        padding-inline-start: 1.1rem;
      }
      ::slotted(strong) {
        color: var(--text-1);
      }
      a {
        display: inline-flex;
        align-items: center;
        gap: var(--space-1);
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      a:hover {
        text-decoration: underline;
      }
      .rail {
        display: flex;
        flex-direction: column;
        align-items: center;
        gap: var(--space-4);
        padding: var(--space-2) 0 var(--space-4);
      }
      .rail-title {
        writing-mode: vertical-rl;
        color: var(--accent-text);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
        letter-spacing: 0.02em;
        white-space: nowrap;
      }
    `,
  ];

  declare topic: string;
  declare heading: string;
  declare docsHref: string;
  declare docsLabel: string;
  declare collapseLabel: string;
  declare expandLabel: string;
  declare collapsed: boolean;
  private readonly bodyId = uid('explainer');

  constructor() {
    super();
    this.topic = '';
    this.heading = '';
    this.docsHref = '';
    this.docsLabel = '';
    this.collapseLabel = '';
    this.expandLabel = '';
    this.collapsed = false;
  }

  private get storageKey(): string {
    return `lcm.explainer.${this.topic}`;
  }

  override connectedCallback(): void {
    super.connectedCallback();
    // Vor dem ersten Rendern, sonst steht die Spalte einen Moment offen da und klappt sichtbar zu.
    this.restore();
    this.toggleAttribute('collapsed', this.collapsed);
    this.addEventListener('transitionend', this.#settled);
  }

  override disconnectedCallback(): void {
    this.removeEventListener('transitionend', this.#settled);
    super.disconnectedCallback();
  }

  /** Nach dem Umschalten ruht die Breite: `animated` soll nicht stehen bleiben (etwa beim Wechsel der Fenstergröße). */
  readonly #settled = (event: TransitionEvent): void => {
    if (event.propertyName === 'inline-size') this.removeAttribute('animated');
  };

  override willUpdate(changed: Map<string, unknown>): void {
    if (!changed.has('topic')) return;
    this.removeAttribute('animated');
    this.restore();
  }

  private restore(): void {
    if (this.topic) this.collapsed = safeStorage.get(this.storageKey) === 'closed';
  }

  private toggle(): void {
    this.setAttribute('animated', '');
    this.collapsed = !this.collapsed;
    if (this.topic) safeStorage.set(this.storageKey, this.collapsed ? 'closed' : null);
    emit(this, 'lcm-explainer-toggle', { collapsed: this.collapsed });
  }

  override render() {
    if (this.collapsed) {
      return html`<aside>
        <div class="rail">
          <button class="toggle" type="button" aria-expanded="false" aria-controls=${this.bodyId} aria-label=${this.expandLabel} @click=${this.toggle}>${icon('info')}</button>
          <span class="rail-title" aria-hidden="true">${this.heading}</span>
        </div>
        <div id=${this.bodyId} hidden></div>
      </aside>`;
    }
    return html`<aside>
      <div class="head">
        <h2>${icon('info')}${this.heading}</h2>
        <button class="toggle" type="button" aria-expanded="true" aria-controls=${this.bodyId} aria-label=${this.collapseLabel} title=${this.collapseLabel} @click=${this.toggle}>
          ${icon('panel-right')}
        </button>
      </div>
      <div id=${this.bodyId} class="body">
        <slot></slot>
        ${this.docsHref ? html`<a href=${this.docsHref}>${this.docsLabel}${icon('arrow-right')}</a>` : nothing}
      </div>
    </aside>`;
  }
}
customElements.define('lcm-explainer', LcmExplainer);
