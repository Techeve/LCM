import { LitElement, css, html } from 'lit';
import { icon } from './icons.js';
import { modalClosed, modalOpened } from './modal-watch.js';
import { baseStyles, dialogStyles, emit } from './shared.js';

export interface ShortcutRow {
  /** Tasten, die nacheinander gedrückt werden. */
  keys: string[];
  label: string;
}

export interface ShortcutSection {
  label: string;
  rows: ShortcutRow[];
}

/**
 * <lcm-shortcut-help open heading="…" close-label="…" .sections=${…}> – Hilfe zu den Tastenkürzeln
 * als modaler Dialog. Esc, Klick daneben und der Schließen-Knopf melden `lcm-help-close`.
 */
export class LcmShortcutHelp extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    heading: { type: String },
    intro: { type: String },
    closeLabel: { type: String, attribute: 'close-label' },
    sections: { attribute: false },
  };
  static override styles = [
    baseStyles,
    dialogStyles,
    css`
      :host {
        display: contents;
      }
      dialog {
        inline-size: min(40rem, calc(100vw - 2rem));
        max-block-size: calc(100dvh - 4rem);
        overflow: auto;
      }
      .head {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
        padding: var(--space-4) var(--space-5);
      }
      h2 {
        margin: 0;
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .close {
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
      .close:hover {
        background: var(--surface-2);
        color: var(--text-1);
      }
      .body {
        padding: 0 var(--space-5) var(--space-5);
      }
      .intro {
        margin: 0 0 var(--space-4);
        color: var(--text-2);
      }
      h3 {
        margin: var(--space-4) 0 var(--space-2);
        color: var(--text-3);
        font-family: var(--label-font);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-2) var(--space-5);
        margin: 0;
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        font-size: var(--fs-m);
      }
      dt {
        display: flex;
        gap: var(--space-1);
      }
      dd {
        margin: 0;
        color: var(--text-2);
      }
      kbd {
        min-inline-size: 1.6rem;
        padding: 0.1rem 0.45rem;
        border-radius: var(--radius-s);
        background: var(--surface-1);
        box-shadow: 0 1px 0 var(--line-strong);
        color: var(--text-1);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        text-align: center;
      }
    `,
  ];

  declare open: boolean;
  declare heading: string;
  declare intro: string;
  declare closeLabel: string;
  declare sections: ShortcutSection[];

  constructor() {
    super();
    this.open = false;
    this.heading = '';
    this.intro = '';
    this.closeLabel = '';
    this.sections = [];
  }

  private get dialog(): HTMLDialogElement | null {
    return this.renderRoot.querySelector('dialog');
  }

  override updated(): void {
    const dialog = this.dialog;
    if (!dialog) return;
    if (this.open && !dialog.open) {
      dialog.showModal();
      modalOpened(this);
    } else if (!this.open && dialog.open) {
      dialog.close();
      modalClosed(this);
    }
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    modalClosed(this);
  }

  private close(): void {
    this.open = false;
    emit(this, 'lcm-help-close');
  }

  private onNativeCancel(event: Event): void {
    event.preventDefault();
    this.close();
  }

  private onBackdropClick(event: MouseEvent): void {
    if (event.target === this.dialog) this.close();
  }

  private renderSection(section: ShortcutSection) {
    return html`<h3>${section.label}</h3>
      <dl>
        ${section.rows.map(
          (row) => html`<dt>${row.keys.map((key) => html`<kbd>${key}</kbd>`)}</dt>
            <dd>${row.label}</dd>`,
        )}
      </dl>`;
  }

  override render() {
    return html`<dialog aria-labelledby="title" @click=${this.onBackdropClick} @cancel=${this.onNativeCancel}>
      <div class="head">
        <h2 id="title">${this.heading}</h2>
        <button class="close" type="button" aria-label=${this.closeLabel} @click=${this.close}>${icon('x')}</button>
      </div>
      <div class="body">
        <p class="intro">${this.intro}</p>
        ${this.sections.map((section) => this.renderSection(section))}
      </div>
    </dialog>`;
  }
}
customElements.define('lcm-shortcut-help', LcmShortcutHelp);
