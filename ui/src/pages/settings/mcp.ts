import { html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-card';
import '../../components/lcm-explainer';
import '../../components/lcm-status';
import { settingsApi, type GlobalSettings } from '../../api/settings';
import { t } from '../../lib/i18n';
import './access/key-list';
import { clientConfig, curlExample, mcpNeedsDomain, mcpUrl } from './access/mcp-model';
import './system/copy-field';
import { flag, type Draft, type FieldSpec } from './system/form-state';
import { SettingsForm } from './system/settings-form';

const FIELDS: FieldSpec[] = [flag('mcp_enabled')];

/** Einstellungen → MCP: Schnittstelle für KI-Assistenten an- und abschalten, wirksame Adresse, eigene Schlüssel, Anbindungsbeispiel. */
export class SettingsMcp extends SettingsForm {
  protected readonly fields = FIELDS;

  protected override request(_body: Record<string, unknown>, draft: Draft): Promise<void> {
    return settingsApi.setMcp({ enabled: draft.mcp_enabled === true });
  }

  /** Ohne Domain (Modus C) ist MCP aus: Der Schalter ist gesperrt und verweist auf die Adressen. */
  #noDomain(): TemplateResult {
    return html`<p class="muted" data-field="no-domain">${t('settings.mcp.interface.noDomain')} <a href="/settings/general">${t('settings.mcp.interface.noDomainLink')}</a></p>`;
  }

  #interface(settings: GlobalSettings): TemplateResult {
    const running = settings.mcp_enabled === true;
    const needsDomain = mcpNeedsDomain(settings);
    const address = String(settings['effective_urls.mcp'] ?? '');
    return html`<lcm-card level="2" heading=${t('settings.mcp.interface.title')} subtitle=${t('settings.mcp.interface.subtitle')}>
      <lcm-status slot="actions" level=${running ? 'great' : 'unknown'} label=${running ? t('settings.mcp.interface.on') : t('settings.mcp.interface.off')}></lcm-status>
      <div class="fields">
        ${this.flag('mcp_enabled', t('settings.service.mcp.label'), { on: t('settings.service.mcp.on'), off: t('settings.service.mcp.off'), disabled: needsDomain })}
        ${needsDomain ? this.#noDomain() : nothing}
        ${address ? html`<lcm-copy-field label=${t('settings.mcp.interface.address')} value=${address}></lcm-copy-field>` : nothing}
      </div>
    </lcm-card>`;
  }

  #example(settings: GlobalSettings): TemplateResult {
    const url = mcpUrl(String(settings['effective_urls.mcp'] ?? ''), String(settings.mcp_port ?? ''));
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

  protected renderForm(settings: GlobalSettings): TemplateResult {
    return html`${this.#interface(settings)}
      <lcm-settings-key-list mode="mcp"></lcm-settings-key-list>
      ${this.#example(settings)}`;
  }
}
customElements.define('lcm-settings-mcp', SettingsMcp);
