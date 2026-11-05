import { html, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import '../../components/lcm-explainer';
import '../../components/lcm-status';
import { settingsApi, type GlobalSettings } from '../../api/settings';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import './access/key-list';
import { clientConfig, curlExample, isLoopbackHost, mcpUrl } from './access/mcp-model';
import './system/copy-field';
import { flag, int, text, type Draft, type FieldErrors, type FieldSpec } from './system/form-state';
import { SettingsForm } from './system/settings-form';

const FIELDS: FieldSpec[] = [flag('mcp_enabled'), text('mcp_bind_host', LIMITS.host, 'host'), int('mcp_port', 1, 65535)];

/** Einstellungen → MCP: Schnittstelle für KI-Assistenten an- und abschalten, eigene Schlüssel, Anbindungsbeispiel. */
export class SettingsMcp extends SettingsForm {
  protected readonly fields = FIELDS;

  protected override request(_body: Record<string, unknown>, draft: Draft): Promise<void> {
    return settingsApi.setMcp({ enabled: draft.mcp_enabled === true, bind_host: String(draft.mcp_bind_host).trim(), port: Number(draft.mcp_port) });
  }

  protected override validate(draft: Draft): FieldErrors {
    const host = String(draft.mcp_bind_host).trim();
    return host === '' || isLoopbackHost(host) ? {} : { mcp_bind_host: 'settings.mcp.hostLocal' };
  }

  #interface(settings: GlobalSettings, draft: Draft): TemplateResult {
    const running = settings.mcp_enabled === true;
    return html`<lcm-card level="2" heading=${t('settings.mcp.interface.title')} subtitle=${t('settings.mcp.interface.subtitle')}>
      <lcm-status slot="actions" level=${running ? 'great' : 'unknown'} label=${running ? t('settings.mcp.interface.on') : t('settings.mcp.interface.off')}></lcm-status>
      <div class="fields">
        ${this.flag('mcp_enabled', t('settings.mcp.interface.enable'), { on: t('settings.mcp.interface.enableOn'), off: t('settings.mcp.interface.enableOff') })}
        <div class="field-row">
          ${this.input('mcp_bind_host', t('settings.mcp.interface.host'), { mono: true, placeholder: '127.0.0.1', hint: t('settings.mcp.interface.hostHint'), disabled: draft.mcp_enabled !== true })}
          ${this.input('mcp_port', t('settings.mcp.interface.port'), { placeholder: '9330', width: 'xs', disabled: draft.mcp_enabled !== true })}
        </div>
      </div>
    </lcm-card>`;
  }

  #example(settings: GlobalSettings): TemplateResult {
    const url = mcpUrl(String(settings.mcp_bind_host ?? ''), String(settings.mcp_port ?? ''));
    const placeholder = t('settings.mcp.example.keyPlaceholder');
    return html`<lcm-card level="2" heading=${t('settings.mcp.example.title')} subtitle=${t('settings.mcp.example.subtitle')}>
      <div class="fields">
        <lcm-copy-field label=${t('settings.mcp.example.url')} value=${url}></lcm-copy-field>
        <lcm-copy-field block label=${t('settings.mcp.example.config')} value=${clientConfig(url, placeholder)}></lcm-copy-field>
        <lcm-copy-field block label=${t('settings.mcp.example.curl')} value=${curlExample(url, placeholder)}></lcm-copy-field>
      </div>
    </lcm-card>`;
  }

  protected override explainer(): TemplateResult {
    return html`<lcm-explainer topic="settings-mcp" heading=${t('settings.mcp.explainer.title')} collapse-label=${t('settings.form.collapse')} expand-label=${t('settings.form.expand')}>
      <p>${t('settings.mcp.explainer.what')}</p>
      <p>${t('settings.mcp.explainer.local')}</p>
      <p>${t('settings.mcp.explainer.keys')}</p>
    </lcm-explainer>`;
  }

  protected renderForm(settings: GlobalSettings, draft: Draft): TemplateResult {
    return html`${this.#interface(settings, draft)}
      <lcm-settings-key-list mode="mcp"></lcm-settings-key-list>
      ${this.#example(settings)}`;
  }
}
customElements.define('lcm-settings-mcp', SettingsMcp);
