import { html, type TemplateResult } from 'lit';
import { t } from '../../../lib/i18n';
import type { Draft } from '../system/form-state';
import { PORT_KEY, SERVICES, SHARED_PORT_KEY, readMode } from './address-model';

/** Die Ports aus dem Entwurf, wie der Hilfetext sie nennt. */
function portsOf(draft: Draft): Record<string, string> {
  const ports: Record<string, string> = { shared: String(draft[SHARED_PORT_KEY] ?? '').trim() || '443' };
  for (const service of SERVICES) ports[service] = String(draft[PORT_KEY[service]] ?? '').trim() || '?';
  return ports;
}

/**
 * Hilfetext zu den öffentlichen Adressen. Er folgt dem gewählten Modus und nennt die Ports des Entwurfs:
 * wann der Modus passt, was ein Reverse Proxy weiterreichen muss, was die Firewall freigeben muss und wann
 * Let's Encrypt in LCM nötig ist. Der Aufrufer setzt ihn in eine `lcm-explainer`.
 */
export function addressHelp(draft: Draft): TemplateResult {
  const mode = readMode(draft.address_mode);
  const ports = portsOf(draft);
  const key = (part: string) => `settings.general.address.help.${mode}.${part}`;
  return html`
    <p><strong>${t('settings.general.address.help.fits')}</strong> ${t(key('fits'))}</p>
    <p><strong>${t('settings.general.address.help.proxy')}</strong> ${t(key('proxy'), ports)}</p>
    <p><strong>${t('settings.general.address.help.firewall')}</strong> ${t(key('firewall'), ports)}</p>
    <p><strong>${t('settings.general.address.help.acme')}</strong> ${t(key('acme'))}</p>
  `;
}
