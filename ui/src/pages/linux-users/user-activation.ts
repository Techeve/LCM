import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-dialog';
import '../../components/lcm-notice';
import { baseStyles } from '../../components/shared';
import { linuxUsersApi, type ActivationLink, type LinuxUser } from '../../api/linux-users';
import { copyWithToast } from '../../lib/clipboard';
import { I18nController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { t } from '../../lib/i18n';
import { sectionStyles } from './lu-styles';
import './lu-action';
import { ACTIVATION_TTL_HOURS, activationUrl } from './user-model';

/**
 * <lcm-lu-user-activation .user=${…} can-write> – Zugang einrichten: Aktivierungslink erzeugen oder per E-Mail
 * schicken. Über den Link setzt der Mitarbeiter selbst Passwort und SSH-Schlüssel; LCM fasst dafür keinen Server an.
 */
export class LcmLuUserActivation extends LitElement {
  static override properties = {
    user: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _link: { state: true },
  };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .link {
        margin: var(--space-3) 0 0;
        padding: var(--space-2) var(--space-3);
        border-radius: var(--radius-s);
        background: var(--surface-1);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        overflow-wrap: anywhere;
        user-select: all;
      }
      .status {
        margin: var(--space-3) 0 0;
        font-size: var(--fs-s);
      }
    `,
  ];

  declare user: LinuxUser;
  declare canWrite: boolean;
  declare _link: ActivationLink | null;

  constructor() {
    super();
    this.canWrite = false;
    this._link = null;
    new I18nController(this);
  }

  async #generate(sendEmail: boolean): Promise<ActivationLink> {
    return linuxUsersApi.generateActivation(this.user.id, ACTIVATION_TTL_HOURS, sendEmail);
  }

  async #copy(): Promise<void> {
    if (!this._link) return;
    await copyWithToast(activationUrl(location.origin, this._link.token), { copied: t('linuxUsers.activation.copied'), failed: t('linuxUsers.activation.copyFailed') });
  }

  #status(link: ActivationLink) {
    if (link.mailed) return html`<p class="status">${t('linuxUsers.activation.mailed', { email: this.user.email })}</p>`;
    if (link.mail_error) return html`<lcm-notice tone="warn">${t('linuxUsers.activation.mailFailed', { error: link.mail_error })}</lcm-notice>`;
    return nothing;
  }

  #dialog() {
    const link = this._link;
    return html`<lcm-dialog
      ?open=${link !== null}
      heading=${t('linuxUsers.activation.dialog.title')}
      message=${link ? t('linuxUsers.activation.dialog.message', { until: formatDateTime(link.expires_at) }) : ''}
      confirm-label=${t('linuxUsers.activation.dialog.copy')}
      cancel-label=${t('linuxUsers.action.close')}
      @lcm-confirm=${() => void this.#copy()}
      @lcm-cancel=${() => (this._link = null)}
    >
      ${link ? html`${this.#status(link)}<p class="link">${activationUrl(location.origin, link.token)}</p>` : nothing}
    </lcm-dialog>`;
  }

  override render() {
    const email = this.user.email;
    return html`<section>
        <h3>
          ${t('linuxUsers.activation.title')}
          <lcm-badge tone=${this.user.has_password ? 'ok' : 'neutral'}>${this.user.has_password ? t('linuxUsers.activation.passwordSet') : t('linuxUsers.activation.noPassword')}</lcm-badge>
        </h3>
        <p>${t('linuxUsers.activation.intro')}</p>
        ${this.canWrite
          ? html`<div class="actions">
              <lcm-lu-action
                label=${t('linuxUsers.activation.create.label')}
                help=${t('linuxUsers.activation.create.help')}
                meaning=${t('linuxUsers.activation.create.meaning', { hours: ACTIVATION_TTL_HOURS })}
                icon="key"
                no-confirm
                .run=${() => this.#generate(false)}
                @lcm-done=${(e: CustomEvent<ActivationLink>) => (this._link = e.detail)}
              ></lcm-lu-action>
              <lcm-lu-action
                label=${t('linuxUsers.activation.mail.label')}
                help=${t('linuxUsers.activation.mail.help')}
                meaning=${email ? t('linuxUsers.activation.mail.meaning', { email }) : t('linuxUsers.activation.mail.noEmail')}
                icon="bell"
                ?disabled=${!email}
                confirm-heading=${t('linuxUsers.activation.mail.heading', { email })}
                confirm-label=${t('linuxUsers.activation.mail.confirm')}
                .impacts=${[
                  { label: t('linuxUsers.activation.mail.recipient'), value: email },
                  { label: t('linuxUsers.activation.mail.valid'), value: t('linuxUsers.activation.mail.validValue', { hours: ACTIVATION_TTL_HOURS }) },
                  { label: t('linuxUsers.impact.servers'), value: t('linuxUsers.impact.noServers'), tone: 'ok' },
                ]}
                .run=${() => this.#generate(true)}
                @lcm-done=${(e: CustomEvent<ActivationLink>) => (this._link = e.detail)}
              ></lcm-lu-action>
            </div>`
          : nothing}
      </section>
      ${this.#dialog()}`;
  }
}
customElements.define('lcm-lu-user-activation', LcmLuUserActivation);
