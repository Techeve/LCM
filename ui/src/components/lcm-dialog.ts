import { LitElement, css, html, nothing } from 'lit';
import './lcm-button.js';
import './lcm-notice.js';
import { modalClosed, modalOpened } from './modal-watch.js';
import { baseStyles, dialogStyles, emit, uid } from './shared.js';

export interface Impact {
  label: string;
  value: string;
  tone?: 'ok' | 'warn' | 'crit';
}

/**
 * <lcm-dialog open heading="…" message="…" .impacts=${[…]} tone="danger" expect="web03"> – Bestätigung mit Wirkung.
 * Zeigt, was passiert (`impacts`: betroffene Server, Ausfallzeit, umkehrbar …) statt „Sind Sie sicher?“.
 * Mit `expect` muss der Name getippt werden (zerstörende Aktionen). Natives <dialog>: Fokusfalle und Esc inklusive.
 * Höchstens so hoch wie das Browserfenster: Der Inhalt scrollt, der Fuß mit den Knöpfen bleibt stehen.
 * Scheitert die Aktion, gehört der Fehler in den Dialog (`error`, steht über dem Fuß), nicht in einen Toast.
 * Mit `confirm-disabled` bleibt der Bestätigen-Knopf gesperrt (z. B. solange im Dialog nichts gewählt ist).
 * Ereignisse: `lcm-confirm`, `lcm-cancel`.
 */
export class LcmDialog extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    heading: { type: String },
    message: { type: String },
    tone: { type: String, reflect: true },
    confirmLabel: { type: String, attribute: 'confirm-label' },
    cancelLabel: { type: String, attribute: 'cancel-label' },
    expect: { type: String },
    expectLabel: { type: String, attribute: 'expect-label' },
    impacts: { attribute: false },
    busy: { type: Boolean },
    confirmDisabled: { type: Boolean, attribute: 'confirm-disabled' },
    error: { type: String },
    _showError: { state: true },
    _typed: { state: true },
  };
  static override styles = [
    baseStyles,
    dialogStyles,
    css`
      :host {
        display: contents;
      }
      dialog {
        inline-size: min(34rem, calc(100vw - 2rem));
        max-block-size: calc(100dvh - 4rem);
      }
      dialog[open] {
        display: flex;
        flex-direction: column;
      }
      .body {
        flex: 1 1 auto;
        min-block-size: 0;
        padding: var(--space-5);
        overflow-y: auto;
      }
      h2 {
        margin: 0 0 var(--space-2);
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
        line-height: 1.25;
      }
      p {
        margin: 0;
        color: var(--text-2);
      }
      dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-2) var(--space-5);
        margin: var(--space-4) 0 0;
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        font-size: var(--fs-s);
      }
      dt {
        color: var(--text-2);
      }
      dd {
        margin: 0;
        font-weight: var(--fw-semibold);
      }
      dd.warn {
        color: var(--warn-text);
      }
      dd.crit {
        color: var(--crit-text);
      }
      dd.ok {
        color: var(--ok-text);
      }
      label {
        display: block;
        margin: var(--space-4) 0 var(--space-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      input {
        inline-size: 100%;
        min-block-size: var(--control-h);
        padding-inline: var(--space-3);
        border: 1px solid var(--line-strong);
        border-radius: var(--radius-m);
        background: var(--surface-1);
        color: var(--text-1);
        font-family: var(--font-mono);
        font-size: var(--fs-m);
      }
      .error {
        flex: none;
        padding: 0 var(--space-5) var(--space-3);
      }
      footer {
        flex: none;
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
  declare message: string;
  declare tone: 'default' | 'danger';
  declare confirmLabel: string;
  declare cancelLabel: string;
  declare expect: string;
  declare expectLabel: string;
  declare impacts: Impact[];
  declare busy: boolean;
  /** Bestätigen geht noch nicht, z. B. solange im Dialog nichts ausgewählt ist. */
  declare confirmDisabled: boolean;
  declare error: string;
  /** Ein Fehler vom letzten Öffnen bleibt verborgen, bis die Seite einen neuen meldet. */
  declare _showError: boolean;
  declare _typed: string;
  private readonly inputId = uid('confirm-input');

  constructor() {
    super();
    this.open = false;
    this.heading = '';
    this.message = '';
    this.tone = 'default';
    this.confirmLabel = '';
    this.cancelLabel = '';
    this.expect = '';
    this.expectLabel = '';
    this.impacts = [];
    this.busy = false;
    this.confirmDisabled = false;
    this.error = '';
    this._showError = false;
    this._typed = '';
  }

  private get dialog(): HTMLDialogElement | null {
    return this.renderRoot.querySelector('dialog');
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('open') && this.open) {
      this._showError = false;
      this._typed = '';
    }
    if (changed.has('error') && this.hasUpdated) this._showError = this.error !== '';
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

  private cancel(): void {
    this.open = false;
    emit(this, 'lcm-cancel');
  }

  private confirm(): void {
    emit(this, 'lcm-confirm');
  }

  private onBackdropClick(event: MouseEvent): void {
    if (event.target === this.dialog) this.cancel();
  }

  private onNativeCancel(event: Event): void {
    event.preventDefault();
    this.cancel();
  }

  private get canConfirm(): boolean {
    return !this.busy && !this.confirmDisabled && (this.expect === '' || this._typed === this.expect);
  }

  override render() {
    return html`<dialog aria-labelledby="title" @click=${this.onBackdropClick} @cancel=${this.onNativeCancel}>
      <div class="body">
        <h2 id="title">${this.heading}</h2>
        ${this.message ? html`<p>${this.message}</p>` : nothing}
        ${this.impacts.length
          ? html`<dl>${this.impacts.map((i) => html`<dt>${i.label}</dt><dd class=${i.tone ?? ''}>${i.value}</dd>`)}</dl>`
          : nothing}
        <slot></slot>
        ${this.expect
          ? html`<label for=${this.inputId}>${this.expectLabel}</label>
              <input
                id=${this.inputId}
                autocomplete="off"
                spellcheck="false"
                maxlength=${this.expect.length + 8}
                .value=${this._typed}
                @input=${(e: Event) => (this._typed = (e.target as HTMLInputElement).value)}
              />`
          : nothing}
      </div>
      ${this.error && this._showError ? html`<div class="error"><lcm-notice tone="crit">${this.error}</lcm-notice></div>` : nothing}
      <footer>
        <lcm-button variant="ghost" @click=${this.cancel}>${this.cancelLabel}</lcm-button>
        <lcm-button variant=${this.tone === 'danger' ? 'danger-solid' : 'primary'} ?disabled=${!this.canConfirm} ?busy=${this.busy} @click=${this.confirm}
          >${this.confirmLabel}</lcm-button
        >
      </footer>
    </dialog>`;
  }
}
customElements.define('lcm-dialog', LcmDialog);
