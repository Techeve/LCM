import { LitElement, html } from 'lit';
import '../components/lcm-dialog';
import { I18nController } from '../lib/controllers';
import { t } from '../lib/i18n';
import { unsaved } from '../lib/unsaved';

/** <lcm-leave-dialog> – die Rückfrage, bevor ungespeicherte Änderungen beim Seitenwechsel verloren gehen. */
export class LcmLeaveDialog extends LitElement {
  static override properties = { _open: { state: true } };

  declare _open: boolean;

  #answer: ((leave: boolean) => void) | undefined;
  #pending: Promise<boolean> | undefined;

  constructor() {
    super();
    this._open = false;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    unsaved.setPrompt(() => this.#ask());
  }

  /** Klickt jemand bei offener Frage auf Zurück, bleibt es bei dieser einen Frage. */
  #ask(): Promise<boolean> {
    this.#pending ??= new Promise((resolve) => {
      this.#answer = resolve;
      this._open = true;
    });
    return this.#pending;
  }

  #close(leave: boolean): void {
    this._open = false;
    this.#pending = undefined;
    this.#answer?.(leave);
  }

  override render() {
    return html`<lcm-dialog
      ?open=${this._open}
      tone="danger"
      heading=${t('settings.form.leaveTitle')}
      message=${t('settings.form.leaveText')}
      confirm-label=${t('settings.form.discard')}
      cancel-label=${t('settings.form.leaveStay')}
      @lcm-confirm=${() => this.#close(true)}
      @lcm-cancel=${() => this.#close(false)}
    ></lcm-dialog>`;
  }
}
customElements.define('lcm-leave-dialog', LcmLeaveDialog);
