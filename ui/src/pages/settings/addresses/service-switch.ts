import { html, nothing, type TemplateResult } from 'lit';
import '../../../components/lcm-notice';
import '../../../components/lcm-switch';
import type { GlobalSettings } from '../../../api/settings';
import { t } from '../../../lib/i18n';
import type { Draft } from '../system/form-state';
import { ENABLED_KEY, switchState, type SwitchedService } from './address-model';

export interface ServiceSwitchOptions {
  service: SwitchedService;
  draft: Draft;
  settings: GlobalSettings;
  /** Der Schalter wurde umgelegt; die Seite schreibt den Wert in ihren Entwurf. */
  onChange: (key: string, checked: boolean) => void;
  /** Hinweis, wenn in Modus C der Name des Dienstes fehlt; je nach Ort mit oder ohne Verweis auf die Einstellungen. */
  noNameHint: TemplateResult | string;
}

/** Wie viele Server den Agenten nutzen, wenn er gespeichert an ist und im Entwurf aus: Sie gehen beim Speichern offline. */
export function agentServersLosing(draft: Draft, settings: GlobalSettings): number {
  const count = Number(settings.agent_servers ?? 0);
  const wasOn = settings[ENABLED_KEY.agent] !== false;
  return wasOn && !switchState(draft, settings, 'agent').checked ? count : 0;
}

/**
 * Der Schalter an/aus für Agent oder MCP: derselbe Entwurfswert (`agent_enabled`, `mcp_enabled`) an jeder Stelle der Seite.
 * Ohne Namen in Modus C steht er aus und gesperrt, mit Hinweis darunter; legt die `config.json` ihn fest, steht dort der Hinweis darauf.
 * Schaltet jemand den Agenten aus, den Server nutzen, warnt eine Meldung vor dem Speichern.
 */
export function serviceSwitch(options: ServiceSwitchOptions): TemplateResult {
  const { service, draft, settings, onChange, noNameHint } = options;
  const { checked, lock } = switchState(draft, settings, service);
  const key = ENABLED_KEY[service];
  const hint = lock === 'noName' ? noNameHint : lock === 'config' ? t('settings.general.address.locked') : '';
  return html`<div data-switch=${service}>
    <lcm-switch
      label=${t(`settings.service.${service}.label`)}
      on-label=${t('settings.form.on')}
      off-label=${t('settings.form.off')}
      on-text=${t(`settings.service.${service}.on`)}
      off-text=${t(`settings.service.${service}.off`)}
      ?disabled=${lock !== undefined}
      .checked=${checked}
      @lcm-change=${(event: CustomEvent<{ checked: boolean }>) => onChange(key, event.detail.checked)}
    ></lcm-switch>
    ${hint ? html`<div class="muted" data-switch-hint=${service}>${hint}</div>` : nothing}
    ${service === 'agent' ? agentWarning(agentServersLosing(draft, settings)) : nothing}
  </div>`;
}

function agentWarning(count: number): TemplateResult | typeof nothing {
  return count > 0 ? html`<lcm-notice tone="warn" data-field="agent-offline">${t('settings.service.agent.offline', { count })}</lcm-notice>` : nothing;
}
