import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-dialog';
import '../../components/lcm-server-action';
import type { ButtonVariant } from '../../components/button-styles';
import type { IconName } from '../../components/icons';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, emit } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { actionLabels } from '../servers/preview';

/**
 * <lcm-lu-action label="…" help="…" .impacts=${[…]} .run=${async () => …}> – Aktion, die auf Servern etwas ändert:
 * Knopf mit Erklärblase (`help`, `meaning`) und Bestätigung mit Wirkung (`impacts`). Mit `expect` muss der Name
 * getippt werden. `run` führt aus; danach erscheint ein Hinweis (`done`) und `lcm-done` meldet das Ergebnis,
 * ein Fehler erscheint als Hinweis. Mit `no-confirm` entfällt die Bestätigung. Inhalt im Standard-Slot erscheint im Dialog.
 */
export class LcmLuAction extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    label: { type: String },
    help: { type: String },
    meaning: { type: String },
    icon: { type: String },
    variant: { type: String },
    disabled: { type: Boolean },
    confirmHeading: { type: String, attribute: 'confirm-heading' },
    confirmMessage: { type: String, attribute: 'confirm-message' },
    confirmLabel: { type: String, attribute: 'confirm-label' },
    danger: { type: Boolean },
    expect: { type: String },
    expectLabel: { type: String, attribute: 'expect-label' },
    done: { type: String },
    noConfirm: { type: Boolean, attribute: 'no-confirm' },
    impacts: { attribute: false },
    run: { attribute: false },
    _open: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
      }
    `,
  ];

  declare label: string;
  declare help: string;
  declare meaning: string;
  declare icon: IconName | '';
  declare variant: ButtonVariant;
  declare disabled: boolean;
  declare confirmHeading: string;
  declare confirmMessage: string;
  declare confirmLabel: string;
  declare danger: boolean;
  declare expect: string;
  declare expectLabel: string;
  declare done: string;
  declare noConfirm: boolean;
  declare impacts: Impact[];
  declare run: (() => Promise<unknown>) | undefined;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _open: boolean;
  declare _busy: boolean;

  constructor() {
    super();
    this._dialogError = '';
    this.label = '';
    this.help = '';
    this.meaning = '';
    this.icon = '';
    this.variant = 'secondary';
    this.disabled = false;
    this.confirmHeading = '';
    this.confirmMessage = '';
    this.confirmLabel = '';
    this.danger = false;
    this.expect = '';
    this.expectLabel = '';
    this.done = '';
    this.noConfirm = false;
    this.impacts = [];
    this.run = undefined;
    this._open = false;
    this._busy = false;
    new I18nController(this);
  }

  #onClick(): void {
    if (this.noConfirm) void this.#start();
    else this._open = true;
  }

  async #start(): Promise<void> {
    if (!this.run || this._busy) return;
    this._busy = true;
    this._dialogError = '';
    try {
      const result = await this.run();
      this._open = false;
      if (this.done) toast({ tone: 'ok', title: this.done });
      emit(this, 'lcm-done', result);
    } catch (error) {
      // Mit Bestätigung bleibt der Fehler im Dialog, ohne kommt er als Toast.
      if (this.noConfirm) toast({ tone: 'crit', title: t('linuxUsers.action.failed', { label: this.label }), message: errorText(error) });
      else this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  override render() {
    return html`<lcm-server-action
        label=${this.label}
        help=${this.help}
        meaning=${this.meaning}
        icon=${this.icon}
        variant=${this.variant}
        ?disabled=${this.disabled}
        ?busy=${this._busy && !this._open}
        .labels=${actionLabels()}
        @click=${this.#onClick}
      ></lcm-server-action>
      ${this.noConfirm
        ? nothing
        : html`<lcm-dialog
            ?open=${this._open}
            tone=${this.danger ? 'danger' : 'default'}
            heading=${this.confirmHeading || this.label}
            message=${this.confirmMessage}
            confirm-label=${this.confirmLabel || this.label}
            cancel-label=${t('linuxUsers.action.cancel')}
            expect=${this.expect}
            expect-label=${this.expectLabel}
            .impacts=${this.impacts}
            error=${this._dialogError}
            ?busy=${this._busy}
            @lcm-cancel=${() => (this._open = false)}
            @lcm-confirm=${() => void this.#start()}
          >
            <slot></slot>
          </lcm-dialog>`}`;
  }
}
customElements.define('lcm-lu-action', LcmLuAction);
