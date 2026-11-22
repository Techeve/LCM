import { css, html, type PropertyValues, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-choice';
import '../../components/lcm-notice';
import '../../components/lcm-textarea';
import '../settings/endpoints/acme-card';
import type { ChoiceOption } from '../../components/lcm-choice';
import type { IconName } from '../../components/icons';
import type { AcmeState } from '../../api/endpoints';
import { t } from '../../lib/i18n';
import { addressHelp } from '../settings/addresses/address-help';
import { certificateNames } from '../settings/addresses/address-model';
import { coversNames } from '../settings/endpoints/acme-model';
import { CERTIFICATE_CHOICES, PROXY_KEY, certificateBody, certificateChoice, certificateRedirect, proxyEntries, proxyProblem, proxyProblemText, type CertificateChoice, type ProxyProblem } from './certificate-model';
import { setupDraft } from './setup-model';
import { SetupStep } from './setup-step';

/** So lange steht der Hinweis vor dem Wechsel der Adresse, wenn der Benutzer nicht vorher klickt. */
export const REDIRECT_DELAY_MS = 6000;

const ICON: Record<CertificateChoice, IconName> = { letsencrypt: 'lock', proxy: 'firewall' };

/**
 * Schritt „Zertifikat“: LCM hat für eigene Namen ohne Hilfe kein gültiges Zertifikat. Zur Wahl stehen Let's Encrypt
 * (darunter die Karte der Einstellungen, geöffnet, mit dem Knopf „Zertifikat anfordern“; der Knopf des Schritts bleibt gesperrt, bis ein gültiges Zertifikat alle Namen trägt, und heißt dann „Weiter“) und ein Reverse-Proxy davor
 * (die Adressen, denen LCM die Client-Adresse glaubt, mit eingeschaltetem `trust_proxy_header`). Eine dritte Wahl
 * gibt es nicht; ohne beides liefert LCM sein eigenes Zertifikat. Ohne Namen in Schritt 2 zeigt Let's Encrypt nur einen
 * Hinweis mit dem Weg zurück (vorgewählt ist dann der Proxy). Trägt die Oberfläche danach einen eingetragenen Namen, der
 * von der aktuellen Adresse abweicht, kündigt der Schritt den Wechsel an und leitet den Browser dorthin.
 */
export class SetupCertificate extends SetupStep {
  static override properties = { ...SetupStep.properties, _acme: { state: true }, _choice: { state: true }, _proxies: { state: true }, _problem: { state: true }, _redirect: { state: true } };
  static override styles = [
    ...SetupStep.styles,
    css`
      .block {
        display: grid;
        gap: var(--space-4);
      }
      p {
        margin: 0;
      }
    `,
  ];

  readonly step = 'certificate' as const;
  protected override readonly warnUnsaved = true;
  declare _choice: CertificateChoice | undefined;
  declare _proxies: string;
  declare _problem: ProxyProblem | null;
  /** Die Adresse, zu der der Browser gleich wechselt. */
  declare _redirect: string;
  /** Der Stand von Let's Encrypt, wie die Karte darunter ihn meldet. */
  declare _acme: AcmeState | undefined;
  #timer = 0;

  constructor() {
    super();
    this._proxies = '';
    this._problem = null;
    this._redirect = '';
    this._acme = undefined;
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    window.clearTimeout(this.#timer);
  }

  override willUpdate(changed: PropertyValues): void {
    super.willUpdate(changed);
    if (this._choice) return;
    this._choice = this.#names.length === 0 ? 'proxy' : certificateChoice(this.settings);
    this._proxies = String(this.settings[PROXY_KEY] ?? '');
  }

  /** Die Namen der gespeicherten Adressen; ohne sie gibt es kein Zertifikat von Let's Encrypt. */
  get #names(): string[] {
    return certificateNames(setupDraft(this.settings), this.settings).saved;
  }

  get #letsEncryptChosen(): boolean {
    return (this._choice ?? 'letsencrypt') === 'letsencrypt';
  }

  /** Mit Let's Encrypt geht es erst weiter, wenn ein gültiges Zertifikat alle Namen trägt. */
  get #certificateReady(): boolean {
    return this._acme !== undefined && coversNames(this._acme, this.#names);
  }

  /** Dieselbe Hilfe wie beim Schritt der Adressen: Proxy, Firewall und Let's Encrypt je nach Modus. */
  protected override help(): TemplateResult {
    return addressHelp(setupDraft(this.settings));
  }

  protected override saveLabel(): string {
    return this.#letsEncryptChosen ? t('setup.next') : t('setup.save');
  }

  protected override saveDisabled(): boolean {
    return this.#letsEncryptChosen && (this.#names.length === 0 || !this.#certificateReady);
  }

  protected override async save(): Promise<void> {
    const choice = this._choice ?? 'letsencrypt';
    const entries = choice === 'proxy' ? proxyEntries(this._proxies) : [];
    this._problem = choice === 'proxy' ? proxyProblem(entries) : null;
    if (this._problem) return;
    const updated = await this.patch(certificateBody(choice, entries));
    const target = certificateRedirect(updated, window.location.href);
    if (target) this.#announce(target);
    else this.done(updated);
  }

  #announce(target: string): void {
    this._redirect = target;
    this.#timer = window.setTimeout(() => this.#go(), REDIRECT_DELAY_MS);
  }

  #go(): void {
    window.clearTimeout(this.#timer);
    window.location.assign(this._redirect);
  }

  get #options(): ChoiceOption[] {
    return CERTIFICATE_CHOICES.map((choice) => ({
      id: choice,
      label: t(choice === 'proxy' ? 'setup.certificate.proxy.title' : 'setup.certificate.letsencrypt.title'),
      description: t(choice === 'proxy' ? 'setup.certificate.proxy.text' : 'setup.certificate.letsencrypt.text'),
      icon: ICON[choice],
    }));
  }

  #proxyError(): string {
    if (!this._problem) return '';
    const { key, list } = proxyProblemText(this._problem);
    return t(key, { list });
  }

  /** Ohne Namen gibt es für Let's Encrypt nichts auszufüllen: Der Hinweis führt zurück zu Schritt 2. */
  #needsName(): TemplateResult {
    return html`<div class="block" data-field="letsencrypt-needs-name">
      <lcm-notice tone="info">${t('setup.certificate.letsencrypt.needsName')}</lcm-notice>
      <div class="actions">
        <lcm-button variant="outline" icon="chevron-left" data-action="to-addresses" ?disabled=${this._busy} @click=${() => void this.goTo('addresses')}>${t('setup.certificate.letsencrypt.toAddresses')}</lcm-button>
      </div>
    </div>`;
  }

  #letsEncrypt(): TemplateResult {
    if (this.#names.length === 0) return this.#needsName();
    const { saved, pending } = certificateNames(setupDraft(this.settings), this.settings);
    return html`<div class="block" data-field="letsencrypt">
      <lcm-settings-acme
        open
        wizard
        .names=${saved}
        ?names-pending=${pending}
        @lcm-acme-state=${(e: CustomEvent<{ state: AcmeState }>) => (this._acme = e.detail.state)}
      ></lcm-settings-acme>
    </div>`;
  }

  #proxy(): TemplateResult {
    return html`<div class="block" data-field="proxy">
      <lcm-textarea
        data-field="trusted-proxies"
        label=${t('setup.certificate.proxy.label')}
        width="m"
        mono
        rows="4"
        placeholder=${'10.0.0.1\n192.168.1.0/24'}
        hint=${t('setup.certificate.proxy.hint')}
        error=${this.#proxyError()}
        .value=${this._proxies}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => ((this._proxies = e.detail.value), (this._problem = null))}
      ></lcm-textarea>
    </div>`;
  }

  protected override body(): TemplateResult {
    return html`<div class="block">
      <lcm-choice
        data-field="certificate-choice"
        label=${t('setup.certificate.choice')}
        variant="cards"
        .options=${this.#options}
        .value=${this._choice ?? 'letsencrypt'}
        @lcm-change=${(e: CustomEvent<{ value: CertificateChoice }>) => ((this._choice = e.detail.value), (this._problem = null))}
      ></lcm-choice>
      ${this._choice === 'proxy' ? this.#proxy() : this.#letsEncrypt()}
      <p class="muted">${t('setup.certificate.fallback')}</p>
    </div>`;
  }

  #redirecting(): TemplateResult {
    const address = new URL(this._redirect).origin;
    return html`<div class="block" data-step="certificate">
      <lcm-notice tone="info" heading=${t('setup.certificate.redirect.title')} data-field="redirect">${t('setup.certificate.redirect.text', { address })}</lcm-notice>
      <div class="actions"><lcm-button variant="primary" icon="arrow-right" data-action="redirect" @click=${() => this.#go()}>${t('setup.certificate.redirect.now')}</lcm-button></div>
    </div>`;
  }

  override render(): TemplateResult {
    return this._redirect ? this.#redirecting() : super.render();
  }
}
customElements.define('lcm-setup-certificate', SetupCertificate);
