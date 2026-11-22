import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-explainer';
import '../../components/lcm-notice';
import '../../components/lcm-server-action';
import '../../components/lcm-status';
import { toast } from '../../components/lcm-toaster';
import { MAIL_CHANNEL_URL, readMailChannel, settingsApi, type GlobalSettings, type MailChannel } from '../../api/settings';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { portForSecurity, type MailSecurity } from '../../lib/mail-security';
import { auth } from '../../lib/auth';
import { router } from '../../lib/router';
import { LIMITS } from '../../lib/validate';
import { addressHelp } from './addresses/address-help';
import { ADDRESS_FIELDS, PORT_KEY, redirectTarget, shownUrls, switchState, uiPortChange, validateAddresses } from './addresses/address-model';
import { MAIL_FIELDS, RETENTION_FIELDS, mailFields, retentionFields, type InputRenderer } from './general-fields';
import { onboardingGuide, onboardingGuideStyles } from './onboarding-guide';
import { onboardingUser } from './onboarding-model';
import './addresses/address-block';
import './session-mode-field';
import './system/copy-field';
import { sessionTtlError } from './access/security-model';
import { choice, int, text, type Draft, type FieldErrors, type FieldSpec } from './system/form-state';
import { helpLabels } from './system/help';
import { SettingsForm } from './system/settings-form';

const FIELDS: FieldSpec[] = [
  text('default_ssh_user', LIMITS.loginUser),
  int('default_ssh_port', 1, 65535),
  ...ADDRESS_FIELDS,
  choice('browser_session_mode', () => 'memory'),
  int('session_ttl_minutes', 0, 43200),
  int('job_idle_timeout_minutes', 0, 1440),
  int('job_idle_timeout_slow_minutes', 0, 1440),
  int('packages_stale_warn_hours', 24, 720),
  int('packages_stale_critical_days', 2, 90),
  ...MAIL_FIELDS,
  ...RETENTION_FIELDS,
];

/** Einstellungen → Allgemein: Onboarding (Vorgabe, Schlüssel, Agent), öffentliche Adressen, Anmeldung im Browser, Job-Überwachung, E-Mail-Versand, Aufbewahrung. */
export class SettingsGeneral extends SettingsForm {
  static override properties = { ...SettingsForm.properties, _testing: { state: true } };
  static override styles = [
    ...SettingsForm.styles,
    onboardingGuideStyles,
    css`
      .subcards {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
      /* Auf schmalen Bildschirmen steht die Hilfe zu den Adressen im Block selbst, nicht über der Seite. */
      @media (max-width: 75rem) {
        .layout.explain lcm-explainer {
          display: none;
        }
      }
    `,
  ];

  declare _testing: boolean;
  protected readonly fields = FIELDS;
  readonly #channel = new LiveData<MailChannel>(this, { url: () => MAIL_CHANNEL_URL, transform: readMailChannel });

  constructor() {
    super();
    this._testing = false;
  }

  /**
   * Mit dem Weg der Anmeldung wechselt der Server alle Sitzungen aus, auch diese: danach geht es zur Anmeldung und von dort
   * hierher zurück. Die Adresse steht vor dem Speichern fest: Meldet der Ereigniskanal das Ende der Sitzung vor der Antwort,
   * steht der Router schon auf der Anmeldung.
   */
  #returnAfterSwitch: string | undefined;
  /** Der Port der Oberfläche wechselt mit dem Speichern, und die Adresse danach: Die Seite folgt ihr. */
  #uiPortChange: { changed: boolean; announced: string | null } = { changed: false, announced: null };

  protected override async request(body: Record<string, unknown>, draft: Draft, base: Draft): Promise<GlobalSettings | void> {
    const switched = 'browser_session_mode' in body;
    this.#returnAfterSwitch = switched ? router.state.path : undefined;
    this.#uiPortChange = { changed: PORT_KEY.ui in body, announced: uiPortChange(draft, this.settings, window.location.href) };
    // Wer den Weg hier bewusst wählt, muss die einmalige Frage nach den Pflichtschritten nicht mehr beantworten.
    return super.request(switched ? { ...body, browser_session_mode_confirmed: true } : body, draft, base);
  }

  protected override async afterSave(updated: GlobalSettings | void): Promise<void> {
    if (this.#returnAfterSwitch) {
      await auth.leaveAfterSessionModeChange(this.#returnAfterSwitch);
      return;
    }
    // Ohne E-Mail-Versand kann es auch keinen E-Mail-Kanal geben.
    if (updated && updated.mail_enabled !== true && this.#channel.data?.enabled) await settingsApi.setMailChannel(false);
    const target = redirectTarget(updated, window.location.href, this.#uiPortChange.changed, this.#uiPortChange.announced);
    if (target) window.location.assign(target);
  }

  protected override validate(draft: Draft): FieldErrors {
    const ttl = sessionTtlError(String(draft.session_ttl_minutes));
    return { ...validateAddresses(draft), ...(ttl ? { session_ttl_minutes: ttl } : {}) };
  }

  protected override fieldForCode(code: string): string | undefined {
    return code === 'settings.mailer_incomplete' ? 'mail_host' : undefined;
  }

  readonly #input: InputRenderer = (key, label, options) => this.input(key, label, options);

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

  #sshDefaults(): TemplateResult {
    return html`<lcm-card level="3" variant="inset" collapsible ?open=${'default_ssh_user' in this._errors || 'default_ssh_port' in this._errors} data-card="ssh" heading=${t('settings.general.ssh.title')} subtitle=${t('settings.general.ssh.subtitle')}>
      <div class="field-row">
        ${this.input('default_ssh_user', t('settings.general.ssh.user'), { placeholder: 'root', mono: true })}
        ${this.input('default_ssh_port', t('settings.general.ssh.port'), { width: 'xs' })}
      </div>
    </lcm-card>`;
  }

  #onboardingKey(settings: GlobalSettings): TemplateResult {
    const key = String(settings.onboarding_pub_key ?? '');
    return html`<lcm-card level="3" variant="inset" collapsible data-card="key" heading=${t('settings.general.key.title')} subtitle=${t('settings.general.key.subtitle')}>
      ${key
        ? html`<div class="fields">
            <lcm-copy-field block value=${key}></lcm-copy-field>
            ${onboardingGuide(key, onboardingUser(settings.default_ssh_user))}
          </div>`
        : html`<lcm-notice>${t('settings.general.key.none')}</lcm-notice>`}
    </lcm-card>`;
  }

  /** Der Schalter für den Agenten steht im Block der Adressen; hier führt ein Knopf dorthin. */
  #toAddresses(): void {
    this.renderRoot.querySelector('lcm-address-block')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  /** Der Hinweis, solange die Agenten-Schnittstelle aus ist: warum und wo man sie einschaltet. */
  #agentOff(noName: boolean): TemplateResult {
    return html`<div class="row" data-field="agent-off">
      <span class="muted">${t(noName ? 'settings.general.agent.noDomain' : 'settings.general.agent.off')}</span>
      <lcm-button variant="ghost" size="small" icon="arrow-right" data-action="to-addresses" @click=${() => this.#toAddresses()}>${t('settings.general.agent.toAddresses')}</lcm-button>
    </div>`;
  }

  #agentMode(settings: GlobalSettings, draft: Draft): TemplateResult {
    const address = shownUrls(draft, settings).agent;
    const { checked, lock } = switchState(draft, settings, 'agent');
    return html`<lcm-card level="3" variant="inset" collapsible data-card="agent" heading=${t('settings.general.agent.title')} subtitle=${t('settings.general.agent.subtitle')}>
      <lcm-status slot="actions" data-field="agent-status" level=${checked ? 'ok' : 'unknown'} label=${t(checked ? 'settings.general.agent.statusOn' : 'settings.general.agent.statusOff')}></lcm-status>
      <div class="fields">
        <p class="muted">${t('settings.general.agent.text')}</p>
        ${checked ? nothing : this.#agentOff(lock === 'noName')}
        ${address ? html`<lcm-copy-field label=${t('settings.general.agent.address')} value=${address}></lcm-copy-field>` : nothing}
        <div class="row">
          <lcm-button variant="outline" icon="server" href="/servers/join?mode=agent">${t('settings.general.agent.join')}</lcm-button>
        </div>
      </div>
    </lcm-card>`;
  }

  #onboarding(settings: GlobalSettings, draft: Draft): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.general.onboarding.title')} subtitle=${t('settings.general.onboarding.subtitle')}>
      <div class="subcards">${this.#sshDefaults()} ${this.#onboardingKey(settings)} ${this.#agentMode(settings, draft)}</div>
    </lcm-card>`;
  }

  #onAddressChange(event: CustomEvent<{ values: Record<string, string | boolean> }>): void {
    for (const [key, value] of Object.entries(event.detail.values)) this.set(key, value);
  }

  #address(settings: GlobalSettings, draft: Draft): TemplateResult {
    return html`<lcm-address-block .settings=${settings} .draft=${draft} .errors=${this._errors} @lcm-address-change=${this.#onAddressChange}></lcm-address-block>`;
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
        ${mailFields(this.#input, this.value('mail_security'), (security) => this.#setSecurity(security), !enabled)}
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
        ${retentionFields(this.#input)}
      </div>
    </lcm-card>`;
  }

  protected override explainer(): TemplateResult {
    return html`<lcm-explainer
      topic="settings-addresses"
      heading=${t('settings.general.address.help.title')}
      collapse-label=${t('settings.form.collapse')}
      expand-label=${t('settings.form.expand')}
    >
      ${addressHelp(this._draft ?? {})}
    </lcm-explainer>`;
  }

  protected renderForm(settings: GlobalSettings, draft: Draft): TemplateResult {
    return html`${this.#onboarding(settings, draft)} ${this.#address(settings, draft)} ${this.#session(settings)} ${this.#jobs()} ${this.#mail(settings, draft)} ${this.#keeping()}`;
  }
}
customElements.define('lcm-settings-general', SettingsGeneral);
