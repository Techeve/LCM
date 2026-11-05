import { html, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import type { GlobalSettings } from '../../api/settings';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { text, type FieldSpec } from './system/form-state';
import { SettingsForm } from './system/settings-form';

const FIELDS: FieldSpec[] = [text('dns_server_presets', LIMITS.list, 'text'), text('dns_test_domains', LIMITS.list, 'domains')];

/** Einstellungen → DNS: Vorgabe-Nameserver und Test-Domains. */
export class SettingsDns extends SettingsForm {
  protected readonly fields = FIELDS;

  protected renderForm(_settings: GlobalSettings): TemplateResult {
    return html`<lcm-card level="2" heading=${t('settings.dns.servers.title')} subtitle=${t('settings.dns.servers.subtitle')}>
        ${this.area('dns_server_presets', t('settings.dns.servers.label'), {
          rows: 6,
          width: 'l',
          placeholder: 'Cloudflare = 1.1.1.1\nGoogle = 8.8.8.8',
          hint: t('settings.dns.servers.hint'),
        })}
      </lcm-card>
      <lcm-card level="2" heading=${t('settings.dns.domains.title')} subtitle=${t('settings.dns.domains.subtitle')}>
        ${this.area('dns_test_domains', t('settings.dns.domains.label'), {
          rows: 5,
          width: 'l',
          placeholder: 'deb.debian.org\ngithub.com',
          hint: t('settings.dns.domains.hint'),
        })}
      </lcm-card>`;
  }
}
customElements.define('lcm-settings-dns', SettingsDns);
