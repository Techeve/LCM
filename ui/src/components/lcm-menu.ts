import { LitElement, css, html, nothing } from 'lit';
import { placeFloating, type Align } from './floating.js';
import { icon, type IconName } from './icons.js';
import { baseStyles, emit, uid } from './shared.js';

export interface MenuItem {
  id: string;
  label: string;
  hint?: string;
  /** Erklärung, die beim Überfahren neben dem Menü erscheint (nur für Einträge ohne eigenen Dialog). */
  tip?: string;
  icon?: IconName;
  danger?: boolean;
  separatorBefore?: boolean;
}

/**
 * <lcm-menu label="Sprache" icon="globe" .items=${…} value="de"> – Knopf mit Menü (Popover).
 * Mit `value` verhält es sich wie eine Auswahl (menuitemradio), sonst wie ein Aktionsmenü.
 * Pfeiltasten, Pos1/Ende, Esc; Auswahl meldet `lcm-select` mit { id }.
 * `variant="button"` gibt dem Auslöser den Rahmen eines Knopfs (etwa „Aktionen“ neben einer Knopfleiste).
 * Ein `hint` steht klein unter der Bezeichnung des Eintrags, ein `tip` erscheint beim Überfahren neben dem Menü.
 */
export class LcmMenu extends LitElement {
  static override properties = {
    label: { type: String },
    text: { type: String },
    icon: { type: String },
    avatar: { type: String },
    items: { attribute: false },
    value: { type: String },
    align: { type: String },
    variant: { type: String, reflect: true },
    _open: { state: true },
    _tip: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
      }
      .trigger {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        min-block-size: var(--control-h);
        padding-inline: var(--space-3);
        border: 0;
        border-radius: var(--radius-m);
        background: transparent;
        color: var(--menu-fg, var(--text-1));
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
        cursor: pointer;
        transition: background var(--dur-1) var(--ease);
      }
      .trigger:hover,
      .trigger[aria-expanded='true'] {
        background: var(--menu-hover, var(--surface-2));
      }
      :host([variant='button']) .trigger {
        min-block-size: var(--btn-h, var(--control-h));
        padding-inline: var(--btn-px, 0.85rem);
        border: 1px solid var(--line);
        border-start-start-radius: var(--btn-r-start, var(--radius-s));
        border-end-start-radius: var(--btn-r-start, var(--radius-s));
        border-start-end-radius: var(--btn-r-end, var(--radius-s));
        border-end-end-radius: var(--btn-r-end, var(--radius-s));
        background: var(--surface-1);
        font-size: var(--btn-fs, var(--fs-m));
        font-weight: var(--fw-regular);
        transition:
          border-color var(--dur-1) var(--ease),
          color var(--dur-1) var(--ease);
      }
      :host([variant='button']) .trigger:hover,
      :host([variant='button']) .trigger[aria-expanded='true'] {
        border-color: var(--accent);
        background: var(--surface-1);
        color: var(--accent-text);
      }
      .trigger.icon-only {
        padding-inline: var(--space-2);
      }
      .chev {
        inline-size: 1em;
        block-size: 1em;
        opacity: 0.7;
      }
      .avatar {
        display: grid;
        place-items: center;
        inline-size: 1.75rem;
        block-size: 1.75rem;
        border-radius: 50%;
        background: var(--accent-solid);
        color: var(--text-on-accent);
        font-size: 0.7rem;
        font-weight: var(--fw-bold);
        letter-spacing: 0.02em;
      }
      .menu {
        min-inline-size: 13rem;
        max-inline-size: min(20rem, calc(100vw - 16px));
        padding: var(--space-2);
        border: 1px solid transparent;
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow: var(--elev-2);
        overflow: auto;
      }
      .item {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        inline-size: 100%;
        min-block-size: calc(var(--control-h) - 0.25rem);
        padding-inline: var(--space-3);
        border: 0;
        border-radius: var(--radius-s);
        background: transparent;
        color: var(--text-1);
        font-size: var(--fs-m);
        text-align: start;
        cursor: pointer;
      }
      .item:hover,
      .item:focus-visible {
        background: var(--surface-2);
        outline: none;
      }
      .item:focus-visible {
        box-shadow: inset 0 0 0 2px var(--focus-ring);
      }
      .item.danger {
        color: var(--crit-text);
      }
      .item .text {
        display: grid;
        flex: 1;
        padding-block: 0.35rem;
        line-height: 1.3;
      }
      .item .hint {
        color: var(--text-3);
        font-size: var(--fs-xs);
      }
      .item .check {
        inline-size: 1.1em;
        block-size: 1.1em;
        color: var(--accent-text);
      }
      .menu.tips {
        overflow: visible;
      }
      .tip {
        position: absolute;
        inline-size: 17rem;
        padding: var(--space-2) var(--space-3);
        border: 1px solid var(--line);
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-2);
        box-shadow: var(--elev-2);
        font-size: var(--fs-s);
        line-height: 1.45;
        pointer-events: none;
      }
      .tip.end {
        inset-inline-start: calc(100% + 8px);
      }
      .tip.start {
        inset-inline-end: calc(100% + 8px);
      }
      .sep {
        block-size: 1px;
        margin-block: var(--space-2);
        background: var(--line);
      }
    `,
  ];

  declare label: string;
  declare text: string;
  declare icon: IconName | '';
  declare avatar: string;
  declare items: MenuItem[];
  declare value: string;
  declare align: Align;
  declare variant: 'plain' | 'button';
  declare _open: boolean;
  declare _tip: { text: string; top: number; side: 'start' | 'end' } | null;
  private readonly menuId = uid('menu');

  constructor() {
    super();
    this.label = '';
    this.text = '';
    this.icon = '';
    this.avatar = '';
    this.items = [];
    this.value = '';
    this.align = 'end';
    this.variant = 'plain';
    this._open = false;
    this._tip = null;
  }

  private get radio(): boolean {
    return this.value !== '';
  }

  private buttons(): HTMLButtonElement[] {
    return [...this.renderRoot.querySelectorAll<HTMLButtonElement>('.item')];
  }

  private onToggle(event: Event): void {
    const open = (event as ToggleEvent).newState === 'open';
    this._open = open;
    if (!open) {
      this._tip = null;
      return;
    }
    const menu = event.target as HTMLElement;
    placeFloating(this.renderRoot.querySelector('.trigger')!, menu, { align: this.align, gap: 6 });
    const buttons = this.buttons();
    (buttons.find((b) => b.getAttribute('aria-checked') === 'true') ?? buttons[0])?.focus();
  }

  private onKeyDown(event: KeyboardEvent): void {
    const buttons = this.buttons();
    const index = buttons.indexOf(this.shadowRoot?.activeElement as HTMLButtonElement);
    const move = (to: number) => {
      event.preventDefault();
      buttons[(to + buttons.length) % buttons.length]?.focus();
    };
    if (event.key === 'ArrowDown') move(index + 1);
    else if (event.key === 'ArrowUp') move(index - 1);
    else if (event.key === 'Home') move(0);
    else if (event.key === 'End') move(buttons.length - 1);
  }

  private onTriggerKey(event: KeyboardEvent): void {
    if (event.key !== 'ArrowDown' || this._open) return;
    event.preventDefault();
    this.renderRoot.querySelector<HTMLElement>('.menu')?.showPopover();
  }

  private select(item: MenuItem): void {
    this.renderRoot.querySelector<HTMLElement>('.menu')?.hidePopover();
    emit(this, 'lcm-select', { id: item.id });
  }

  /** Die Position kommt über CSSOM: Ein `style`-Attribut (auch per `styleMap` beim ersten Rendern) verwirft die CSP. */
  protected override updated(): void {
    const tip = this.renderRoot.querySelector<HTMLElement>('.tip');
    if (tip && this._tip) tip.style.insetBlockStart = `${this._tip.top}px`;
  }

  /** Erklärung neben dem Menü; rechts, solange dort Platz ist, sonst links. */
  private showTip(item: MenuItem, el: HTMLElement): void {
    if (!item.tip) {
      this._tip = null;
      return;
    }
    const menu = el.closest('.menu')?.getBoundingClientRect();
    const side = menu && menu.right + 300 > window.innerWidth ? 'start' : 'end';
    this._tip = { text: item.tip, top: el.offsetTop, side };
  }

  private renderItem(item: MenuItem) {
    const checked = this.radio && item.id === this.value;
    return html`${item.separatorBefore ? html`<div class="sep" role="separator"></div>` : nothing}
      <button
        class=${item.danger ? 'item danger' : 'item'}
        type="button"
        role=${this.radio ? 'menuitemradio' : 'menuitem'}
        @pointerenter=${(e: PointerEvent) => this.showTip(item, e.currentTarget as HTMLElement)}
        @pointerleave=${() => (this._tip = null)}
        @focus=${(e: FocusEvent) => this.showTip(item, e.currentTarget as HTMLElement)}
        @blur=${() => (this._tip = null)}
        aria-checked=${this.radio ? String(checked) : nothing}
        @click=${() => this.select(item)}
      >
        ${item.icon ? icon(item.icon) : nothing}
        <span class="text"><span>${item.label}</span>${item.hint ? html`<span class="hint">${item.hint}</span>` : nothing}</span>
        ${checked ? html`<span class="check">${icon('check')}</span>` : nothing}
      </button>`;
  }

  override render() {
    const iconOnly = !this.text && !this.avatar;
    return html`
      <!-- Der Name enthält die sichtbaren Initialen des Avatars (WCAG 2.5.3, „Label in Name“). -->
      <button
        class=${iconOnly ? 'trigger icon-only' : 'trigger'}
        type="button"
        popovertarget=${this.menuId}
        aria-haspopup="menu"
        aria-expanded=${String(this._open)}
        aria-label=${this.avatar ? `${this.label}, ${this.avatar}` : this.label}
        title=${iconOnly ? this.label : nothing}
        @keydown=${this.onTriggerKey}
      >
        ${this.avatar ? html`<span class="avatar" aria-hidden="true">${this.avatar}</span>` : this.icon ? icon(this.icon) : nothing}
        ${this.text ? html`<span>${this.text}</span>` : nothing}
        ${iconOnly ? nothing : html`<span class="chev">${icon('chevron-down')}</span>`}
      </button>
      <div
        id=${this.menuId}
        class=${this.items.some((item) => item.tip) ? 'menu tips' : 'menu'}
        popover="auto"
        role="menu"
        aria-label=${this.label}
        @toggle=${this.onToggle}
        @keydown=${this.onKeyDown}
      >
        ${this.items.map((item) => this.renderItem(item))}
        ${this._tip ? html`<div class="tip ${this._tip.side}" role="tooltip">${this._tip.text}</div>` : nothing}
      </div>
    `;
  }
}
customElements.define('lcm-menu', LcmMenu);
