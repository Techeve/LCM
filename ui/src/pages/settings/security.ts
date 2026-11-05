import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import '../../components/lcm-notice';
import '../../components/lcm-server-action';
import '../../components/lcm-status';
import { toast } from '../../components/lcm-toaster';
import { ADVISORY_STATUS_URL, ROLES_URL, readAdvisoryStatus, readRoles, settingsApi, type AdvisoryStatus, type GlobalSettings, type Role } from '../../api/settings';
import { auth } from '../../lib/auth';
import { formatDateTime } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { jobLink } from '../../lib/job-link';
import { LiveData } from '../../lib/live-data';
import { store } from '../../lib/store';
import { LIMITS } from '../../lib/validate';
import { assignableRoles } from './access/users-model';
import { cronError, hitsSelfWithout2fa, toggleTwoFactorRole, twoFactorRoles } from './access/security-model';
import { flag, int, text, type Draft, type FieldErrors, type FieldSpec } from './system/form-state';
import { helpLabels } from './system/help';
import { watchJob } from '../../lib/job-watch';
import { SettingsForm } from './system/settings-form';

const FIELDS: FieldSpec[] = [
  text('require_2fa_roles', LIMITS.list),
  flag('cve_scan_enabled'),
  text('cve_scan_cron', LIMITS.cron),
  flag('apt_include_phased'),
  text('cve_high_weight_packages', LIMITS.list, 'text'),
  flag('advisory_polling_enabled'),
  flag('advisory_local_copy'),
  int('advisory_cache_ttl_minutes', 0, 30),
];

/** Einstellungen → Sicherheit: Anmeldung (2FA-Pflicht, Sitzungsdauer), CVE-Scan und Frühwarnung. */
export class SettingsSecurity extends SettingsForm {
  static override properties = { ...SettingsForm.properties, _mirroring: { state: true } };
  static override styles = [
    ...SettingsForm.styles,
    css`
      fieldset {
        margin: 0;
        padding: 0;
        border: 0;
      }
      legend {
        margin-block-end: var(--space-2);
        color: var(--text-1);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .role {
        display: flex;
        align-items: flex-start;
        gap: var(--space-2);
        padding-block: var(--space-1);
        font-size: var(--fs-s);
      }
      .role input {
        margin-block-start: 3px;
        accent-color: var(--accent);
      }
      .role span {
        display: flex;
        flex-direction: column;
      }
      .role small {
        color: var(--text-2);
      }
    `,
  ];

  declare _mirroring: boolean;
  protected readonly fields = FIELDS;
  readonly #roles = new LiveData<Role[]>(this, { url: () => (auth.can('roles:read') ? ROLES_URL : null), transform: readRoles });
  readonly #status = new LiveData<AdvisoryStatus>(this, { url: () => (auth.can('packages:read') ? ADVISORY_STATUS_URL : null), transform: readAdvisoryStatus });
  #stopWatch: (() => void) | undefined;

  constructor() {
    super();
    this._mirroring = false;
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stopWatch?.();
  }

  protected override validate(draft: Draft): FieldErrors {
    const errors: FieldErrors = {};
    const cron = cronError(String(draft.cve_scan_cron));
    if (cron) errors.cve_scan_cron = cron;
    return errors;
  }

  protected override async afterSave(): Promise<void> {
    // Der Zustand der Frühwarnung hängt an den Einstellungen (eine lokale Kopie ohne Daten gilt als „aus“).
    store.invalidate(ADVISORY_STATUS_URL);
  }

  async #mirror(): Promise<void> {
    this._mirroring = true;
    try {
      const { jobId } = await settingsApi.mirrorAdvisories();
      toast({ tone: 'info', title: t('settings.security.advisory.mirrorStarted') });
      this.#stopWatch?.();
      this.#stopWatch = watchJob(jobId, (status) => {
        this._mirroring = false;
        store.invalidate(ADVISORY_STATUS_URL);
        if (status === 'failed') toast({ tone: 'crit', title: t('settings.security.advisory.mirrorFailed'), ...jobLink(jobId) });
        else toast({ tone: 'ok', title: t('settings.security.advisory.mirrorDone'), background: true, ...jobLink(jobId) });
      });
      if (!jobId) this._mirroring = false;
    } catch (error) {
      this._mirroring = false;
      toast({ tone: 'crit', title: errorText(error) });
    }
  }

  #twoFactor(draft: Draft): TemplateResult {
    const chosen = twoFactorRoles(String(draft.require_2fa_roles));
    const roles = assignableRoles(this.#roles.data ?? []);
    const field = roles.length
      ? html`<fieldset>
          <legend>${t('settings.security.login.twofaRoles')}</legend>
          ${roles.map(
            (role) => html`<label class="role">
              <input
                type="checkbox"
                .checked=${chosen.includes(role.name.toLowerCase())}
                @change=${() => this.set('require_2fa_roles', toggleTwoFactorRole(this.value('require_2fa_roles'), role.name))}
              />
              <span>${role.name}${role.description ? html`<small>${role.description}</small>` : nothing}</span>
            </label>`,
          )}
          ${this._errors.require_2fa_roles ? html`<lcm-notice tone="crit">${this._errors.require_2fa_roles}</lcm-notice>` : nothing}
        </fieldset>`
      : this.input('require_2fa_roles', t('settings.security.login.twofaRoles'), { mono: true, placeholder: 'admin,manager', hint: t('settings.security.login.twofaRolesHint') });
    const user = auth.user;
    const self = user ? hitsSelfWithout2fa(String(draft.require_2fa_roles), user.roles, user.totp_enabled) : false;
    return html`${field} ${chosen.includes('admin') ? nothing : html`<lcm-notice tone="warn">${t('settings.security.login.adminWarning')}</lcm-notice>`}
    ${self ? html`<lcm-notice tone="warn">${t('settings.security.login.selfWarning')}</lcm-notice>` : nothing}`;
  }

  #login(draft: Draft): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.security.login.title')} subtitle=${t('settings.security.login.subtitle')}>
      <div class="fields">
        ${this.#twoFactor(draft)}
      </div>
    </lcm-card>`;
  }

  #cve(draft: Draft): TemplateResult {
    const enabled = draft.cve_scan_enabled === true;
    return html`<lcm-card level="2" heading=${t('settings.security.cve.title')} subtitle=${t('settings.security.cve.subtitle')}>
      <div class="fields">
        ${this.flag('cve_scan_enabled', t('settings.security.cve.enable'), { on: t('settings.security.cve.enableOn'), off: t('settings.security.cve.enableOff') })}
        ${this.input('cve_scan_cron', t('settings.security.cve.cron'), { mono: true, width: 's', placeholder: '30 2 * * *', hint: t('settings.security.cve.cronHint'), disabled: !enabled })}
        ${this.flag('apt_include_phased', t('settings.security.cve.phased'), { on: t('settings.security.cve.phasedOn'), off: t('settings.security.cve.phasedOff') })}
        ${this.area('cve_high_weight_packages', t('settings.security.cve.weight'), { rows: 3, width: 'l', placeholder: 'nginx, apache2, openssh-server, postgresql', hint: t('settings.security.cve.weightHint') })}
      </div>
    </lcm-card>`;
  }

  #statusFacts(status: AdvisoryStatus): TemplateResult {
    return html`<dl class="facts">
        <dt>${t('settings.security.advisory.state')}</dt>
        <dd>${status.enabled ? (status.localCopy ? t('settings.security.advisory.stateLocal') : t('settings.security.advisory.stateOnline')) : t('settings.security.advisory.stateOff')}</dd>
        <dt>${t('settings.security.advisory.lastPoll')}</dt>
        <dd>${status.lastPollAt ? formatDateTime(status.lastPollAt) : t('settings.security.advisory.never')}</dd>
        ${status.localCopy
          ? html`<dt>${t('settings.security.advisory.mirrored')}</dt>
              <dd>${status.mirroredAt ? formatDateTime(status.mirroredAt) : t('settings.security.advisory.never')}</dd>
              <dt>${t('settings.security.advisory.systems')}</dt>
              <dd class=${status.mirrorable.length ? '' : 'warn'}>${status.mirrorable.length ? status.mirrorable.join(', ') : t('settings.security.advisory.noSystems')}</dd>`
          : nothing}
      </dl>
      ${status.localCopy
        ? html`<div class="row">
            <lcm-server-action
              icon="download"
              label=${t('settings.security.advisory.mirror')}
              help=${t('settings.security.advisory.mirrorHelp')}
              meaning=${t('settings.security.advisory.mirrorMeaning')}
              .labels=${helpLabels()}
              ?disabled=${status.mirrorable.length === 0}
              ?busy=${this._mirroring}
              @click=${() => void this.#mirror()}
            ></lcm-server-action>
          </div>`
        : nothing}`;
  }

  #advisory(settings: GlobalSettings, draft: Draft): TemplateResult {
    const enabled = draft.advisory_polling_enabled === true;
    const status = this.#status.data;
    const level = !status?.enabled ? 'unknown' : status.lastPollAt ? 'great' : 'warn';
    return html`<lcm-card level="2" heading=${t('settings.security.advisory.title')} subtitle=${t('settings.security.advisory.subtitle')}>
      ${status ? html`<lcm-status slot="actions" level=${level} label=${status.enabled ? t('settings.security.advisory.on') : t('settings.security.advisory.off')}></lcm-status>` : nothing}
      <div class="fields">
        ${settings.advisory_polling_enabled !== true || draft.advisory_local_copy !== true ? html`<lcm-notice>${t('settings.security.advisory.privacy')}</lcm-notice>` : nothing}
        ${this.flag('advisory_polling_enabled', t('settings.security.advisory.enable'), { on: t('settings.security.advisory.enableOn'), off: t('settings.security.advisory.enableOff') })}
        ${this.flag('advisory_local_copy', t('settings.security.advisory.local'), { on: t('settings.security.advisory.localOn'), off: t('settings.security.advisory.localOff'), disabled: !enabled })}
        ${this.input('advisory_cache_ttl_minutes', t('settings.security.advisory.ttl'), { hint: t('settings.security.advisory.ttlHint'), disabled: !enabled })}
        ${status ? this.#statusFacts(status) : nothing}
      </div>
    </lcm-card>`;
  }

  protected renderForm(settings: GlobalSettings, draft: Draft): TemplateResult {
    return html`${this.#login(draft)} ${this.#cve(draft)} ${this.#advisory(settings, draft)}`;
  }
}
customElements.define('lcm-settings-security', SettingsSecurity);
