import { LitElement, html } from 'lit';
import '../../components/lcm-explainer';
import { baseStyles } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import './access/key-list';
import { settingsStyles } from './system/settings-styles';

/** Kopfzeile, mit der ein Programm den Schlüssel mitschickt (siehe Middleware `auth.go`). */
const KEY_HEADER = 'X-API-Key: …';

/** Einstellungen → API-Schlüssel: Schlüssel für Skripte und andere Dienste (REST), einmalige Anzeige, Widerruf. */
export class SettingsApiKeys extends LitElement {
  static override styles = [baseStyles, settingsStyles];

  constructor() {
    super();
    new I18nController(this);
  }

  override render() {
    return html`<lcm-settings-key-list mode="rest" aside>
      <lcm-explainer slot="aside" topic="settings-apikeys" heading=${t('settings.apikeys.explainer.title')} collapse-label=${t('settings.form.collapse')} expand-label=${t('settings.form.expand')}>
        <p>${t('settings.apikeys.explainer.what')}</p>
        <ul>
          <li><strong>${t('settings.apikeys.scope.read')}:</strong> ${t('settings.apikeys.explainer.read')}</li>
          <li><strong>${t('settings.apikeys.scope.readwrite')}:</strong> ${t('settings.apikeys.explainer.readwrite')}</li>
        </ul>
        <p>${t('settings.apikeys.explainer.rights')}</p>
        <p>${t('settings.apikeys.explainer.header')} <code>${KEY_HEADER}</code></p>
      </lcm-explainer>
    </lcm-settings-key-list>`;
  }
}
customElements.define('lcm-settings-apikeys', SettingsApiKeys);
