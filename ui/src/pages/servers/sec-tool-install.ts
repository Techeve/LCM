import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-choice';
import '../../components/lcm-input';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import '../../components/lcm-switch';
import type { ChoiceOption } from '../../components/lcm-choice';
import { baseStyles } from '../../components/shared';
import { IP_ALLOWLISTS_URL, normalizeAllowlists, type IpAllowlist } from '../../api/groups';
import { securityApi, splitList, TOOL_NAMES, type LapiMode, type SecurityToolInstall, type ServerSecurity } from '../../api/servers-security';
import { GLOBAL_SETTINGS_URL, LAPIS_READ_URL, consoleConfigured, normaliseLapis, type Lapi } from '../../api/settings-more';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { LIMITS, invalidAddresses } from '../../lib/validate';
import './set-action';
import { secStyles } from './sec-styles';

type Choice = '' | 'fail2ban' | 'crowdsec' | 'ssh-2fa';

const DEFAULT_COLLECTIONS = 'crowdsecurity/sshd';

export interface InstallForm {
  tool: Choice;
  allowlistIps: string;
  allowlistIds: number[];
  bouncer: boolean;
  collections: string;
  lapiMode: LapiMode;
  lapiId: number;
}

export function emptyInstallForm(lcmSourceIp: string): InstallForm {
  return { tool: '', allowlistIps: lcmSourceIp, allowlistIds: [], bouncer: true, collections: DEFAULT_COLLECTIONS, lapiMode: 'local', lapiId: 0 };
}

// CrowdSec-Collections heißen `autor/name`; das Backend lässt Buchstaben, Ziffern und ._/- zu.
const COLLECTION = /^[A-Za-z0-9._/-]+$/;

/** Die Einträge der Allowlist und der Collections, die das Backend ablehnen würde. */
export function installErrors(form: InstallForm): { allowlistIps: string[]; collections: string[] } {
  return {
    allowlistIps: invalidAddresses(form.allowlistIps),
    collections: form.tool === 'crowdsec' ? splitList(form.collections).filter((entry) => !COLLECTION.test(entry)) : [],
  };
}

/** Die ersten Einträge für die Fehlermeldung. */
export function shownEntries(entries: string[]): string {
  return entries.length > 3 ? `${entries.slice(0, 3).join(', ')} …` : entries.join(', ');
}

/** Der Aufruf für fail2ban oder CrowdSec; SSH-2FA läuft über einen eigenen Endpunkt und ergibt `null`. */
export function installPayload(form: InstallForm): SecurityToolInstall | null {
  if (form.tool !== 'fail2ban' && form.tool !== 'crowdsec') return null;
  const base = { tool: form.tool, allowlist_ips: splitList(form.allowlistIps), allowlist_ids: [...form.allowlistIds] };
  if (form.tool === 'fail2ban') return base;
  return {
    ...base,
    bouncer: form.bouncer,
    collections: splitList(form.collections),
    lapi_mode: form.lapiMode,
    ...(form.lapiMode === 'remote' ? { lapi_id: form.lapiId } : {}),
  };
}

/**
 * `<lcm-srv-tool-install open server-id server-name .security>`: fail2ban, CrowdSec oder SSH-2FA einrichten
 * (`?modal=security-tool`). Die Adresse, über die LCM den Server erreicht, steht vorab in der Allowlist, damit sich LCM
 * nicht selbst aussperrt. Bereits Eingerichtetes ist nicht wählbar; verwaltet wird es im Reiter.
 */
export class SrvToolInstall extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    security: { attribute: false },
    _form: { state: true },
  };
  static override styles = [
    baseStyles,
    secStyles,
    css`
      .fields {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
    `,
  ];

  declare open: boolean;
  declare serverId: number;
  declare serverName: string;
  declare security: ServerSecurity | undefined;
  declare _form: InstallForm | undefined;

  readonly #allowlists = new LiveData<IpAllowlist[]>(this, { url: () => (this.open ? IP_ALLOWLISTS_URL : null), transform: normalizeAllowlists });
  readonly #lapis = new LiveData<Lapi[]>(this, { url: () => (this.open ? LAPIS_READ_URL : null), transform: normaliseLapis });
  readonly #console = new LiveData<boolean>(this, { url: () => (this.open && auth.can('settings:manage') ? GLOBAL_SETTINGS_URL : null), transform: consoleConfigured });

  constructor() {
    super();
    this.open = false;
    this.serverId = 0;
    this.serverName = '';
    this.security = undefined;
    this._form = undefined;
    new I18nController(this);
  }

  override willUpdate(): void {
    if (!this.open) {
      this._form = undefined;
      return;
    }
    if (!this._form && this.security) this._form = emptyInstallForm(this.security.lcmSourceIp);
    const lapis = this.#lapis.data ?? [];
    if (this._form && this._form.lapiId === 0 && lapis[0]) this._form = { ...this._form, lapiId: lapis[0].id };
  }

  #patch(change: Partial<InstallForm>): void {
    if (this._form) this._form = { ...this._form, ...change };
  }

  #close(): void {
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  /** Der Name des Jobs: „fail2ban einrichten“ bzw. „SSH-2FA einrichten“. */
  #jobLabel(form: InstallForm): string {
    const payload = installPayload(form);
    return t('serverSecurity.install.jobLabel', { tool: payload ? TOOL_NAMES[payload.tool] : t('serverSecurity.twoFactor.title') });
  }

  /** Einrichten mit Bestätigung: Die Blase und der Dialog zeigen die Befehle aus der Vorschau des Backends. */
  #renderSubmit(form: InstallForm | undefined, ready: boolean): TemplateResult {
    const payload = form ? installPayload(form) : null;
    const label = form && ready ? this.#jobLabel(form) : t('serverSecurity.install.submit');
    return html`<lcm-set-action
      slot="footer"
      variant="primary"
      icon="download"
      server-id=${this.serverId}
      server-name=${this.serverName}
      ?disabled=${!ready}
      action=${!ready ? '' : payload ? 'security-tool' : 'ssh-2fa-enable'}
      .params=${payload ? { tool: payload } : {}}
      label=${label}
      help=${t('serverSecurity.install.submitHelp')}
      heading=${t('previews.toolInstall.heading', { tool: payload ? TOOL_NAMES[payload.tool] : t('serverSecurity.twoFactor.title'), name: this.serverName })}
      .run=${() => (payload ? securityApi.installTool(this.serverId, payload) : securityApi.configureSsh2fa(this.serverId, true))}
      @lcm-done=${() => this.#close()}
    ></lcm-set-action>`;
  }

  #toolOptions(security: ServerSecurity): ChoiceOption[] {
    const already = t('serverSecurity.install.already');
    return [
      { id: 'fail2ban', label: TOOL_NAMES.fail2ban, icon: 'shield', description: security.installed.fail2ban ? already : t('serverSecurity.install.fail2ban'), disabled: security.installed.fail2ban },
      { id: 'crowdsec', label: TOOL_NAMES.crowdsec, icon: 'shield', description: security.installed.crowdsec ? already : t('serverSecurity.install.crowdsec'), disabled: security.installed.crowdsec },
      { id: 'ssh-2fa', label: t('serverSecurity.twoFactor.title'), icon: 'key', description: security.twoFactor ? already : t('serverSecurity.install.twoFactor'), disabled: security.twoFactor },
    ];
  }

  #lapiOptions(): ChoiceOption[] {
    const lapis = this.#lapis.data ?? [];
    const consoleReady = this.#console.data === true;
    const missing = t('serverSecurity.install.lapi.missing');
    return [
      { id: 'local', label: t('serverSecurity.install.lapi.local'), description: t('serverSecurity.install.lapi.localText') },
      { id: 'remote', label: t('serverSecurity.install.lapi.remote'), description: lapis.length > 0 ? t('serverSecurity.install.lapi.remoteText') : missing, disabled: lapis.length === 0 },
      { id: 'console', label: t('serverSecurity.install.lapi.console'), description: consoleReady ? t('serverSecurity.install.lapi.consoleText') : missing, disabled: !consoleReady },
    ];
  }

  #toggleList(form: InstallForm, id: number): void {
    this.#patch({ allowlistIds: form.allowlistIds.includes(id) ? form.allowlistIds.filter((x) => x !== id) : [...form.allowlistIds, id] });
  }

  #renderAllowlist(form: InstallForm): TemplateResult {
    const lists = this.#allowlists.data ?? [];
    const bad = installErrors(form).allowlistIps;
    return html`<lcm-input
        label=${t('serverSecurity.install.allowlist')}
        width="l"
        mono
        maxlength=${LIMITS.list}
        placeholder="203.0.113.7 198.51.100.0/24"
        .value=${form.allowlistIps}
        error=${bad.length > 0 ? t('check.addresses', { list: shownEntries(bad) }) : ''}
        hint=${t('serverSecurity.install.allowlistHint')}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ allowlistIps: e.detail.value })}
      ></lcm-input>
      ${lists.length > 0
        ? html`<div class="section">
            <h3>${t('groups.firewall.allowlists')}</h3>
            <div class="chips" role="group" aria-label=${t('groups.firewall.allowlists')}>
              ${lists.map((list) => html`<button class="chip" type="button" aria-pressed=${String(form.allowlistIds.includes(list.id))} @click=${() => this.#toggleList(form, list.id)}>${list.name}</button>`)}
            </div>
          </div>`
        : nothing}`;
  }

  #renderCrowdSec(form: InstallForm): TemplateResult {
    const lapis = this.#lapis.data ?? [];
    const badCollections = installErrors(form).collections;
    return html`<lcm-switch
        label=${t('serverSecurity.install.bouncer.label')}
        on-text=${t('serverSecurity.install.bouncer.on')}
        off-text=${t('serverSecurity.install.bouncer.off')}
        .checked=${form.bouncer}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#patch({ bouncer: e.detail.checked })}
      ></lcm-switch>
      <lcm-input
        label=${t('serverSecurity.install.collections')}
        width="l"
        mono
        maxlength=${LIMITS.list}
        placeholder=${DEFAULT_COLLECTIONS}
        .value=${form.collections}
        error=${badCollections.length > 0 ? t('check.collections', { list: shownEntries(badCollections) }) : ''}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#patch({ collections: e.detail.value })}
      ></lcm-input>
      <lcm-choice
        label=${t('serverSecurity.install.lapi.label')}
        variant="cards"
        .options=${this.#lapiOptions()}
        value=${form.lapiMode}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#patch({ lapiMode: e.detail.value === 'remote' || e.detail.value === 'console' ? e.detail.value : 'local' })}
      ></lcm-choice>
      ${form.lapiMode === 'remote' && lapis.length > 0
        ? html`<lcm-select
            label=${t('serverSecurity.install.lapi.choose')}
            width="l"
            .options=${lapis.map((l) => ({ value: String(l.id), label: `${l.name} (${l.url})` }))}
            .value=${String(form.lapiId)}
            @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#patch({ lapiId: Number(e.detail.value) })}
          ></lcm-select>`
        : nothing}`;
  }

  #renderBody(security: ServerSecurity, form: InstallForm): TemplateResult {
    if (security.restrictedSudo) return html`<lcm-notice tone="warn">${t('serverSecurity.restricted')}</lcm-notice>`;
    return html`<div class="fields">
      <p class="lead">${t('serverSecurity.install.lead')}</p>
      <lcm-choice label=${t('serverSecurity.install.tool')} variant="cards" .options=${this.#toolOptions(security)} value=${form.tool} @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#patch({ tool: e.detail.value as Choice })}></lcm-choice>
      ${form.tool === 'ssh-2fa'
        ? html`<p class="muted">${t('serverSecurity.twoFactor.intro')}</p>
            <lcm-notice tone="warn">${t('serverSecurity.twoFactor.installWarn')}</lcm-notice>`
        : nothing}
      ${form.tool === 'fail2ban' || form.tool === 'crowdsec' ? this.#renderAllowlist(form) : nothing} ${form.tool === 'crowdsec' ? this.#renderCrowdSec(form) : nothing}
    </div>`;
  }

  override render() {
    const security = this.security;
    const form = this._form;
    const errors = form ? installErrors(form) : undefined;
    const ready = !!form && form.tool !== '' && !!security && !security.restrictedSudo && errors?.allowlistIps.length === 0 && errors.collections.length === 0;
    return html`<lcm-modal ?open=${this.open} size="m" heading=${t('serverSecurity.install.title')} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => this.#close()}>
      ${security && form ? this.#renderBody(security, form) : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t('servers.confirm.cancel')}</lcm-button>
      ${this.#renderSubmit(form, ready)}
    </lcm-modal>`;
  }
}
customElements.define('lcm-srv-tool-install', SrvToolInstall);
