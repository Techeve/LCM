import { LitElement, css, html, nothing, type PropertyValues, type TemplateResult } from 'lit';
import '../../../components/lcm-button';
import '../../../components/lcm-card';
import '../../../components/lcm-notice';
import '../../../components/lcm-switch';
import '../../../components/lcm-textarea';
import { toast } from '../../../components/lcm-toaster';
import { baseStyles } from '../../../components/shared';
import { settingsApi, type GlobalSettings } from '../../../api/settings';
import { I18nController } from '../../../lib/controllers';
import { errorText, t } from '../../../lib/i18n';
import { unsaved } from '../../../lib/unsaved';
import { proxyBody, proxyChanged, proxyDraftOf, proxyDraftProblem, proxyProblemText, type ProxyDraft } from '../../setup/certificate-model';
import { settingsStyles } from '../system/settings-styles';

/**
 * <lcm-settings-proxy .settings> – Karte „Reverse-Proxy“ im Block Öffentliche Adressen, eingeklappt unter Let's Encrypt:
 * Schalter `trust_proxy_header` und die Adressen des Proxys (`trusted_proxies`), nur von denen glaubt LCM die Client-Adresse
 * aus `X-Forwarded-For`. So lässt sich später von Let's Encrypt auf einen Proxy davor wechseln. Wie Let's Encrypt speichert
 * die Karte für sich (`PATCH /settings/global`) und meldet ungespeicherte Eingaben im Register `unsaved`.
 */
export class LcmSettingsProxy extends LitElement {
  static override properties = {
    settings: { attribute: false },
    _draft: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _showProblem: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      p {
        margin: 0;
      }
    `,
  ];

  declare settings: GlobalSettings;
  declare _draft: ProxyDraft | undefined;
  declare _busy: boolean;
  declare _error: string;
  /** Die Prüfung zeigt ihren Fehler erst nach einem Versuch zu speichern. */
  declare _showProblem: boolean;
  #source: GlobalSettings | undefined;

  constructor() {
    super();
    this.settings = {};
    this._busy = false;
    this._error = '';
    this._showProblem = false;
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
  }

  /** Neue gespeicherte Werte ersetzen den Entwurf, solange er nicht geändert ist. */
  override willUpdate(): void {
    if (this.settings === this.#source) return;
    const wasDirty = this.#source !== undefined && this._draft !== undefined && proxyChanged(this._draft, this.#source);
    this.#source = this.settings;
    if (!this._draft || !wasDirty) this._draft = proxyDraftOf(this.settings);
  }

  override updated(changed: PropertyValues): void {
    super.updated(changed);
    unsaved.set(this, this.#dirty);
  }

  get #dirty(): boolean {
    return !!this._draft && proxyChanged(this._draft, this.settings);
  }

  #set(patch: Partial<ProxyDraft>): void {
    if (this._draft) this._draft = { ...this._draft, ...patch };
    this._showProblem = false;
    this._error = '';
  }

  #discard(): void {
    this._draft = proxyDraftOf(this.settings);
    this._showProblem = false;
    this._error = '';
  }

  async #save(): Promise<void> {
    const draft = this._draft;
    if (!draft || this._busy || !this.#dirty) return;
    if (proxyDraftProblem(draft)) {
      this._showProblem = true;
      return;
    }
    this._busy = true;
    this._error = '';
    try {
      await settingsApi.patchGlobal(proxyBody(draft));
      toast({ tone: 'ok', title: t('settings.proxy.saved') });
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #problemText(draft: ProxyDraft): string {
    const problem = this._showProblem ? proxyDraftProblem(draft) : null;
    if (!problem) return '';
    const { key, list } = proxyProblemText(problem);
    return t(key, { list });
  }

  #fields(draft: ProxyDraft): TemplateResult {
    return html`<div class="fields">
      <p class="muted">${t('settings.proxy.help')}</p>
      <lcm-switch
        data-field="trust-proxy"
        label=${t('settings.proxy.enable')}
        on-label=${t('settings.form.on')}
        off-label=${t('settings.form.off')}
        on-text=${t('settings.proxy.enableOn')}
        off-text=${t('settings.proxy.enableOff')}
        .checked=${draft.trust}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set({ trust: e.detail.checked })}
      ></lcm-switch>
      <lcm-textarea
        data-field="trusted-proxies"
        label=${t('setup.certificate.proxy.label')}
        width="m"
        mono
        rows="4"
        placeholder=${'10.0.0.1\n192.168.1.0/24'}
        hint=${t('setup.certificate.proxy.hint')}
        error=${this.#problemText(draft)}
        ?disabled=${!draft.trust}
        .value=${draft.entries}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ entries: e.detail.value })}
      ></lcm-textarea>
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
    </div>`;
  }

  override render() {
    const draft = this._draft;
    if (!draft) return nothing;
    const dirty = this.#dirty;
    return html`<lcm-card level="3" variant="inset" collapsible data-card="proxy" heading=${t('settings.proxy.title')} subtitle=${t('settings.proxy.subtitle')}>
      ${this.#fields(draft)}
      <div slot="footer" class="row">
        <lcm-button variant="primary" ?busy=${this._busy} ?disabled=${!dirty} @click=${() => void this.#save()}>${t('settings.acme.save')}</lcm-button>
        <lcm-button variant="ghost" ?disabled=${!dirty || this._busy} @click=${() => this.#discard()}>${t('settings.acme.discard')}</lcm-button>
      </div>
    </lcm-card>`;
  }
}
customElements.define('lcm-settings-proxy', LcmSettingsProxy);
