import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-card';
import '../../components/lcm-choice';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import type { ChoiceOption } from '../../components/lcm-choice';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, i18n, languageName, t } from '../../lib/i18n';
import { getDensity, getTheme, onThemeChange, setDensity, setTheme, type Density, type Theme } from '../../lib/theme';
import { formStyles } from './form-styles';

/**
 * <lcm-account-preferences> – Sprache (am Konto gespeichert, gilt auf allen Geräten), Theme und Dichte (nur in
 * diesem Browser).
 */
export class LcmAccountPreferences extends LitElement {
  static override properties = { _error: { state: true } };
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: grid;
        gap: var(--gap);
      }
      .stack {
        max-inline-size: 40rem;
      }
    `,
  ];

  declare _error: string;

  constructor() {
    super();
    this._error = '';
    new I18nController(this);
    new SubscribeController(this, onThemeChange);
  }

  async #language(language: string): Promise<void> {
    this._error = '';
    try {
      await auth.changeLanguage(language);
      toast({ tone: 'ok', title: t('account.preferences.languageSaved') });
    } catch (error) {
      this._error = errorText(error);
    }
  }

  #themes(): ChoiceOption[] {
    return [
      { id: 'auto', label: t('theme.auto'), icon: 'auto', description: t('account.preferences.theme.autoText') },
      { id: 'light', label: t('theme.light'), icon: 'sun', description: t('account.preferences.theme.lightText') },
      { id: 'dark', label: t('theme.dark'), icon: 'moon', description: t('account.preferences.theme.darkText') },
    ];
  }

  #densities(): ChoiceOption[] {
    return [
      { id: 'comfortable', label: t('account.preferences.density.comfortable'), description: t('account.preferences.density.comfortableText') },
      { id: 'compact', label: t('account.preferences.density.compact'), description: t('account.preferences.density.compactText') },
    ];
  }

  override render() {
    return html`<lcm-card level="2" heading=${t('account.preferences.languageTitle')} subtitle=${t('account.preferences.languageSubtitle')}>
        <div class="stack">
          <lcm-select
            label=${t('account.preferences.language')}
            .options=${i18n.languages.map((language) => ({ value: language, label: languageName(language) }))}
            .value=${i18n.language}
            @lcm-change=${(event: CustomEvent<{ value: string }>) => void this.#language(event.detail.value)}
          ></lcm-select>
          ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
        </div>
      </lcm-card>
      <lcm-card level="2" heading=${t('account.preferences.lookTitle')} subtitle=${t('account.preferences.lookSubtitle')}>
        <div class="stack">
          <lcm-choice
            label=${t('account.preferences.theme.label')}
            .options=${this.#themes()}
            value=${getTheme()}
            @lcm-change=${(event: CustomEvent<{ value: Theme }>) => setTheme(event.detail.value)}
          ></lcm-choice>
          <lcm-choice
            label=${t('account.preferences.density.label')}
            .options=${this.#densities()}
            value=${getDensity()}
            @lcm-change=${(event: CustomEvent<{ value: Density }>) => setDensity(event.detail.value)}
          ></lcm-choice>
        </div>
      </lcm-card>`;
  }
}
customElements.define('lcm-account-preferences', LcmAccountPreferences);
