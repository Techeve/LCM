// Zellen der Serverliste. Die Tabelle rendert Zellen in ihrem eigenen Shadow DOM; damit Kennzeichen und Blasen ihre
// Gestaltung mitbringen, ist jede Zelle mit mehr als Text ein eigenes kleines Element.

import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-ampel';
import '../../components/lcm-badge';
import '../../components/lcm-status';
import { hostLabel, isOffline, isToleratedOutage } from '../../api/servers-list';
import type { ServerItem, ServerStatus } from '../../api/servers';
import { baseStyles, type StatusLevel } from '../../components/shared';
import { t } from '../../lib/i18n';
import { insightText } from '../../lib/insight-text';
import '../../components/lcm-hover-tip';

const STATUS_LEVEL: Record<ServerStatus, StatusLevel> = { excellent: 'great', green: 'ok', yellow: 'warn', red: 'crit' };

const cellStyles = css`
  :host {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-inline-size: 0;
    max-inline-size: 100%;
    vertical-align: middle;
  }
  .name {
    min-inline-size: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: var(--fw-semibold);
  }
  .host {
    min-inline-size: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .dimmed {
    color: var(--text-dim);
    font-weight: var(--fw-medium);
  }
  .tip-title {
    display: block;
    color: var(--text-1);
    font-weight: var(--fw-semibold);
  }
  .tip-text {
    margin: var(--space-1) 0 0;
    color: var(--text-2);
  }
  .tip-list {
    margin: var(--space-2) 0 0;
    padding-inline-start: var(--space-4);
    color: var(--text-2);
  }
  .tip-list li + li {
    margin-block-start: var(--space-1);
  }
  .tip-list li.critical::marker {
    color: var(--crit);
  }
  .tip-list li.warning::marker {
    color: var(--warn);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    overflow-wrap: anywhere;
  }
`;

/** <lcm-srv-status .server=${…} compact> – Ampel; die Gründe für Gelb oder Rot stehen in der Blase. */
export class SrvStatusCell extends LitElement {
  static override properties = { server: { attribute: false }, compact: { type: Boolean } };
  static override styles = [baseStyles, cellStyles];

  declare server: ServerItem | undefined;
  declare compact: boolean;

  constructor() {
    super();
    this.server = undefined;
    this.compact = false;
  }

  override render() {
    const server = this.server;
    if (!server) return nothing;
    const label = t(`servers.status.${server.status}`);
    // Schmal bleibt nur die Ampel; ihr Text steht dann als Beschriftung für Screenreader da.
    const light = this.compact
      ? html`<lcm-ampel slot="trigger" level=${STATUS_LEVEL[server.status]} size="m" label=${label}></lcm-ampel>`
      : html`<lcm-status slot="trigger" level=${STATUS_LEVEL[server.status]} label=${label}></lcm-status>`;
    if (server.insights.length === 0) return light;
    return html`<lcm-hover-tip label=${label}>
      ${light}
      <span class="tip-title">${t('servers.insightsTitle', { status: label })}</span>
      <ul class="tip-list">
        ${server.insights.map((insight) => html`<li class=${insight.severity}>${insightText(insight)}</li>`)}
      </ul>
    </lcm-hover-tip>`;
  }
}
customElements.define('lcm-srv-status', SrvStatusCell);

/**
 * <lcm-srv-name .server=${…} .consoles=${['admin']}> – Name mit kleinen Kennzeichen (Konsole offen, Wartung, offline).
 * Ein tolerierter Ausfall dämpft den Namen. Wartung steht vor offline: Sie erklärt, warum der Server nicht antwortet.
 */
export class SrvNameCell extends LitElement {
  static override properties = { server: { attribute: false }, consoles: { attribute: false } };
  static override styles = [
    baseStyles,
    cellStyles,
    css`
      /* Lange Abzeichen rutschen in der schmalen Liste unter den Namen, statt die Spalte „Updates“ hinauszuschieben. */
      :host {
        flex-wrap: wrap;
      }
    `,
  ];

  declare server: ServerItem | undefined;
  declare consoles: string[];

  constructor() {
    super();
    this.server = undefined;
    this.consoles = [];
  }

  #offline(server: ServerItem): TemplateResult | typeof nothing {
    if (!isOffline(server)) return nothing;
    const tolerated = isToleratedOutage(server);
    return html`<lcm-hover-tip icon="power" tone=${tolerated ? 'muted' : 'crit'} label=${t('servers.offline')}>
      <span class="tip-title">${t('servers.offline')}</span>
      <p class="tip-text">${tolerated ? t('servers.offlineTolerated') : t('servers.offlineTip', { count: server.failed_checks })}</p>
      ${server.last_error ? html`<p class="tip-text mono">${server.last_error}</p>` : nothing}
    </lcm-hover-tip>`;
  }

  override render() {
    const server = this.server;
    if (!server) return nothing;
    return html`<span class=${isToleratedOutage(server) ? 'name dimmed' : 'name'}>${server.name}</span>
      ${server.incapable ? html`<lcm-badge tone="crit">${t('servers.incapable.badge')}</lcm-badge>` : nothing}
      ${this.consoles.length > 0
        ? html`<lcm-hover-tip icon="terminal" tone="info" label=${t('servers.console')}>
            <span class="tip-title">${t('servers.console')}</span>
            <p class="tip-text">${t('servers.consoleTip', { who: this.consoles.join(', ') })}</p>
          </lcm-hover-tip>`
        : nothing}
      ${server.maintenance
        ? html`<lcm-hover-tip icon="repair" tone="warn" label=${t('servers.maintenance')}>
            <span class="tip-title">${t('servers.maintenance')}</span>
            <p class="tip-text">${t('servers.maintenanceTip')}</p>
          </lcm-hover-tip>`
        : nothing}
      ${this.#offline(server)}`;
  }
}
customElements.define('lcm-srv-name', SrvNameCell);

/** <lcm-srv-host .server=${…}> – `host:port`; Agent-Server mit Kennzeichen statt Port. */
export class SrvHostCell extends LitElement {
  static override properties = { server: { attribute: false } };
  static override styles = [baseStyles, cellStyles];

  declare server: ServerItem | undefined;

  constructor() {
    super();
    this.server = undefined;
  }

  override render() {
    const server = this.server;
    if (!server) return nothing;
    if (server.transport !== 'agent') return html`<span class="host">${hostLabel(server)}</span>`;
    return html`<span class="host">${server.host}</span>
      <lcm-hover-tip icon="bolt" tone=${server.agent_connected ? 'info' : 'muted'} label=${t('servers.agent')}>
        <span class="tip-title">${t('servers.agent')}</span>
        <p class="tip-text">${t(server.agent_connected ? 'servers.agentOn' : 'servers.agentOff')}</p>
      </lcm-hover-tip>`;
  }
}
customElements.define('lcm-srv-host', SrvHostCell);
