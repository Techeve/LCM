import { LitElement, css, html, nothing } from 'lit';
import { fieldStyles, fieldWidthProperty, type FieldWidth } from './field-styles.js';
import { placeFloating } from './floating.js';
import { icon } from './icons.js';
import type { Option } from './lcm-select.js';
import { baseStyles, emit, uid } from './shared.js';
import { LIMITS } from '../lib/validate.js';

/**
 * <lcm-combobox label="Server" .options=${[…]} .value=${…} empty-text="Keine Treffer"> – Auswahl mit Tippfilter (ARIA 1.2 Combobox).
 * Pfeiltasten, Enter wählt, Esc schließt. Meldet `lcm-change` { value }.
 * `width` (xs, s, m, l, full; Vorgabe m) richtet die Breite nach den Einträgen, siehe `FieldWidth`.
 */
export class LcmCombobox extends LitElement {
  static override properties = {
    width: fieldWidthProperty,
    label: { type: String },
    options: { attribute: false },
    value: { type: String },
    placeholder: { type: String },
    hint: { type: String },
    emptyText: { type: String, attribute: 'empty-text' },
    _query: { state: true },
    _open: { state: true },
    _active: { state: true },
  };
  static override styles = [
    baseStyles,
    fieldStyles,
    css`
      .list {
        min-inline-size: 14rem;
        max-block-size: 16rem;
        margin: 0;
        padding: var(--space-2);
        border: 1px solid transparent;
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        box-shadow: var(--elev-2);
        list-style: none;
        overflow-y: auto;
      }
      .opt {
        display: flex;
        justify-content: space-between;
        gap: var(--space-3);
        min-block-size: calc(var(--control-h) - 0.25rem);
        align-items: center;
        padding-inline: var(--space-3);
        border-radius: var(--radius-s);
        cursor: pointer;
      }
      .opt[data-active='true'] {
        background: var(--surface-2);
      }
      .opt[aria-selected='true'] {
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
      }
      .opt .opt-hint {
        color: var(--text-3);
        font-size: var(--fs-xs);
      }
      .empty {
        padding: var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  /** Breite der Eingabefläche nach Inhalt, siehe `FieldWidth`. */
  declare width: FieldWidth | undefined;
  declare label: string;
  declare options: Option[];
  declare value: string;
  declare placeholder: string;
  declare hint: string;
  declare emptyText: string;
  declare _query: string;
  declare _open: boolean;
  declare _active: number;
  private readonly base = uid('combo');

  constructor() {
    super();
    this.label = '';
    this.options = [];
    this.value = '';
    this.placeholder = '';
    this.hint = '';
    this.emptyText = '';
    this._query = '';
    this._open = false;
    this._active = 0;
  }

  private get selectedLabel(): string {
    return this.options.find((o) => o.value === this.value)?.label ?? '';
  }

  private get filtered(): Option[] {
    const q = this._query.trim().toLowerCase();
    return q ? this.options.filter((o) => o.label.toLowerCase().includes(q)) : this.options;
  }

  private get list(): HTMLElement | null {
    return this.renderRoot.querySelector('.list');
  }

  private openList(): void {
    if (this._open) return;
    this._open = true;
    this.list?.showPopover();
    this.place();
  }

  /**
   * Liste unter das Feld, bei zu wenig Platz darüber. Sie bleibt beim Scrollen offen und folgt dem Feld: Das Feld
   * zu fokussieren scrollt die Seite oft erst selbst, und eine beim ersten Scrollen geschlossene Liste käme nie wieder.
   */
  private place(): void {
    const control = this.renderRoot.querySelector('.control');
    if (control && this.list?.matches(':popover-open')) placeFloating(control, this.list, { matchWidth: true, gap: 4, keepOnScroll: true });
  }

  private readonly onViewportChange = (event: Event): void => {
    if (this._open && event.target !== this.list) this.place();
  };

  override connectedCallback(): void {
    super.connectedCallback();
    window.addEventListener('scroll', this.onViewportChange, { capture: true, passive: true });
    window.addEventListener('resize', this.onViewportChange);
  }

  override disconnectedCallback(): void {
    window.removeEventListener('scroll', this.onViewportChange, { capture: true });
    window.removeEventListener('resize', this.onViewportChange);
    super.disconnectedCallback();
  }

  private closeList(): void {
    this._open = false;
    this._query = '';
    if (this.list?.matches(':popover-open')) this.list.hidePopover();
  }

  /** Schließt etwas anderes als `closeList` die Liste, muss der Zustand folgen, sonst öffnet sie nie wieder. */
  private onListToggle(event: Event): void {
    if ((event as ToggleEvent).newState === 'closed' && this._open) {
      this._open = false;
      this._query = '';
    }
  }

  private choose(option: Option): void {
    this.value = option.value;
    this.closeList();
    emit(this, 'lcm-change', { value: option.value });
  }

  private onKeyDown(event: KeyboardEvent): void {
    const items = this.filtered;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      this.openList();
      const step = event.key === 'ArrowDown' ? 1 : -1;
      this._active = (this._active + step + items.length) % Math.max(items.length, 1);
    } else if (event.key === 'Enter' && this._open) {
      event.preventDefault();
      const option = items[this._active];
      if (option) this.choose(option);
    } else if (event.key === 'Escape' && this._open) {
      // Auch den Standard unterbinden, sonst schließt der Browser zusätzlich das umgebende Fenster.
      event.preventDefault();
      event.stopPropagation();
      this.closeList();
    }
  }

  override updated(): void {
    this.renderRoot.querySelector('[data-active="true"]')?.scrollIntoView({ block: 'nearest' });
  }

  override render() {
    const items = this.filtered;
    const active = items[this._active];
    return html`<div class="field">
      <label for=${`${this.base}-in`}>${this.label}</label>
      <div class="control" @focusout=${(e: FocusEvent) => !this.renderRoot.contains(e.relatedTarget as Node) && this.closeList()}>
        <input
          id=${`${this.base}-in`}
          role="combobox"
          aria-expanded=${String(this._open)}
          aria-controls=${`${this.base}-list`}
          aria-autocomplete="list"
          aria-activedescendant=${this._open && active ? `${this.base}-${active.value}` : nothing}
          autocomplete="off"
          maxlength=${LIMITS.query}
          placeholder=${this.placeholder || nothing}
          .value=${this._open ? this._query : this.selectedLabel}
          @focus=${() => this.openList()}
          @click=${() => this.openList()}
          @input=${(e: Event) => {
            this._query = (e.target as HTMLInputElement).value;
            this._active = 0;
            this.openList();
          }}
          @keydown=${this.onKeyDown}
        />
        <span class="adorn">${icon('chevron-down')}</span>
      </div>
      <ul id=${`${this.base}-list`} class="list" popover="manual" role="listbox" aria-label=${this.label} @mousedown=${(e: Event) => e.preventDefault()} @toggle=${this.onListToggle}>
        ${items.length === 0
          ? html`<li class="empty" role="presentation">${this.emptyText}</li>`
          : items.map(
              (o, i) => html`<li
                id=${`${this.base}-${o.value}`}
                class="opt"
                role="option"
                aria-selected=${String(o.value === this.value)}
                data-active=${String(i === this._active)}
                @click=${() => this.choose(o)}
                @pointermove=${() => (this._active = i)}
              >
                <span>${o.label}</span>${o.hint ? html`<span class="opt-hint">${o.hint}</span>` : nothing}
              </li>`,
            )}
      </ul>
      ${this.hint ? html`<div class="hint">${this.hint}</div>` : nothing}
    </div>`;
  }
}
customElements.define('lcm-combobox', LcmCombobox);
