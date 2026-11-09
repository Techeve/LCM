import { html, nothing, type TemplateResult } from 'lit';
import type { ServerRef } from '../../api/containers';
import '../security/sec-cell';
import './ctr-servers-more';
import { t } from '../../lib/i18n';

const SHOWN_SERVERS = 3;

/** Die ersten Server als Links, der Rest als Knopf, der alle aufklappt; die Gesamtzahl nennt die Zelle immer. */
export function serverLinks(servers: ServerRef[], total: number = servers.length): TemplateResult | string {
  if (servers.length === 0) return '';
  const shown = servers.slice(0, SHOWN_SERVERS);
  const rest = total - shown.length;
  return html`${shown.map((server, index) => html`${index > 0 ? ', ' : ''}<lcm-sec-cell href=${`/servers/${server.id}`}>${server.name}</lcm-sec-cell>`)}${rest > 0 ? html` <lcm-ctr-servers-more label=${t('containers.more', { count: rest })} heading=${t('containers.allServers', { count: total })} .servers=${servers}></lcm-ctr-servers-more>` : nothing}`;
}
