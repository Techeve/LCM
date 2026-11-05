import { LitElement, css, html, nothing } from 'lit';
import { icon, type IconName } from './icons.js';
import { modalClosed, modalOpened } from './modal-watch.js';
import { baseStyles, dialogStyles, emit, uid } from './shared.js';
import { LIMITS } from '../lib/validate.js';

export interface PaletteResult {
  id: string;
  label: string;
  hint?: string;
  icon?: IconName;
}

export interface PaletteResultGroup {
  id: string;
  label: string;
  items: PaletteResult[];
}

/** Kennung der Ergebnisse, damit eine gleiche Liste die Auswahl nicht zurücksetzt. */
function idsOf(groups: PaletteResultGroup[] | undefined): string {
  return (groups ?? []).flatMap((group) => group.items.map((item) => item.id)).join('\n');
}

/**
 * <lcm-command-palette open label="…" placeholder="…" status-text="…" empty-text="…" .groups=${…}> –
 * Befehlspalette als modaler Dialog mit Suchfeld und Ergebnisliste (Combobox mit Listbox).
 * Die Komponente sucht nicht selbst: sie meldet `lcm-palette-query` { query }, der Besitzer liefert
 * die passenden `groups`. Pfeiltasten, Pos1/Ende, Enter wählt, Esc und Klick daneben schließen.
 * Ereignisse: `lcm-palette-query`, `lcm-palette-select` { id }, `lcm-palette-close`.
 */
export class LcmCommandPalette extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    label: { type: String },
    placeholder: { type: String },
    statusText: { type: String, attribute: 'status-text' },
    emptyText: { type: String, attribute: 'empty-text' },
    closeHint: { type: String, attribute: 'close-hint' },
    groups: { attribute: false },
    _query: { state: true },
    _active: { state: true },
  };
  static override styles = [
    baseStyles,
    dialogStyles,
    css`
      :host {
        display: contents;
        --dialog-enter: translateY(-0.5rem) scale(0.98);
        --dialog-exit: var(--dialog-enter);
      }
      dialog {
        inline-size: min(40rem, calc(100vw - 2rem));
        max-block-size: min(32rem, calc(100dvh - 8rem));
        margin: 0;
        margin-inline: auto;
        margin-block-start: 12dvh;
      }
      dialog[open] {
        display: flex;
        flex-direction: column;
      }
      .search {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-3) var(--space-4);
        border-block-end: 1px solid var(--line);
        color: var(--text-2);
        font-size: var(--fs-l);
      }
      input {
        flex: 1;
        min-inline-size: 0;
        min-block-size: var(--control-h);
        border: 0;
        background: transparent;
        color: var(--text-1);
        font: inherit;
      }
      input:focus-visible {
        outline: none;
      }
      input::placeholder {
        color: var(--text-3);
      }
      .search:focus-within {
        box-shadow: inset 0 -2px 0 var(--focus-ring);
      }
      kbd {
        padding: 0.1rem 0.45rem;
        border-radius: var(--radius-s);
        background: var(--surface-2);
        color: var(--text-2);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
      }
      .results {
        overflow: auto;
        padding: var(--space-2);
      }
      ul {
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .group-label {
        padding: var(--space-3) var(--space-3) var(--space-1);
        color: var(--text-3);
        font-family: var(--label-font);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      [role='option'] {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        min-block-size: var(--control-h);
        padding-inline: var(--space-3);
        border-radius: var(--radius-m);
        color: var(--text-1);
        font-size: var(--fs-m);
        cursor: pointer;
      }
      [role='option'][aria-selected='true'] {
        background: var(--accent-soft);
        color: var(--accent-text);
      }
      [role='option'] .icon {
        color: var(--text-3);
      }
      [role='option'][aria-selected='true'] .icon {
        color: inherit;
      }
      .label {
        flex: 1;
        min-inline-size: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      .hint {
        flex: none;
        color: var(--text-3);
        font-size: var(--fs-s);
      }
      [role='option'][aria-selected='true'] .hint {
        color: inherit;
        opacity: 0.8;
      }
      .empty {
        margin: 0;
        padding: var(--space-6) var(--space-4);
        color: var(--text-2);
        text-align: center;
      }
    `,
  ];

  declare open: boolean;
  declare label: string;
  declare placeholder: string;
  declare statusText: string;
  declare emptyText: string;
  declare closeHint: string;
  declare groups: PaletteResultGroup[];
  declare _query: string;
  declare _active: number;
  private readonly listId = uid('palette-list');

  constructor() {
    super();
    this.open = false;
    this.label = '';
    this.placeholder = '';
    this.statusText = '';
    this.emptyText = '';
    this.closeHint = 'Esc';
    this.groups = [];
    this._query = '';
    this._active = 0;
  }

  private get dialog(): HTMLDialogElement | null {
    return this.renderRoot.querySelector('dialog');
  }

  private get results(): PaletteResult[] {
    return this.groups.flatMap((group) => group.items);
  }

  private optionId(index: number): string {
    return `${this.listId}-${index}`;
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('groups') && idsOf(this.groups) !== idsOf(changed.get('groups') as PaletteResultGroup[] | undefined)) this._active = 0;
    if (changed.has('open') && this.open) this._query = '';
  }

  override updated(changed: Map<string, unknown>): void {
    const dialog = this.dialog;
    if (!dialog) return;
    if (this.open && !dialog.open) {
      dialog.showModal();
      modalOpened(this);
      this.renderRoot.querySelector('input')?.focus();
    } else if (!this.open && dialog.open) {
      dialog.close();
      modalClosed(this);
    }
    if (changed.has('_active')) this.renderRoot.querySelector(`#${this.optionId(this._active)}`)?.scrollIntoView({ block: 'nearest' });
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    modalClosed(this);
  }

  private close(): void {
    this.open = false;
    emit(this, 'lcm-palette-close');
  }

  private choose(index: number): void {
    const result = this.results[index];
    if (result) emit(this, 'lcm-palette-select', { id: result.id });
  }

  private onInput(event: Event): void {
    this._query = (event.target as HTMLInputElement).value;
    emit(this, 'lcm-palette-query', { query: this._query });
  }

  private onKeyDown(event: KeyboardEvent): void {
    const count = this.results.length;
    const move = (to: number) => {
      event.preventDefault();
      if (count > 0) this._active = (to + count) % count;
    };
    if (event.key === 'ArrowDown') move(this._active + 1);
    else if (event.key === 'ArrowUp') move(this._active - 1);
    else if (event.key === 'Home') move(0);
    else if (event.key === 'End') move(count - 1);
    else if (event.key === 'Enter') {
      event.preventDefault();
      this.choose(this._active);
    }
  }

  private onNativeCancel(event: Event): void {
    event.preventDefault();
    this.close();
  }

  private onBackdropClick(event: MouseEvent): void {
    if (event.target === this.dialog) this.close();
  }

  private renderOption(result: PaletteResult, index: number) {
    return html`<li
      id=${this.optionId(index)}
      role="option"
      aria-selected=${String(index === this._active)}
      @pointermove=${() => (this._active = index)}
      @click=${() => this.choose(index)}
    >
      ${result.icon ? icon(result.icon) : nothing}
      <span class="label">${result.label}</span>
      ${result.hint ? html`<span class="hint">${result.hint}</span>` : nothing}
    </li>`;
  }

  private renderGroups() {
    let index = 0;
    return this.groups.map((group) => {
      const labelId = `${this.listId}-${group.id}`;
      return html`<li role="presentation">
        <div class="group-label" id=${labelId} role="presentation">${group.label}</div>
        <ul role="group" aria-labelledby=${labelId}>
          ${group.items.map((item) => this.renderOption(item, index++))}
        </ul>
      </li>`;
    });
  }

  override render() {
    const hasResults = this.results.length > 0;
    return html`<dialog aria-label=${this.label} @click=${this.onBackdropClick} @cancel=${this.onNativeCancel} @keydown=${this.onKeyDown}>
      <div class="search">
        ${icon('search')}
        <input
          type="text"
          maxlength=${LIMITS.query}
          role="combobox"
          aria-label=${this.label}
          aria-expanded=${String(hasResults)}
          aria-controls=${hasResults ? this.listId : nothing}
          aria-autocomplete="list"
          aria-activedescendant=${hasResults ? this.optionId(this._active) : nothing}
          placeholder=${this.placeholder}
          autocomplete="off"
          spellcheck="false"
          .value=${this._query}
          @input=${this.onInput}
        />
        <kbd aria-hidden="true">${this.closeHint}</kbd>
      </div>
      <div class="results">
        ${hasResults
          ? html`<ul id=${this.listId} role="listbox" aria-label=${this.label}>
              ${this.renderGroups()}
            </ul>`
          : html`<p class="empty">${this.emptyText}</p>`}
      </div>
      <div class="sr-only" role="status">${this.statusText}</div>
    </dialog>`;
  }
}
customElements.define('lcm-command-palette', LcmCommandPalette);
