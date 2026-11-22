import { LitElement, css, html, nothing, type PropertyValues, type TemplateResult } from 'lit';
import '../../../components/lcm-button';
import '../../../components/lcm-dialog';
import '../../../components/lcm-notice';
import '../../../components/lcm-status';
import '../../../components/lcm-textarea';
import type { Impact } from '../../../components/lcm-dialog';
import { baseStyles, emit } from '../../../components/shared';
import { endpointsApi, type AgentPeer, type EndpointName, type NetworkEndpoint, type NetworkState } from '../../../api/endpoints';
import { I18nController } from '../../../lib/controllers';
import { errorText, t } from '../../../lib/i18n';
import { unsaved } from '../../../lib/unsaved';
import { settingsStyles } from '../system/settings-styles';
import { MAX_NETWORK_ENTRIES, formatNetworks, networkFailure, networkProblem, parseNetworks, sameNetworks } from './network-model';

/**
 * <lcm-network-group endpoint="ui" .state=${…}> – die erlaubten Netzbereiche eines Dienstes mit eigenem Speichern.
 * Die eigene Adresse lässt sich nicht aussperren (der Server lehnt ab und nennt sie), und würde ein engerer Bereich
 * verbundene Agenten trennen, fragt ein Dialog vorher nach. Meldet nach dem Speichern `lcm-network-saved`.
 */
export class LcmNetworkGroup extends LitElement {
  static override properties = {
    endpoint: { type: String },
    state: { attribute: false },
    _list: { state: true },
    _tried: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _saved: { state: true },
    _agents: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      section {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
      }
      h4 {
        margin: 0;
        font-size: var(--fs-m);
        font-weight: var(--fw-semibold);
      }
      p {
        margin: 0;
      }
    `,
  ];

  declare endpoint: EndpointName;
  /** Der gespeicherte Stand aller Dienste und die Adresse des Aufrufers. */
  declare state: NetworkState | undefined;
  /** Die Netzbereiche, wie sie im Feld stehen. */
  declare _list: string;
  declare _tried: boolean;
  declare _busy: boolean;
  declare _error: string;
  declare _saved: string;
  /** Verbundene Agenten, die ein engerer Bereich trennen würde; die Bestätigung steht an. */
  declare _agents: AgentPeer[] | undefined;

  #lastSaved: string[] | undefined;

  constructor() {
    super();
    this.endpoint = 'ui';
    this._list = '';
    this._tried = false;
    this._busy = false;
    this._error = '';
    this._saved = '';
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    unsaved.set(this, false);
  }

  /** Ein neuer Stand ersetzt das Feld nur, solange der Benutzer nichts getippt hat. */
  override willUpdate(changed: PropertyValues): void {
    if (changed.has('state')) {
      const saved = this.#saved?.allowed ?? [];
      if (this.#lastSaved === undefined || sameNetworks(parseNetworks(this._list), this.#lastSaved)) this._list = formatNetworks(saved);
      this.#lastSaved = saved;
    }
    unsaved.set(this, this.#dirty);
  }

  get #saved(): NetworkEndpoint | undefined {
    return this.state?.endpoints.find((item) => item.endpoint === this.endpoint);
  }

  get #dirty(): boolean {
    const saved = this.#saved;
    return !!saved && !sameNetworks(parseNetworks(this._list), saved.allowed);
  }

  async #save(confirm = false): Promise<void> {
    if (this._busy || !this.#dirty) return;
    const entries = parseNetworks(this._list);
    if (networkProblem(entries)) {
      this._tried = true;
      return;
    }
    this._busy = true;
    this._error = '';
    this._saved = '';
    try {
      const result = await endpointsApi.updateNetwork(this.endpoint, entries, confirm);
      this._agents = undefined;
      const count = result.disconnected.length;
      this._saved = count > 0 ? t('settings.endpoint.net.savedDisconnected', { count }) : t('settings.endpoint.net.saved');
      this.#lastSaved = entries;
      emit(this, 'lcm-network-saved');
    } catch (error) {
      this.#showSaveFailure(error);
    } finally {
      this._busy = false;
    }
  }

  #showSaveFailure(error: unknown): void {
    const failure = networkFailure(error);
    if (failure.kind === 'agentsOutside') this._agents = failure.agents;
    else if (failure.kind === 'lockout') this._error = `${errorText(error)}. ${t('settings.endpoint.net.lockoutHelp')}`;
    else this._error = errorText(error);
  }

  #problemText(): string {
    const problem = networkProblem(parseNetworks(this._list));
    if (!problem || !this._tried) return '';
    return problem.kind === 'invalid'
      ? t('settings.endpoint.net.invalid', { list: problem.entries.slice(0, 3).join(', ') })
      : t('settings.endpoint.net.tooMany', { max: MAX_NETWORK_ENTRIES });
  }

  #agentsDialog(): TemplateResult {
    const agents = this._agents ?? [];
    const impacts: Impact[] = agents.map((agent) => ({ label: agent.name || agent.id, value: agent.address || t('settings.endpoint.net.agentsDialog.unknownAddress'), tone: 'warn' }));
    return html`<lcm-dialog
      ?open=${this._agents !== undefined}
      tone="danger"
      heading=${t('settings.endpoint.net.agentsDialog.heading')}
      message=${t('settings.endpoint.net.agentsDialog.message', { count: agents.length })}
      confirm-label=${t('settings.endpoint.net.agentsDialog.confirm')}
      cancel-label=${t('settings.endpoint.net.agentsDialog.cancel')}
      .impacts=${impacts}
      ?busy=${this._busy}
      @lcm-confirm=${() => void this.#save(true)}
      @lcm-cancel=${() => (this._agents = undefined)}
    ></lcm-dialog>`;
  }

  override render() {
    if (!this.state) return nothing;
    const saved = this.#saved;
    const name = t(`settings.endpoint.name.${this.endpoint}`);
    const restricted = !!saved && saved.allowed.length > 0;
    const client = this.state.clientAddress;
    return html`<section data-endpoint=${this.endpoint}>
      <h4>${name}</h4>
      <p class="muted">${t(`settings.endpoint.net.intro.${this.endpoint}`)}</p>
      <div class="row">
        <lcm-status level=${restricted ? 'ok' : 'unknown'} label=${restricted ? t('settings.endpoint.net.restricted', { count: saved.allowed.length }) : t('settings.endpoint.net.all')}></lcm-status>
      </div>
      ${this._saved ? html`<lcm-notice tone="ok">${this._saved}</lcm-notice>` : nothing}
      <lcm-textarea
        data-field="networks"
        label=${t('settings.endpoint.net.field')}
        width="m"
        mono
        rows="4"
        placeholder=${'10.0.0.0/8\n192.168.1.0/24'}
        hint=${t('settings.endpoint.net.hint')}
        error=${this.#problemText()}
        .value=${this._list}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => ((this._list = e.detail.value), (this._saved = ''), (this._error = ''))}
      ></lcm-textarea>
      ${client
        ? html`<p class="muted">
            ${t('settings.endpoint.net.yourAddress')}: <span class="mono" data-field="client-address">${client}</span>
            ${this.endpoint === 'ui' ? html`<br />${t('settings.endpoint.net.yourAddressUi')}` : nothing}
          </p>`
        : nothing}
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="row">
        <lcm-button
          variant="primary"
          label=${t('settings.endpoint.net.saveFor', { name })}
          ?busy=${this._busy}
          ?disabled=${!this.#dirty}
          @click=${() => void this.#save()}
        >
          ${t('settings.endpoint.net.save')}
        </lcm-button>
      </div>
      ${this.#agentsDialog()}
    </section>`;
  }
}
customElements.define('lcm-network-group', LcmNetworkGroup);
