import { css, html, type TemplateResult } from 'lit';
import '../settings/endpoints/network-group';
import '../../components/lcm-skeleton';
import '../../components/lcm-notice';
import { NETWORK_URL, readNetworkState, type EndpointName, type NetworkState } from '../../api/endpoints';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { SERVICES, enabledOf } from '../settings/addresses/address-model';
import { SetupStep } from './setup-step';

/**
 * Schritt „Zugriff“: je für die Oberfläche und die eingeschalteten Dienste die Netzbereiche, aus denen sie erreichbar
 * sind (`lcm-network-group`, speichert für sich). Der Schritt selbst speichert nichts; ist eine Gruppe geändert und
 * nicht gespeichert, warnt er einmal vor dem Weitergehen.
 */
export class SetupAccess extends SetupStep {
  static override styles = [
    ...SetupStep.styles,
    css`
      .groups {
        display: grid;
        gap: var(--space-5);
      }
      lcm-network-group + lcm-network-group {
        padding-block-start: var(--space-5);
        border-block-start: 1px solid var(--line-muted);
      }
    `,
  ];

  readonly step = 'access' as const;
  protected override readonly warnUnsaved = true;
  readonly #network = new LiveData<NetworkState>(this, { url: () => NETWORK_URL, transform: readNetworkState });

  protected override async save(): Promise<void> {
    await this.advance();
  }

  /** Die Oberfläche und die Dienste, die an sind (in Modus C ohne Namen sind sie aus). */
  get #endpoints(): EndpointName[] {
    const enabled = enabledOf(this.settings);
    return SERVICES.filter((service) => enabled[service]);
  }

  protected override body(): TemplateResult {
    const state = this.#network.data;
    if (!state) {
      return this.#network.error
        ? html`<lcm-notice tone="crit">${this.#network.error}</lcm-notice>`
        : html`<lcm-skeleton shape="block" label=${t('settings.form.loading')}></lcm-skeleton>`;
    }
    return html`<div class="groups">
      ${this.#endpoints.map((endpoint) => html`<lcm-network-group endpoint=${endpoint} .state=${state}></lcm-network-group>`)}
    </div>`;
  }
}
customElements.define('lcm-setup-access', SetupAccess);
