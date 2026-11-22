import { css, html, type TemplateResult } from 'lit';
import { t } from '../../lib/i18n';
import { SWITCHED, type SwitchedService } from '../settings/addresses/address-model';
import { serviceSwitch } from '../settings/addresses/service-switch';
import type { Draft } from '../settings/system/form-state';
import { servicesBody, setupDraft } from './setup-model';
import { SetupStep } from './setup-step';

const SENTENCE: Record<SwitchedService, string> = { agent: 'setup.services.agent', mcp: 'setup.services.mcp' };

/**
 * Schritt „Dienste“: je ein Schalter für Agent und MCP mit einem Satz dazu (die Schalter der Einstellungen, `serviceSwitch`).
 * In Modus C steht ein Dienst ohne Namen aus und gesperrt da.
 */
export class SetupServices extends SetupStep {
  static override properties = { ...SetupStep.properties, _draft: { state: true } };
  static override styles = [
    ...SetupStep.styles,
    css`
      .services {
        display: grid;
        gap: var(--space-5);
      }
      .service {
        display: grid;
        gap: var(--space-2);
      }
      .service p {
        margin: 0;
      }
    `,
  ];

  readonly step = 'services' as const;
  declare _draft: Draft | undefined;
  #base: Draft | undefined;

  override willUpdate(): void {
    if (this._draft) return;
    this.#base = setupDraft(this.settings);
    this._draft = { ...this.#base };
  }

  protected override async save(): Promise<void> {
    if (this._draft && this.#base) await this.advance(servicesBody(this._draft, this.#base));
  }

  #service(service: SwitchedService, draft: Draft): TemplateResult {
    return html`<div class="service">
      <p class="lead">${t(SENTENCE[service])}</p>
      ${serviceSwitch({
        service,
        draft,
        settings: this.settings,
        onChange: (key, checked) => (this._draft = { ...draft, [key]: checked }),
        noNameHint: t('setup.services.noDomain'),
      })}
    </div>`;
  }

  protected override body(): TemplateResult {
    const draft = this._draft ?? {};
    return html`<div class="services">${SWITCHED.map((service) => this.#service(service, draft))}</div>`;
  }
}
customElements.define('lcm-setup-services', SetupServices);
