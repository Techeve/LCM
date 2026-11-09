import { LitElement, css, html } from 'lit';
import { icon } from './icons.js';
import { modalClosed, modalOpened } from './modal-watch.js';
import { baseStyles, dialogStyles, emit } from './shared.js';

/**
 * <lcm-modal open heading="Server bearbeiten" close-label="Schließen" size="m"> – Fenster zum Bearbeiten.
 * Mittig, so hoch wie der Inhalt (höchstens die Fensterhöhe, dann scrollt der Inhalt); `size` legt die größte
 * Breite fest: s 30rem, m 40rem, ml 42rem (zwei Feldspalten der Breite m oder ein Feld l neben einem Port), l 56rem,
 * xl 72rem. Slots: Standard (Inhalt), `note` (Fehler zur Aktion des Fensters, steht fest über dem Fuß, meist
 * `<lcm-notice variant="line" tone="crit">`), `footer` (Aktionen, entfällt ohne Inhalt).
 * Natives <dialog>: Fokusfalle, Esc, Fokus zurück an den Auslöser. Meldet `lcm-close`.
 */
export class LcmModal extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    heading: { type: String },
    size: { type: String, reflect: true },
    subtitle: { type: String },
    closeLabel: { type: String, attribute: 'close-label' },
    hasFooter: { state: true },
    hasNote: { state: true },
  };
  static override styles = [
    baseStyles,
    dialogStyles,
    css`
      :host {
        display: contents;
        --modal-w: 40rem;
      }
      :host([size='s']) {
        --modal-w: 30rem;
      }
      :host([size='ml']) {
        --modal-w: 42rem;
      }
      :host([size='l']) {
        --modal-w: 56rem;
      }
      :host([size='xl']) {
        --modal-w: 72rem;
      }
      dialog {
        inline-size: min(var(--modal-w), calc(100vw - 2rem));
        max-block-size: calc(100dvh - 4rem);
      }
      dialog:focus {
        outline: none;
      }
      dialog[open] {
        display: flex;
        flex-direction: column;
      }
      header {
        display: flex;
        align-items: flex-start;
        justify-content: space-between;
        gap: var(--space-4);
        padding: var(--space-5) var(--space-5) var(--space-4);
      }
      h2 {
        margin: 0;
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        line-height: 1.25;
      }
      .subtitle {
        margin: 0.2rem 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .close {
        display: grid;
        place-items: center;
        flex: none;
        inline-size: var(--control-h);
        block-size: var(--control-h);
        border: 0;
        border-radius: var(--radius-m);
        background: transparent;
        color: var(--text-2);
        cursor: pointer;
      }
      .close:hover {
        background: var(--surface-2);
        color: var(--text-1);
      }
      .content {
        flex: 1 1 auto;
        min-block-size: 0;
        padding: var(--space-2) var(--space-5) var(--space-5);
        overflow-y: auto;
      }
      footer[hidden],
      .note[hidden] {
        display: none;
      }
      .note {
        flex: none;
        padding: 0 var(--space-5) var(--space-3);
      }
      footer {
        display: flex;
        justify-content: flex-end;
        gap: var(--space-2);
        padding: var(--space-3) var(--space-5);
        background: var(--surface-2);
      }
    `,
  ];

  declare open: boolean;
  declare heading: string;
  declare size: 's' | 'm' | 'ml' | 'l' | 'xl';
  declare subtitle: string;
  declare closeLabel: string;
  declare hasFooter: boolean;
  declare hasNote: boolean;

  constructor() {
    super();
    this.open = false;
    this.heading = '';
    this.size = 'm';
    this.hasFooter = false;
    this.hasNote = false;
    this.subtitle = '';
    this.closeLabel = '';
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
      // Fokus auf das Fenster selbst statt auf den ersten Knopf (Schließen):
      // kein Fokusrahmen beim Öffnen, Tab führt wie gewohnt hinein.
      dialog.focus();
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
    emit(this, 'lcm-close');
  }

  override render() {
    return html`<dialog
      tabindex="-1"
      aria-labelledby="title"
      @click=${(e: MouseEvent) => e.target === this.dialog && this.close()}
      @cancel=${(e: Event) => {
        e.preventDefault();
        this.close();
      }}
    >
      <header>
        <div>
          <h2 id="title">${this.heading}</h2>
          ${this.subtitle ? html`<p class="subtitle">${this.subtitle}</p>` : ''}
        </div>
        <button class="close" type="button" aria-label=${this.closeLabel} @click=${this.close}>${icon('x')}</button>
      </header>
      <div class="content"><slot></slot></div>
      <div class="note" ?hidden=${!this.hasNote}>
        <slot name="note" @slotchange=${(e: Event) => (this.hasNote = (e.target as HTMLSlotElement).assignedElements().length > 0)}></slot>
      </div>
      <footer ?hidden=${!this.hasFooter}>
        <slot name="footer" @slotchange=${(e: Event) => (this.hasFooter = (e.target as HTMLSlotElement).assignedElements().length > 0)}></slot>
      </footer>
    </dialog>`;
  }
}
customElements.define('lcm-modal', LcmModal);
