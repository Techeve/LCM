import { LitElement, css, html, nothing, type PropertyValues, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import { baseStyles } from '../../components/shared';
import { api } from '../../lib/api';
import { GLOBAL_URL, readGlobal, type GlobalSettings } from '../../api/settings';
import { normaliseServers, serversUrl } from '../../api/servers';
import { setupStepOf } from '../../lib/address-setup';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { WIZARD_STEPS, isWizardStep, stepNumber, type WizardStep } from '../../lib/setup-steps';
import { formStyles } from '../account/form-styles';
import { lcmHostPath } from './setup-model';
import type { StepDone } from './setup-step';
import './step-access';
import './step-addresses';
import './step-backup';
import './step-certificate';
import './step-mail';
import './step-onboarding';
import './step-retention';
import './step-services';
import './step-session';
import './step-subscription';

const TEXT: Record<WizardStep, { title: string; lead: string }> = {
  session: { title: 'setup.steps.session.title', lead: 'setup.steps.session.lead' },
  addresses: { title: 'setup.steps.addresses.title', lead: 'setup.steps.addresses.lead' },
  services: { title: 'setup.steps.services.title', lead: 'setup.steps.services.lead' },
  access: { title: 'setup.steps.access.title', lead: 'setup.steps.access.lead' },
  certificate: { title: 'setup.steps.certificate.title', lead: 'setup.steps.certificate.lead' },
  onboarding: { title: 'setup.steps.onboarding.title', lead: 'setup.steps.onboarding.lead' },
  mail: { title: 'setup.steps.mail.title', lead: 'setup.steps.mail.lead' },
  retention: { title: 'setup.steps.retention.title', lead: 'setup.steps.retention.lead' },
  backup: { title: 'setup.steps.backup.title', lead: 'setup.steps.backup.lead' },
  subscription: { title: 'setup.steps.subscription.title', lead: 'setup.steps.subscription.lead' },
};

/**
 * Einrichtungsassistent (`/setup`): zehn Schritte nach Passwort und 2FA des ersten Administrators. Welcher Schritt
 * offen ist, sagt der Server (`setup_step` am Konto), damit der Assistent nach einem Wechsel der Adresse und neuer
 * Anmeldung an derselben Stelle weitermacht. Jeder Schritt (`step-*.ts`) speichert für sich und meldet `lcm-step-done`;
 * hier stehen Überschrift, Fortschritt und der Wechsel zum nächsten Schritt. Nach dem letzten Schritt geht es zur
 * Seite des LCM-Hosts, wo die Einführung wartet.
 */
export class SetupPage extends LitElement {
  static override properties = { _settings: { state: true } };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      /* Inhalt und Hilfe-Spalte wie auf den Einstellungsseiten; Überschrift und Fortschritt stehen über dem Inhalt. */
      :host {
        display: grid;
        grid-template-columns: minmax(0, calc(var(--content-w, 48rem) + var(--gap) + 21rem));
        justify-content: start;
      }
      section {
        display: grid;
        gap: var(--gap);
        min-inline-size: 0;
      }
      .head {
        display: grid;
        gap: var(--gap);
        max-inline-size: var(--content-w, 48rem);
      }
      h1 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .progress {
        display: grid;
        gap: var(--space-2);
      }
      .count {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .bar {
        display: grid;
        grid-template-columns: repeat(${WIZARD_STEPS.length}, minmax(0, 1fr));
        gap: var(--space-1);
      }
      .bar span {
        block-size: 0.25rem;
        border-radius: var(--radius-s);
        background: var(--line);
      }
      .bar span.reached {
        background: var(--accent);
      }
    `,
  ];

  declare _settings: GlobalSettings | undefined;

  readonly #loaded = new LiveData<GlobalSettings>(this, { url: () => GLOBAL_URL, transform: readGlobal });
  #source: GlobalSettings | undefined;
  #leaving = false;

  constructor() {
    super();
    this._settings = undefined;
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  /** Die Einstellungen des Servers ersetzen, was die Seite von einer Antwort übernommen hat; sie sind neuer oder gleich. */
  override willUpdate(): void {
    const data = this.#loaded.data;
    if (data && data !== this.#source) this._settings = this.#source = data;
  }

  override updated(changed: PropertyValues): void {
    super.updated(changed);
    if (!isWizardStep(setupStepOf(auth.user)) && !this.#leaving) void this.#leave();
  }

  /** Nichts offen (etwa ein Lesezeichen) oder alles getan: Bei der Einführung zum LCM-Host, sonst zur Startseite. */
  async #leave(): Promise<void> {
    this.#leaving = true;
    const intro = setupStepOf(auth.user) === 'intro';
    await router.navigate(intro ? await this.#hostPath() : '/', { replace: true });
  }

  async #hostPath(): Promise<string> {
    try {
      const page = normaliseServers(await api.get<unknown>(serversUrl({ q: '', status: '', os: '', kernel: '', group: '', incapable: false, sort: 'name', page: 1 })));
      return lcmHostPath(page.items);
    } catch {
      return '/';
    }
  }

  async #onDone(event: CustomEvent<StepDone>): Promise<void> {
    this._settings = { ...this._settings, ...event.detail.settings };
    await auth.refresh();
  }

  #step(step: WizardStep, settings: GlobalSettings): TemplateResult {
    const done = (event: CustomEvent<StepDone>) => void this.#onDone(event);
    switch (step) {
      case 'session':
        return html`<lcm-setup-session .settings=${settings} @lcm-step-done=${done}></lcm-setup-session>`;
      case 'addresses':
        return html`<lcm-setup-addresses .settings=${settings} @lcm-step-done=${done}></lcm-setup-addresses>`;
      case 'services':
        return html`<lcm-setup-services .settings=${settings} @lcm-step-done=${done}></lcm-setup-services>`;
      case 'access':
        return html`<lcm-setup-access .settings=${settings} @lcm-step-done=${done}></lcm-setup-access>`;
      case 'certificate':
        return html`<lcm-setup-certificate .settings=${settings} @lcm-step-done=${done}></lcm-setup-certificate>`;
      case 'onboarding':
        return html`<lcm-setup-onboarding .settings=${settings} @lcm-step-done=${done}></lcm-setup-onboarding>`;
      case 'mail':
        return html`<lcm-setup-mail .settings=${settings} @lcm-step-done=${done}></lcm-setup-mail>`;
      case 'retention':
        return html`<lcm-setup-retention .settings=${settings} @lcm-step-done=${done}></lcm-setup-retention>`;
      case 'backup':
        return html`<lcm-setup-backup .settings=${settings} @lcm-step-done=${done}></lcm-setup-backup>`;
      case 'subscription':
        return html`<lcm-setup-subscription .settings=${settings} @lcm-step-done=${done}></lcm-setup-subscription>`;
    }
  }

  #loading(): TemplateResult {
    if (this.#loaded.error) {
      return html`<lcm-notice tone="crit" heading=${t('setup.loadFailed')}>${this.#loaded.error}</lcm-notice>
        <div class="actions"><lcm-button variant="outline" @click=${() => this.#loaded.reload()}>${t('settings.form.retry')}</lcm-button></div>`;
    }
    return html`<lcm-skeleton shape="block" label=${t('settings.form.loading')}></lcm-skeleton>`;
  }

  #progress(step: WizardStep): TemplateResult {
    const number = stepNumber(step);
    return html`<div class="progress" data-field="progress">
      <p class="count">${t('setup.progress', { number, total: WIZARD_STEPS.length })}</p>
      <div class="bar" aria-hidden="true">${WIZARD_STEPS.map((_, index) => html`<span class=${index < number ? 'reached' : ''}></span>`)}</div>
    </div>`;
  }

  override render() {
    const step = setupStepOf(auth.user);
    if (!isWizardStep(step)) return nothing;
    const settings = this._settings;
    return html`<section>
      <div class="head">
        ${this.#progress(step)}
        <h1>${t(TEXT[step].title)}</h1>
        <p class="lead">${t(TEXT[step].lead)}</p>
      </div>
      ${settings ? this.#step(step, settings) : this.#loading()}
    </section>`;
  }
}
customElements.define('lcm-page-setup', SetupPage);
