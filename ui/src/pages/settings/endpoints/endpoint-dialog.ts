import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../../components/lcm-notice';
import '../../../components/lcm-skeleton';
import '../packages/settings-modal';
import './cert-section';
import './network-group';
import { baseStyles, emit } from '../../../components/shared';
import { ApiError } from '../../../lib/api';
import { endpointsApi, type EndpointName, type NetworkState, type TlsStatus } from '../../../api/endpoints';
import { I18nController } from '../../../lib/controllers';
import { errorText, t } from '../../../lib/i18n';
import { settingsStyles } from '../system/settings-styles';
import { certificateOf } from './cert-model';

/**
 * <lcm-endpoint-dialog endpoint="ui" ?open draft-address="https://…"> – „Zertifikat & Sicherheit“ eines Dienstes
 * (Weboberfläche, Agent, MCP): das Zertifikat, das LCM für den Namen seiner Adresse ausliefert, Hochladen von Hand
 * und Entfernen. Zertifikate ändern sich sofort. `draft-address` ist die Adresse, wie die Seite sie gerade
 * zeigt; weicht sie von der gespeicherten ab, sagt das Fenster es. Darunter stehen die erlaubten Netzbereiche des
 * Dienstes (`lcm-network-group`, speichert für sich). Meldet `lcm-close`.
 */
export class LcmEndpointDialog extends LitElement {
  static override properties = {
    endpoint: { type: String },
    open: { type: Boolean },
    draftAddress: { type: String, attribute: 'draft-address' },
    _tls: { state: true },
    _unavailable: { state: true },
    _network: { state: true },
    _loadError: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      .body {
        display: flex;
        flex-direction: column;
        gap: var(--space-6);
      }
      section.access {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        padding-block-start: var(--space-5);
        border-block-start: 1px solid var(--line-muted);
      }
      h3 {
        margin: 0;
        font-size: var(--fs-l);
        font-weight: var(--head-weight);
      }
      section.access p {
        margin: 0;
      }
    `,
  ];

  declare endpoint: EndpointName;
  declare open: boolean;
  declare draftAddress: string;
  declare _tls: TlsStatus | undefined;
  /** LCM läuft ohne TLS (--dev): Es gibt keine Zertifikate. */
  declare _unavailable: boolean;
  declare _network: NetworkState | undefined;
  declare _loadError: string;

  constructor() {
    super();
    this.endpoint = 'ui';
    this.open = false;
    this.draftAddress = '';
    this._unavailable = false;
    this._loadError = '';
    new I18nController(this);
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('open') && this.open) void this.#load();
    if (changed.has('open') && !this.open) this.#reset();
  }

  #reset(): void {
    this._tls = this._network = undefined;
    this._unavailable = false;
    this._loadError = '';
  }

  async #load(): Promise<void> {
    this.#reset();
    const [tls, network] = await Promise.allSettled([endpointsApi.getTls(), endpointsApi.getNetwork()]);
    if (tls.status === 'fulfilled') this._tls = tls.value;
    else if (tls.reason instanceof ApiError && tls.reason.code === 'tls.unavailable') this._unavailable = true;
    else this._loadError = errorText(tls.reason);
    if (network.status === 'fulfilled') this._network = network.value;
    else this._loadError ||= errorText(network.reason);
  }

  async #reloadNetwork(): Promise<void> {
    try {
      this._network = await endpointsApi.getNetwork();
    } catch (error) {
      this._loadError = errorText(error);
    }
  }

  #close(): void {
    emit(this, 'lcm-close');
  }

  #onTlsChanged(event: CustomEvent<{ status: TlsStatus }>): void {
    event.stopPropagation();
    this._tls = event.detail.status;
  }

  #notSavedNote(): TemplateResult | typeof nothing {
    if (!this._tls) return nothing;
    const saved = certificateOf(this._tls, this.endpoint)?.url ?? '';
    return this.draftAddress.trim() === saved ? nothing : html`<lcm-notice tone="warn">${t('settings.endpoint.draftDiffers')}</lcm-notice>`;
  }

  #content(): TemplateResult {
    if (this._loadError) return html`<lcm-notice tone="crit">${this._loadError}</lcm-notice>`;
    if (!this._network || (!this._tls && !this._unavailable)) return html`<lcm-skeleton shape="block" label=${t('settings.endpoint.loading')}></lcm-skeleton>`;
    return html`<div class="body">
      ${this.#notSavedNote()}
      <lcm-cert-section .status=${this._tls} endpoint=${this.endpoint} ?unavailable=${this._unavailable} @lcm-tls-changed=${this.#onTlsChanged}></lcm-cert-section>
      <section class="access">
        <h3>${t('settings.access.title')}</h3>
        <p class="muted">${t('settings.access.subtitle')}</p>
        <lcm-network-group endpoint=${this.endpoint} .state=${this._network} @lcm-network-saved=${() => void this.#reloadNetwork()}></lcm-network-group>
      </section>
    </div>`;
  }

  override render() {
    const address = this._tls ? (certificateOf(this._tls, this.endpoint)?.url ?? '') : '';
    return html`<lcm-settings-modal
      ?open=${this.open}
      size="ml"
      heading=${t('settings.endpoint.heading', { name: t(`settings.endpoint.name.${this.endpoint}`) })}
      subtitle=${address || (this._tls ? t('settings.endpoint.noAddress') : '')}
      read-only
      @lcm-close=${this.#close}
    >
      ${this.#content()}
    </lcm-settings-modal>`;
  }
}
customElements.define('lcm-endpoint-dialog', LcmEndpointDialog);
