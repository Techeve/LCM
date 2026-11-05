import { html, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import type { GlobalSettings } from '../../api/settings';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { text, type FieldSpec } from './system/form-state';
import { SettingsForm } from './system/settings-form';

const FIELDS: FieldSpec[] = [text('ntp_server_presets', LIMITS.list, 'text'), text('default_timezone', LIMITS.name, 'timezone')];

/** Einstellungen → Zeit: Vorgaben für die Zeit-Aktionen auf den Servern. */
export class SettingsTime extends SettingsForm {
  protected readonly fields = FIELDS;

  protected renderForm(_settings: GlobalSettings): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.time.servers.title')} subtitle=${t('settings.time.servers.subtitle')}>
        ${this.area('ntp_server_presets', t('settings.time.servers.label'), {
          rows: 6,
          width: 'l',
          placeholder: 'NTP-Pool = 0.pool.ntp.org\nCloudflare = time.cloudflare.com',
          hint: t('settings.time.servers.hint'),
        })}
      </lcm-card>
      <lcm-card level="2" heading=${t('settings.time.zone.title')} subtitle=${t('settings.time.zone.subtitle')}>
        ${this.input('default_timezone', t('settings.time.zone.label'), { placeholder: 'Europe/Berlin', mono: true, hint: t('settings.time.zone.hint') })}
      </lcm-card>`;
  }
}
customElements.define('lcm-settings-time', SettingsTime);
