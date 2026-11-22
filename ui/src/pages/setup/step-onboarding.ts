import { css, html, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import '../../components/lcm-input';
import '../../components/lcm-notice';
import '../settings/system/copy-field';
import { t } from '../../lib/i18n';
import { onboardingGuide, onboardingGuideStyles } from '../settings/onboarding-guide';
import { onboardingUser } from '../settings/onboarding-model';
import type { Draft, FieldErrors } from '../settings/system/form-state';
import { ONBOARDING_FIELDS, onboardingBody, onboardingDraft, onboardingErrors } from './setup-model';
import { SetupStep } from './setup-step';

/**
 * Schritt „Onboarding“: der Vorgabe-Benutzer für neue Server und der Onboarding-Schlüssel mit Kopierknopf, dazu wo er
 * beim Anbieter hinterlegt wird. Die Anleitung für den Weg über `authorized_keys` ist die der Einstellungen, hier
 * eingeklappt eingebettet: Die Einstellungen selbst sind im Assistenten noch gesperrt. Lässt sich überspringen.
 */
export class SetupOnboarding extends SetupStep {
  static override properties = { ...SetupStep.properties, _draft: { state: true }, _errors: { state: true } };
  static override styles = [
    ...SetupStep.styles,
    onboardingGuideStyles,
    css`
      .block,
      .providers {
        display: grid;
        gap: var(--space-3);
      }
      .providers {
        margin: 0;
        padding-inline-start: var(--space-5);
      }
      p {
        margin: 0;
      }
    `,
  ];

  readonly step = 'onboarding' as const;
  declare _draft: Draft | undefined;
  declare _errors: FieldErrors;
  #base: Draft | undefined;

  constructor() {
    super();
    this._errors = {};
  }

  override willUpdate(): void {
    if (this._draft) return;
    this.#base = onboardingDraft(this.settings);
    this._draft = { ...this.#base };
  }

  protected override async save(): Promise<void> {
    const draft = this._draft;
    if (!draft || !this.#base) return;
    const invalid = onboardingErrors(draft);
    if (Object.keys(invalid).length > 0) {
      this._errors = this.errorTexts(draft, invalid, ONBOARDING_FIELDS);
      this._error = t('settings.form.fixErrors');
      return;
    }
    await this.advance(onboardingBody(draft, this.#base));
  }

  #key(key: string, user: string): TemplateResult {
    return key
      ? html`<lcm-copy-field block data-field="onboarding-key" label=${t('settings.general.key.title')} value=${key}></lcm-copy-field>
          <lcm-card level="3" variant="inset" collapsible data-card="guide" heading=${t('setup.onboarding.guide')}>${onboardingGuide(key, user)}</lcm-card>`
      : html`<lcm-notice>${t('settings.general.key.none')}</lcm-notice>`;
  }

  protected override body(): TemplateResult {
    const draft = this._draft ?? {};
    const user = onboardingUser(draft.default_ssh_user);
    return html`<div class="block">
      <lcm-input
        data-field="default_ssh_user"
        label=${t('settings.general.ssh.user')}
        width="m"
        placeholder=${'root'}
        hint=${t('setup.onboarding.userHint')}
        error=${this._errors.default_ssh_user ?? ''}
        autocomplete="off"
        mono
        .value=${String(draft.default_ssh_user ?? '')}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => ((this._draft = { ...draft, default_ssh_user: e.detail.value }), (this._errors = {}))}
      ></lcm-input>
      ${this.#key(String(this.settings.onboarding_pub_key ?? ''), user)}
      <h3>${t('setup.onboarding.where')}</h3>
      <ul class="providers">
        <li>${t('setup.onboarding.hetzner')}</li>
        <li>${t('setup.onboarding.proxmox')}</li>
        <li>${t('setup.onboarding.other')}</li>
      </ul>
    </div>`;
  }
}
customElements.define('lcm-setup-onboarding', SetupOnboarding);
