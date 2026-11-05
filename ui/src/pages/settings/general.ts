import { html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import '../../components/lcm-notice';
import '../../components/lcm-server-action';
import { toast } from '../../components/lcm-toaster';
import { MAIL_CHANNEL_URL, readMailChannel, settingsApi, type GlobalSettings, type MailChannel } from '../../api/settings';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { portForSecurity, readMailSecurity, type MailSecurity } from '../../lib/mail-security';
import { auth } from '../../lib/auth';
import { router } from '../../lib/router';
import { LIMITS } from '../../lib/validate';
import { mailSecurityField } from './mail-security-field';
import './session-mode-field';
import './system/copy-field';
import { sessionTtlError } from './access/security-model';
import { choice, flag, int, secret, text, type Draft, type FieldErrors, type FieldSpec } from './system/form-state';
import { helpLabels } from './system/help';
import { SettingsForm } from './system/settings-form';

const FIELDS: FieldSpec[] = [
  text('default_ssh_user', LIMITS.loginUser),
  int('default_ssh_port', 1, 65535),
  text('public_base_url', LIMITS.path, 'url'),
  choice('browser_session_mode', () => 'memory'),
  int('session_ttl_minutes', 0, 43200),
  int('job_idle_timeout_minutes', 0, 1440),
  int('job_idle_timeout_slow_minutes', 0, 1440),
  int('packages_stale_warn_hours', 24, 720),
  int('packages_stale_critical_days', 2, 90),
  flag('mail_enabled'),
  text('mail_host', LIMITS.host, 'host'),
  int('mail_port', 1, 65535),
  choice('mail_security', (settings) => readMailSecurity(undefined, settings.mail_use_tls)),
  text('mail_username', LIMITS.loginUser),
  secret('mail_password', LIMITS.password),
  text('mail_from', LIMITS.sender, 'sender'),
  text('mail_admin_recipients', LIMITS.list, 'emails'),
  int('log_retention_days', 0, 3650),
  int('routine_log_retention_days', 1, 3650),
  int('storage_history_retention_days', 90, 365),
];

/** Einstellungen → Allgemein: Vorgaben für neue Server, Adresse, Anmeldung im Browser, Job-Überwachung, E-Mail-Versand, Aufbewahrung. */
export class SettingsGeneral extends SettingsForm {
  static override properties = { ...SettingsForm.properties, _testing: { state: true } };

  declare _testing: boolean;
  protected readonly fields = FIELDS;
  readonly #channel = new LiveData<MailChannel>(this, { url: () => MAIL_CHANNEL_URL, transform: readMailChannel });

  constructor() {
    super();
    this._testing = false;
  }

  /** Mit dem Weg der Anmeldung wechselt der Server alle Sitzungen aus, auch diese: danach geht es zur Anmeldung. */
  #switchedSessionMode = false;

  protected override async request(body: Record<string, unknown>, draft: Draft, base: Draft): Promise<GlobalSettings | void> {
    this.#switchedSessionMode = 'browser_session_mode' in body;
    // Wer den Weg hier bewusst wählt, muss die einmalige Frage nach den Pflichtschritten nicht mehr beantworten.
    return super.request(this.#switchedSessionMode ? { ...body, browser_session_mode_confirmed: true } : body, draft, base);
  }

  protected override async afterSave(updated: GlobalSettings | void): Promise<void> {
    if (this.#switchedSessionMode) {
      this.#switchedSessionMode = false;
      await auth.leaveAfterSessionModeChange(router.state.path);
      return;
    }
    // Ohne E-Mail-Versand kann es auch keinen E-Mail-Kanal geben.
    if (updated && updated.mail_enabled !== true && this.#channel.data?.enabled) await settingsApi.setMailChannel(false);
  }

  protected override validate(draft: Draft): FieldErrors {
    const ttl = sessionTtlError(String(draft.session_ttl_minutes));
    return ttl ? { session_ttl_minutes: ttl } : {};
  }

  protected override fieldForCode(code: string): string | undefined {
    return code === 'settings.mailer_incomplete' ? 'mail_host' : undefined;
  }

  /** Moduswechsel: steht der Port auf einem Standardport, folgt er dem neuen Modus. */
  #setSecurity(security: MailSecurity): void {
    this.set('mail_security', security);
    this.set('mail_port', portForSecurity(this.value('mail_port'), security));
  }

  async #setChannel(enabled: boolean): Promise<void> {
    try {
      await settingsApi.setMailChannel(enabled);
    } catch (error) {
      toast({ tone: 'crit', title: errorText(error) });
    }
  }

  async #testMail(): Promise<void> {
    this._testing = true;
    try {
      await settingsApi.testMail();
      toast({ tone: 'ok', title: t('settings.general.mail.testSent') });
    } catch (error) {
      toast({ tone: 'crit', title: errorText(error) });
    } finally {
      this._testing = false;
    }
  }

  #session(settings: GlobalSettings): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.session.title')} subtitle=${t('settings.session.subtitle')}>
      <div class="fields">
        <lcm-session-mode-field
          .value=${this.value('browser_session_mode')}
          ?cookie-available=${settings.browser_session_cookie_available === true}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => this.set('browser_session_mode', e.detail.value)}
        ></lcm-session-mode-field>
        ${this.input('session_ttl_minutes', t('settings.security.session.ttl'), { hint: t('settings.security.session.ttlHint') })}
      </div>
    </lcm-card>`;
  }

  #ssh(settings: GlobalSettings): TemplateResult {
    const key = String(settings.onboarding_pub_key ?? '');
    return html`<lcm-card level="2" heading=${t('settings.general.ssh.title')} subtitle=${t('settings.general.ssh.subtitle')}>
        <div class="field-row">
          ${this.input('default_ssh_user', t('settings.general.ssh.user'), { placeholder: 'root', mono: true })}
          ${this.input('default_ssh_port', t('settings.general.ssh.port'), { width: 'xs' })}
        </div>
      </lcm-card>
      <lcm-card level="2" heading=${t('settings.general.key.title')} subtitle=${t('settings.general.key.subtitle')}>
        ${key
          ? html`<div class="fields">
              <lcm-copy-field block value=${key}></lcm-copy-field>
              <p class="muted">${t('settings.general.key.how')}</p>
            </div>`
          : html`<lcm-notice>${t('settings.general.key.none')}</lcm-notice>`}
      </lcm-card>`;
  }

  #address(draft: Draft): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.general.address.title')} subtitle=${t('settings.general.address.subtitle')}>
      <div class="fields">
        ${this.input('public_base_url', t('settings.general.address.label'), { placeholder: 'https://lcm.example.com', mono: true, width: 'l', hint: t('settings.general.address.hint') })}
        ${String(draft.public_base_url).trim() === '' ? html`<lcm-notice tone="warn">${t('settings.general.address.warning')}</lcm-notice>` : nothing}
      </div>
    </lcm-card>`;
  }

  #jobs(): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.general.jobs.title')} subtitle=${t('settings.general.jobs.subtitle')}>
      <div class="form-stack">
        ${this.input('job_idle_timeout_minutes', t('settings.general.jobs.idle'), { hint: t('settings.general.jobs.idleHint') })}
        ${this.input('job_idle_timeout_slow_minutes', t('settings.general.jobs.idleSlow'), { hint: t('settings.general.jobs.idleSlowHint') })}
        ${this.input('packages_stale_warn_hours', t('settings.general.jobs.staleWarn'), { hint: t('settings.general.jobs.staleHint') })}
        ${this.input('packages_stale_critical_days', t('settings.general.jobs.staleCrit'), { hint: t('settings.general.jobs.staleCritHint') })}
      </div>
    </lcm-card>`;
  }

  #mail(settings: GlobalSettings, draft: Draft): TemplateResult {
    const enabled = draft.mail_enabled === true;
    const savedEnabled = settings.mail_enabled === true;
    const channelOn = this.#channel.data?.enabled === true;
    return html`<lcm-card level="2" heading=${t('settings.general.mail.title')} subtitle=${t('settings.general.mail.subtitle')}>
      <div class="fields">
        ${this.flag('mail_enabled', t('settings.general.mail.enable'), { on: t('settings.general.mail.enableOn'), off: t('settings.general.mail.enableOff') })}
        <div class="field-row">
          ${this.input('mail_host', t('settings.general.mail.host'), { placeholder: 'smtp.example.com', mono: true, disabled: !enabled })}
          ${this.input('mail_port', t('settings.general.mail.port'), { width: 'xs', disabled: !enabled })}
        </div>
        ${mailSecurityField(this.value('mail_security'), (security) => this.#setSecurity(security), !enabled)}
        ${this.input('mail_username', t('settings.general.mail.user'), { disabled: !enabled })}
        ${this.input('mail_password', t('settings.general.mail.password'), {
          type: 'password',
          autocomplete: 'new-password',
          disabled: !enabled,
          placeholder: t('settings.form.unchangedSecret'),
        })}
        ${this.input('mail_from', t('settings.general.mail.from'), { placeholder: 'lcm@example.com', width: 'l', disabled: !enabled, hint: t('settings.general.mail.fromHint') })}
        ${this.input('mail_admin_recipients', t('settings.general.mail.admins'), { placeholder: 'admin@example.com', width: 'l', disabled: !enabled, hint: t('settings.general.mail.adminsHint') })}
        <lcm-switch
          label=${t('settings.general.mail.channel')}
          description=${t('settings.general.mail.channelHint')}
          on-label=${t('settings.form.on')}
          off-label=${t('settings.form.off')}
          on-text=${t('settings.general.mail.channelOn')}
          off-text=${t('settings.general.mail.channelOff')}
          ?disabled=${!savedEnabled}
          .checked=${channelOn && savedEnabled}
          @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => void this.#setChannel(e.detail.checked)}
        ></lcm-switch>
        <div class="row">
          <lcm-server-action
            label=${t('settings.general.mail.test')}
            icon="bell"
            help=${t('settings.general.mail.testHelp')}
            meaning=${t('settings.general.mail.testMeaning')}
            .labels=${helpLabels()}
            ?disabled=${!savedEnabled || this.dirty}
            ?busy=${this._testing}
            @click=${() => void this.#testMail()}
          ></lcm-server-action>
          ${this.dirty && savedEnabled ? html`<span class="muted">${t('settings.general.mail.saveFirst')}</span>` : nothing}
        </div>
      </div>
    </lcm-card>`;
  }

  #keeping(): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.general.keep.title')} subtitle=${t('settings.general.keep.subtitle')}>
      <div class="form-stack">
        ${this.input('log_retention_days', t('settings.general.keep.logs'), { hint: t('settings.general.keep.logsHint') })}
        ${this.input('routine_log_retention_days', t('settings.general.keep.routine'), { hint: t('settings.general.keep.routineHint') })}
        ${this.input('storage_history_retention_days', t('settings.general.keep.storage'), { hint: t('settings.general.keep.storageHint') })}
      </div>
    </lcm-card>`;
  }

  protected renderForm(settings: GlobalSettings, draft: Draft): TemplateResult {
    return html`${this.#ssh(settings)} ${this.#address(draft)} ${this.#session(settings)} ${this.#jobs()} ${this.#mail(settings, draft)} ${this.#keeping()}`;
  }
}
customElements.define('lcm-settings-general', SettingsGeneral);
