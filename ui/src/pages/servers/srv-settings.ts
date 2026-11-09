import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-hover-tip';
import '../../components/lcm-input';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-switch';
import '../../components/lcm-tabs';
import { formLayoutStyles } from '../../components/layout-styles';
import type { TabItem } from '../../components/lcm-tabs';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { isApiDevice, refreshServer, serverUrl } from '../../api/servers';
import { normaliseServerState, settingsApi, settingsPatch, type ServerState, type SettingsForm } from '../../api/servers-settings';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { unsaved } from '../../lib/unsaved';
import { LIMITS } from '../../lib/validate';
import './set-access';
import './set-dns';
import './set-time';
import { addressSuggestions, settingsErrors, type SettingsErrors, type SettingsField } from './set-model';
import { settingsStyles } from './set-styles';

type Section = 'general' | 'operation' | 'access' | 'dns' | 'time';
type Flag = 'maintenance' | 'user_sync_disabled' | 'unreachable_uncritical' | 'docker_updates_disabled' | 'docker_cves_ignored' | 'console_disabled';

const SECTION_LABEL: Record<Section, string> = {
  general: 'serverPackages.settings.section.general',
  operation: 'serverPackages.settings.section.operation',
  access: 'serverPackages.settings.section.access',
  dns: 'serverPackages.settings.section.dns',
  time: 'serverPackages.settings.section.time',
};
/** Beschriftung, kurze Erklärung darunter und die ausführliche (an/aus) hinter dem Info-i der Schalter. */
const FLAG_TEXT: Record<Flag, { label: string; short: string; on: string; off: string }> = {
  maintenance: { label: 'serverPackages.settings.maintenance.label', short: 'serverPackages.settings.maintenance.short', on: 'serverPackages.settings.maintenance.on', off: 'serverPackages.settings.maintenance.off' },
  user_sync_disabled: { label: 'serverPackages.settings.user_sync_disabled.label', short: 'serverPackages.settings.user_sync_disabled.short', on: 'serverPackages.settings.user_sync_disabled.on', off: 'serverPackages.settings.user_sync_disabled.off' },
  unreachable_uncritical: { label: 'serverPackages.settings.unreachable_uncritical.label', short: 'serverPackages.settings.unreachable_uncritical.short', on: 'serverPackages.settings.unreachable_uncritical.on', off: 'serverPackages.settings.unreachable_uncritical.off' },
  docker_updates_disabled: { label: 'serverPackages.settings.docker_updates_disabled.label', short: 'serverPackages.settings.docker_updates_disabled.short', on: 'serverPackages.settings.docker_updates_disabled.on', off: 'serverPackages.settings.docker_updates_disabled.off' },
  docker_cves_ignored: { label: 'serverPackages.settings.docker_cves_ignored.label', short: 'serverPackages.settings.docker_cves_ignored.short', on: 'serverPackages.settings.docker_cves_ignored.on', off: 'serverPackages.settings.docker_cves_ignored.off' },
  console_disabled: { label: 'serverPackages.settings.console_disabled.label', short: 'serverPackages.settings.console_disabled.short', on: 'serverPackages.settings.console_disabled.on', off: 'serverPackages.settings.console_disabled.off' },
};


/**
 * Einstellungen eines Servers im mittigen Fenster (`?modal=settings`), in Abschnitte geteilt: Allgemein (Name,
 * Verbindung), Betrieb (Wartung, Erreichbarkeit, Benutzerabgleich, Docker), Zugang (Konsole, root-Login, SSH-Port),
 * DNS und Zeit. Was LCM nur speichert, sammelt „Speichern“; was auf dem Server etwas einrichtet, ist eine eigene
 * Aktion mit Bestätigung und läuft sofort.
 */
export class SrvSettings extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    serverId: { type: Number, attribute: 'server-id' },
    _form: { state: true },
    _section: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _touched: { state: true },
  };
  static override styles = [baseStyles, formLayoutStyles, settingsStyles, css`
    lcm-tabs {
      display: block;
      min-block-size: 24rem;
    }
    .flag-head {
      display: flex;
      align-items: flex-start;
      gap: var(--space-1);
    }
    .flag-head lcm-hover-tip p + p {
      margin-block-start: var(--space-2);
    }
    /* Unter der Beschriftung, eingerückt um Schalter (2,5rem) und Abstand wie in lcm-switch. */
    .flag .note {
      padding-inline-start: calc(2.5rem + var(--space-3));
    }
  `];

  declare open: boolean;
  declare serverId: number;
  declare _form: SettingsForm | undefined;
  declare _section: Section;
  declare _busy: boolean;
  declare _error: string;
  declare _touched: boolean;

  #base: SettingsForm | undefined;
  #seen: ServerState | undefined;
  readonly #server = new LiveData<ServerState>(this, {
    url: () => (this.open && this.serverId ? serverUrl(this.serverId) : null),
    topics: () => (this.open && this.serverId ? [`server:${this.serverId}`] : []),
    transform: normaliseServerState,
  });

  constructor() {
    super();
    this.open = false;
    this.serverId = 0;
    this._form = undefined;
    this._section = 'general';
    this._busy = false;
    this._error = '';
    this._touched = false;
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
  }

  override willUpdate(): void {
    if (!this.open) {
      this.#base = this._form = this.#seen = undefined;
      this._section = 'general';
      this._touched = false;
      this._error = '';
      return;
    }
    this.#adopt();
  }

  /** Neue Serverdaten (beim Öffnen, nach einer Aktion) übernehmen, solange nichts ungespeichert ist. */
  #adopt(): void {
    const data = this.#server.data;
    if (data && data !== this.#seen && !this.#dirty) {
      this.#seen = data;
      this.#base = data.form;
      this._form = { ...data.form };
    }
  }

  get #dirty(): boolean {
    return !!this._form && !!this.#base && JSON.stringify(this._form) !== JSON.stringify(this.#base);
  }

  #patch(change: Partial<SettingsForm>): void {
    if (!this._form) return;
    this._form = { ...this._form, ...change };
    unsaved.set(this, this.#dirty);
  }

  #close(): void {
    unsaved.set(this, false);
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  async #save(): Promise<void> {
    const form = this._form;
    const base = this.#base;
    if (!form || !base) return;
    this._touched = true;
    if (Object.keys(this.#errors(form)).length > 0) return;
    this._busy = true;
    this._error = '';
    try {
      const patch = settingsPatch(base, form);
      if (Object.keys(patch).length > 0) await settingsApi.update(this.serverId, patch);
      if (form.console_disabled !== base.console_disabled) await settingsApi.setConsoleDisabled(this.serverId, form.console_disabled);
      toast({ tone: 'ok', title: t('serverPackages.settings.saved', { name: form.name.trim() }) });
      refreshServer(this.serverId);
      this.#close();
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  // ---- Abschnitte ----

  #sections(state: ServerState): TabItem[] {
    const device = isApiDevice({ os_id: state.osId });
    const sections: Section[] = ['general', 'operation'];
    if (!device && (auth.can('servers:console') || state.transport !== 'agent')) sections.push('access');
    if (!device) sections.push('dns', 'time');
    return sections.map((id) => ({ id, label: t(SECTION_LABEL[id]) }));
  }

  /** Agent-Server haben keine Verbindungsadresse; der Agent meldet sich selbst. */
  get #ssh(): boolean {
    return this.#server.data?.transport !== 'agent';
  }

  #errors(form: SettingsForm): SettingsErrors {
    return this.#base ? settingsErrors(this.#base, form, this.#ssh) : {};
  }

  #errorFor(field: SettingsField, form: SettingsForm): string {
    const key = this._touched ? this.#errors(form)[field] : undefined;
    return key ? t(key) : '';
  }

  #flag(key: Flag, form: SettingsForm): TemplateResult {
    const text = FLAG_TEXT[key];
    const label = t(text.label);
    return html`<div class="flag">
      <div class="flag-head">
        <lcm-switch label=${label} .checked=${form[key]} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#patch({ [key]: e.detail.checked })}></lcm-switch>
        <lcm-hover-tip icon="info" tone="plain" label=${t('serverPackages.settings.more', { label })}><p>${t(text.on)}</p><p>${t(text.off)}</p></lcm-hover-tip>
      </div>
      <p class="note">${t(text.short)}</p>
    </div>`;
  }

  #renderGeneral(form: SettingsForm, state: ServerState): TemplateResult {
    const suggest = state.hostname && state.hostname !== form.name.trim() ? state.hostname : '';
    return html`<div class="form-stack">
      <div class="field-row">
        <lcm-input
          label=${t('serverPackages.settings.name')}
          width="l"
          maxlength=${LIMITS.name}
          hint=${t('serverPackages.settings.nameHint')}
          .value=${form.name}
          error=${this.#errorFor('name', form)}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ name: e.detail.value })}
        ></lcm-input>
      </div>
      ${suggest
        ? html`<div class="suggest">
            <lcm-button size="small" variant="ghost" icon="copy" @click=${() => this.#patch({ name: suggest })}>${t('serverPackages.settings.useHostname', { name: suggest })}</lcm-button>
          </div>`
        : nothing}
      ${this.#ssh ? this.#renderConnection(form, state) : nothing}
    </div>`;
  }

  #renderConnection(form: SettingsForm, state: ServerState): TemplateResult {
    const suggestions = addressSuggestions(state.ipAddresses, state.hostname, form.host);
    return html`<h3>${t('serverPackages.settings.connection')}</h3>
      <div class="field-row">
        <lcm-input
          label=${t('serverPackages.settings.host')}
          width="l"
          mono
          maxlength=${LIMITS.host}
          hint=${t('serverPackages.settings.hostHint')}
          .value=${form.host}
          error=${this.#errorFor('host', form)}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ host: e.detail.value })}
        ></lcm-input>
        <lcm-input
          label=${t('serverPackages.settings.port')}
          width="xs"
          mono
          digits
          maxlength="5"
          .value=${form.port ? String(form.port) : ''}
          error=${this.#errorFor('port', form)}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ port: Number(e.detail.value) })}
        ></lcm-input>
      </div>
      ${suggestions.length > 0
        ? html`<div class="presets suggest">
            <span class="label">${t('serverPackages.settings.suggestions')}</span>
            ${suggestions.map((address) => html`<lcm-button size="small" variant="ghost" icon="plus" @click=${() => this.#patch({ host: address })}><span class="mono">${address}</span></lcm-button>`)}
          </div>`
        : nothing}
      <p class="note">${t('serverPackages.settings.connectionNote')}</p>`;
  }

  #renderOperation(form: SettingsForm, state: ServerState): TemplateResult {
    const device = isApiDevice({ os_id: state.osId });
    return html`<div class="form-stack">
      ${this.#flag('maintenance', form)}
      ${form.maintenance && state.maintenanceSince && this.#base?.maintenance ? html`<p class="note warn">${t('serverPackages.settings.maintenanceSince', { date: formatDateTime(state.maintenanceSince) })}</p>` : nothing}
      ${this.#flag('unreachable_uncritical', form)}
      ${form.unreachable_uncritical
        ? html`<lcm-input
            label=${t('serverPackages.settings.grace.label')}
            width="s"
            digits
            maxlength="3"
            hint=${t('serverPackages.settings.grace.hint', { days: form.unreachable_grace_days || 0 })}
            error=${this.#errorFor('grace', form)}
            .value=${form.unreachable_grace_days ? String(form.unreachable_grace_days) : ''}
            @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ unreachable_grace_days: Number(e.detail.value) })}
          ></lcm-input>`
        : nothing}
      ${!state.proxmox && !device ? this.#flag('user_sync_disabled', form) : nothing}
      ${state.hasDocker
        ? html`<h3>${t('serverPackages.settings.docker')}</h3>
            ${this.#flag('docker_updates_disabled', form)} ${this.#flag('docker_cves_ignored', form)}
            ${form.docker_cves_ignored ? html`<p class="note warn">${t('serverPackages.settings.docker_cves_ignored.note')}</p>` : nothing}`
        : nothing}
    </div>`;
  }

  #renderAccess(form: SettingsForm, state: ServerState): TemplateResult {
    const ssh = state.transport !== 'agent';
    return html`<div class="form-stack">
      ${auth.can('servers:console') ? html`<h3>${t('serverPackages.settings.console')}</h3>${this.#flag('console_disabled', form)}` : nothing}
      ${ssh ? html`<lcm-set-access server-id=${this.serverId} server-name=${form.name} .state=${state}></lcm-set-access>` : nothing}
    </div>`;
  }

  #renderSection(form: SettingsForm, state: ServerState): TemplateResult {
    switch (this._section) {
      case 'operation':
        return this.#renderOperation(form, state);
      case 'access':
        return this.#renderAccess(form, state);
      case 'dns':
        return html`<lcm-set-dns server-id=${this.serverId} server-name=${form.name} .state=${state}></lcm-set-dns>`;
      case 'time':
        return html`<lcm-set-time server-id=${this.serverId} server-name=${form.name} .state=${state}></lcm-set-time>`;
      default:
        return this.#renderGeneral(form, state);
    }
  }

  override render() {
    const form = this._form;
    const state = this.#server.data;
    const invalid = !!form && this._touched && Object.keys(this.#errors(form)).length > 0;
    return html`<lcm-modal ?open=${this.open} size="ml" heading=${t('serverPackages.settings.title')} subtitle=${this.#base?.name ?? ''} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => this.#close()}>
      ${form && state
        ? html`<lcm-tabs label=${t('serverPackages.settings.title')} .tabs=${this.#sections(state)} value=${this._section} @lcm-tab-change=${(e: CustomEvent<{ id: Section }>) => (this._section = e.detail.id)}>
            <div class="panel">${this.#renderSection(form, state)}</div>
          </lcm-tabs>`
        : nothing}
      ${this._error ? html`<lcm-notice slot="note" tone="crit">${this._error}</lcm-notice>` : nothing}
      ${invalid && !this._error ? html`<lcm-notice slot="note" tone="crit">${t('serverPackages.settings.invalid.summary')}</lcm-notice>` : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t(this.#dirty ? 'servers.confirm.cancel' : 'serverPackages.settings.close')}</lcm-button>
      <lcm-button slot="footer" variant="primary" ?busy=${this._busy} ?disabled=${!this.#dirty} @click=${() => void this.#save()}>${t('serverPackages.settings.save')}</lcm-button>
    </lcm-modal>`;
  }
}
customElements.define('lcm-srv-settings', SrvSettings);
