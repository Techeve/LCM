import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-choice';
import type { ChoiceOption } from '../../components/lcm-choice';
import { baseStyles } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';

/**
 * <lcm-session-mode-field value="memory" cookie-available> – die Wahl, wie der Browser die Anmeldung trägt
 * (Arbeitsspeicher oder Sitzungs-Cookie), mit der Erklärung zu jeder Möglichkeit. Gemeinsame Auswahl der Karte
 * unter Einstellungen → Allgemein und der einmaligen Frage nach den Pflichtschritten. Ohne HTTPS ist das Cookie
 * gesperrt, und der Grund steht sichtbar daneben. `lcm-change` { value } kommt von der Auswahl darin.
 */
export class SessionModeField extends LitElement {
  static override properties = {
    value: { type: String },
    cookieAvailable: { type: Boolean, attribute: 'cookie-available' },
    disabled: { type: Boolean },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        gap: var(--space-3);
      }
      p {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare value: string;
  declare cookieAvailable: boolean;
  declare disabled: boolean;

  constructor() {
    super();
    this.value = 'memory';
    this.cookieAvailable = false;
    this.disabled = false;
    new I18nController(this);
  }

  get #options(): ChoiceOption[] {
    return [
      { id: 'memory', label: t('settings.session.memory.title'), description: t('settings.session.memory.text') },
      { id: 'cookie', label: t('settings.session.cookie.title'), description: t('settings.session.cookie.text'), disabled: !this.cookieAvailable },
    ];
  }

  override render() {
    return html`<lcm-choice
        stacked
        label=${t('settings.session.legend')}
        ?disabled=${this.disabled}
        .options=${this.#options}
        .value=${this.value}
      ></lcm-choice>
      ${this.cookieAvailable ? nothing : html`<p>${t('settings.session.needsHttps')}</p>`}
      <p>${t('settings.session.switchHint')}</p>`;
  }
}
customElements.define('lcm-session-mode-field', SessionModeField);
