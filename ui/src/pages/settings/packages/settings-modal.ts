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
 * <lcm-settings-modal open heading="…" ?dirty ?busy ?can-save ?read-only error="…"> – Fenster der Einstellungs-
 * abschnitte. Standard-Slot: das Formular; Slot `extra`: weitere Abschnitte unter dem Formular, die eigene
 * Aktionen haben (Enter in ihren Feldern speichert das Formular nicht). Meldet Ungespeichertes (`unsaved`),
 * speichert mit Strg/Cmd+S, fragt vor dem Verwerfen nach und zeigt Fehler des Servers. Mit `read-only` entfällt
 * „Speichern“. Ereignisse: `lcm-submit`, `lcm-close`. Slot `footer-start`: zusätzliche Knöpfe links im Fuß.
 */
export class LcmSettingsModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    heading: { type: String },
    subtitle: { type: String },
    saveLabel: { type: String, attribute: 'save-label' },
    dirty: { type: Boolean },
    busy: { type: Boolean },
    canSave: { type: Boolean, attribute: 'can-save' },
    readOnly: { type: Boolean, attribute: 'read-only' },
    error: { type: String },
    _discarding: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      form,
      .extra {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
      .extra {
        margin-block-start: var(--space-6);
        gap: var(--space-6);
      }
      .spacer {
        flex: 1;
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
  declare readOnly: boolean;
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
    this.readOnly = false;
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

  /** Enter in einem einzeiligen Feld des Formulars speichert (die Felder liegen im Shadow DOM, ein echtes Submit gibt es dort nicht). */
  #onKeyDown(event: KeyboardEvent): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement) || ['checkbox', 'radio', 'button'].includes(target.type)) return;
    event.preventDefault();
    this.#submit();
  }

  #submit(): void {
    if (!this.readOnly && this.canSave && !this.busy) emit(this, 'lcm-submit');
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
      <lcm-modal
        ?open=${this.open}
        heading=${this.heading}
        subtitle=${this.subtitle}
        close-label=${t('settings.more.modal.close')}
        @lcm-close=${this.#onModalClose}
      >
        <form @submit=${(e: Event) => e.preventDefault()} @keydown=${this.#onKeyDown}>
          <slot></slot>
        </form>
        ${this.error ? html`<lcm-notice slot="note" tone="crit">${this.error}</lcm-notice>` : nothing}
        <div class="extra"><slot name="extra"></slot></div>
        <slot slot="footer" name="footer-start"></slot>
        <span slot="footer" class="spacer"></span>
        <lcm-button slot="footer" variant="ghost" @click=${this.#requestClose}>${this.readOnly ? t('settings.more.modal.close') : t('settings.more.modal.cancel')}</lcm-button>
        ${this.readOnly
          ? nothing
          : html`<lcm-button slot="footer" variant="primary" ?disabled=${!this.canSave} ?busy=${this.busy} @click=${this.#submit}>
              ${this.saveLabel || t('settings.more.modal.save')}
            </lcm-button>`}
      </lcm-modal>
      <lcm-dialog
        ?open=${this._discarding}
        heading=${t('settings.more.discard.title')}
        message=${t('settings.more.discard.message')}
        confirm-label=${t('settings.more.discard.confirm')}
        cancel-label=${t('settings.more.discard.keep')}
        tone="danger"
        @lcm-confirm=${this.#discard}
        @lcm-cancel=${() => (this._discarding = false)}
      ></lcm-dialog>
    `;
  }
}
customElements.define('lcm-settings-modal', LcmSettingsModal);
