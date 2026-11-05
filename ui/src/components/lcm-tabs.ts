import { LitElement, css, html, nothing } from 'lit';
import { FadeOnChange } from '../lib/motion.js';
import './lcm-badge.js';
import { baseStyles, emit, uid } from './shared.js';

export interface TabItem {
  id: string;
  label: string;
  badge?: string;
  badgeTone?: 'neutral' | 'accent' | 'ok' | 'warn' | 'crit' | 'info';
}

/**
 * <lcm-tabs label="Serverdetail" .tabs=${…} value="overview"> – Reiter mit Pfeiltasten (Roving Tabindex).
 * Der aktive Inhalt steht im Standard-Slot und wird als tabpanel ausgezeichnet.
 * Meldet `lcm-tab-change` mit { id }; im echten LCM ist der Reiter an die URL gebunden. Der neue Inhalt blendet kurz ein.
 */
export class LcmTabs extends LitElement {
  static override properties = {
    tabs: { attribute: false },
    value: { type: String },
    label: { type: String },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .list {
        display: flex;
        gap: 2px;
        padding: var(--tab-track-pad);
        border-radius: var(--tab-radius);
        background: var(--tab-track-bg);
        box-shadow: var(--tab-track-line);
        overflow-x: auto;
        scrollbar-width: thin;
      }
      .tab {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        flex: none;
        min-block-size: calc(var(--control-h) - 0.125rem);
        padding-inline: var(--space-4);
        border: 0;
        border-radius: var(--tab-radius);
        background: transparent;
        color: var(--text-2);
        font-size: var(--fs-m);
        font-weight: var(--fw-medium);
        white-space: nowrap;
        cursor: pointer;
        transition:
          background var(--dur-1) var(--ease),
          color var(--dur-1) var(--ease);
      }
      .tab:hover {
        color: var(--text-1);
        background: color-mix(in srgb, var(--text-1) 6%, transparent);
      }
      .tab[aria-selected='true'] {
        background: var(--tab-active-bg);
        box-shadow: var(--tab-active-shadow);
        color: var(--tab-active-fg);
        font-weight: var(--fw-semibold);
      }
      .panel {
        padding-block-start: var(--space-5);
      }
    `,
  ];

  declare tabs: TabItem[];
  declare value: string;
  declare label: string;
  private readonly idBase = uid('tabs');

  constructor() {
    super();
    this.tabs = [];
    this.value = '';
    this.label = '';
    new FadeOnChange(this, () => this.value, () => this.renderRoot.querySelector('.panel'));
  }

  private select(id: string): void {
    if (id === this.value) return;
    this.value = id;
    emit(this, 'lcm-tab-change', { id });
  }

  private onKeyDown(event: KeyboardEvent): void {
    const keys = ['ArrowRight', 'ArrowLeft', 'Home', 'End'];
    if (!keys.includes(event.key) || this.tabs.length === 0) return;
    const dir = getComputedStyle(this).direction === 'rtl' ? -1 : 1;
    const index = this.tabs.findIndex((t) => t.id === this.value);
    let next = index;
    if (event.key === 'ArrowRight') next = index + dir;
    else if (event.key === 'ArrowLeft') next = index - dir;
    else if (event.key === 'Home') next = 0;
    else next = this.tabs.length - 1;
    next = (next + this.tabs.length) % this.tabs.length;
    event.preventDefault();
    this.select(this.tabs[next]!.id);
    this.updateComplete.then(() => this.renderRoot.querySelector<HTMLElement>(`#${this.idBase}-${this.tabs[next]!.id}`)?.focus());
  }

  override updated(): void {
    this.renderRoot.querySelector('.tab[aria-selected="true"]')?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  override render() {
    return html`
      <div class="list" role="tablist" aria-label=${this.label} @keydown=${this.onKeyDown}>
        ${this.tabs.map(
          (tab) => html`<button
            class="tab"
            id=${`${this.idBase}-${tab.id}`}
            type="button"
            role="tab"
            aria-selected=${String(tab.id === this.value)}
            tabindex=${tab.id === this.value ? '0' : '-1'}
            @click=${() => this.select(tab.id)}
          >
            ${tab.label}${tab.badge ? html`<lcm-badge tone=${tab.badgeTone ?? 'neutral'}>${tab.badge}</lcm-badge>` : nothing}
          </button>`,
        )}
      </div>
      <div class="panel" role="tabpanel" aria-labelledby=${`${this.idBase}-${this.value}`}><slot></slot></div>
    `;
  }
}
customElements.define('lcm-tabs', LcmTabs);
