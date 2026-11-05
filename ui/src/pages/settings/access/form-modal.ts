import { LitElement, css, html, nothing } from 'lit';
import '../../../components/lcm-button';
import '../../../components/lcm-dialog';
import '../../../components/lcm-modal';
import '../../../components/lcm-notice';
import type { LcmModal } from '../../../components/lcm-modal';
import { baseStyles, emit } from '../../../components/shared';
import { I18nController } from '../../../lib/controllers';
import { SAVE_EVENT } from '../../../lib/hotkeys';
import { t } from '../../../lib/i18n';
import { unsaved } from '../../../lib/unsaved';

/**
 * <lcm-settings-form-modal open heading="…" ?dirty ?busy ?can-save error="…"> – Fenster für ein Formular der
 * Einstellungen. Der Inhalt steht im Standard-Slot. Sie meldet Ungespeichertes (`unsaved`), speichert mit Strg/Cmd+S
 * und Enter, fragt vor dem Verwerfen nach und zeigt Fehler des Servers. Ereignisse: `lcm-submit`, `lcm-close`.
 */
export class LcmSettingsFormModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    heading: { type: String },
    subtitle: { type: String },
    saveLabel: { type: String, attribute: 'save-label' },
    dirty: { type: Boolean },
    busy: { type: Boolean },
    canSave: { type: Boolean, attribute: 'can-save' },
    error: { type: String },
    _discarding: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      form {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
    `,
  ];

  declare open: boolean;
  declare heading: string;
  declare subtitle: string;
  declare saveLabel: string;
  declare dirty: boolean;
  declare busy: boolean;
  declare canSave: boolean;
  declare error: string;
  declare _discarding: boolean;

  constructor() {
    super();
    this.open = false;
    this.heading = '';
    this.subtitle = '';
    this.saveLabel = '';
    this.dirty = false;
    this.busy = false;
    this.canSave = true;
    this.error = '';
    this._discarding = false;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    document.addEventListener(SAVE_EVENT, this.#onSave);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    document.removeEventListener(SAVE_EVENT, this.#onSave);
    unsaved.set(this, false);
  }

  override willUpdate(): void {
    unsaved.set(this, this.open && this.dirty);
    if (!this.open) this._discarding = false;
  }

  /** Strg/Cmd+S speichert, solange die Fenster offen ist. */
  #onSave = (event: Event): void => {
    if (!this.open) return;
    event.preventDefault();
    this.#submit();
  };

  /** Enter in einem einzeiligen Feld speichert (die Felder liegen im Shadow DOM, ein echtes Submit gibt es dort nicht). */
  #onKeyDown(event: KeyboardEvent): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement) || ['checkbox', 'radio', 'button'].includes(target.type)) return;
    event.preventDefault();
    this.#submit();
  }

  #submit(): void {
    if (this.canSave && !this.busy) emit(this, 'lcm-submit');
  }

  /** Schließen verlangt eine Rückfrage, solange Ungespeichertes da ist; die Fenster bleibt dann offen. */
  #onModalClose(event: Event): void {
    event.stopPropagation();
    if (!this.dirty) {
      emit(this, 'lcm-close');
      return;
    }
    (event.target as LcmModal).open = true;
    this._discarding = true;
  }

  #requestClose(): void {
    if (this.dirty) this._discarding = true;
    else emit(this, 'lcm-close');
  }

  #discard(): void {
    this._discarding = false;
    emit(this, 'lcm-close');
  }

  override render() {
    return html`
      <lcm-modal ?open=${this.open} heading=${this.heading} subtitle=${this.subtitle} close-label=${t('settings.form.close')} @lcm-close=${this.#onModalClose}>
        <form @submit=${(e: Event) => e.preventDefault()} @keydown=${this.#onKeyDown}>
          <slot></slot>
        </form>
        ${this.error ? html`<lcm-notice slot="note" tone="crit">${this.error}</lcm-notice>` : nothing}
        <lcm-button slot="footer" variant="ghost" @click=${this.#requestClose}>${t('settings.form.cancel')}</lcm-button>
        <lcm-button slot="footer" variant="primary" ?disabled=${!this.canSave} ?busy=${this.busy} @click=${this.#submit}>
          ${this.saveLabel || t('settings.form.save')}
        </lcm-button>
      </lcm-modal>
      <lcm-dialog
        ?open=${this._discarding}
        heading=${t('settings.form.discardTitle')}
        message=${t('settings.form.discardMessage')}
        confirm-label=${t('settings.form.discardConfirm')}
        cancel-label=${t('settings.form.discardKeep')}
        tone="danger"
        @lcm-confirm=${this.#discard}
        @lcm-cancel=${() => (this._discarding = false)}
      ></lcm-dialog>
    `;
  }
}
customElements.define('lcm-settings-form-modal', LcmSettingsFormModal);
