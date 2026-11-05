import { LitElement, html, type TemplateResult } from 'lit';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-list-pager';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import '../security/sec-cell';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { COMPOSE_URL, PAGE_SIZE, isIncomplete, normaliseCompose, type ComposeProject } from '../../api/containers';
import { opsApi } from '../../api/servers-ops';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { plainLabels, secStyles } from '../security/sec-styles';
import { queryFromUrl, queryPatch, slicePage } from './containers-state';
import { serverLinks } from './ctr-cells';
import './ctr-fleet-action';

/** <lcm-ctr-compose> – Compose-Projekte über alle Server. Nicht vollständig laufende Projekte tragen eine Warnung. */
export class CtrCompose extends LitElement {
  static override styles = [baseStyles, secStyles];

  readonly #projects = new LiveData<ComposeProject[]>(this, { url: () => COMPOSE_URL, transform: normaliseCompose });

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  get #columns(): Column<ComposeProject>[] {
    return [
      { key: 'project', label: t('containers.compose.column.project'), render: (r) => html`<lcm-sec-cell strong>${r.project}</lcm-sec-cell>` },
      { key: 'servers', label: t('containers.compose.column.servers'), render: (r) => serverLinks(r.servers) },
      { key: 'services', label: t('containers.compose.column.services'), render: (r) => (r.services.length > 0 ? r.services.join(', ') : '–') },
      { key: 'running', label: t('containers.compose.column.running'), render: (r) => this.#runningCell(r) },
      { key: 'dir', label: t('containers.compose.column.dir'), kind: 'mono', render: (r) => r.workingDir || '–' },
      ...(auth.can('servers:write') ? [{ key: 'action', label: t('serverOps.fleet.column'), render: (r: ComposeProject) => this.#update(r) }] : []),
    ];
  }

  #update(project: ComposeProject): TemplateResult {
    return html`<lcm-ctr-fleet-action
      icon="refresh"
      label=${t('serverOps.fleet.compose.label')}
      help=${t('serverOps.fleet.compose.help', { project: project.project })}
      meaning=${t('serverOps.fleet.compose.meaning')}
      heading=${t('serverOps.fleet.compose.heading', { project: project.project })}
      .servers=${project.servers}
      .run=${(serverId: number) => opsApi.composeUpdate(serverId, project.project)}
    ></lcm-ctr-fleet-action>`;
  }

  #runningCell(project: ComposeProject): TemplateResult {
    const label = t('containers.compose.runningValue', { running: project.running, total: project.containers });
    return html`<lcm-status level=${isIncomplete(project) ? 'warn' : 'ok'} label=${label}></lcm-status>`;
  }

  #renderList(projects: ComposeProject[]): TemplateResult {
    if (projects.length === 0) {
      return html`<lcm-empty-state level="3" icon="container" heading=${t('containers.compose.empty.title')} text=${t('containers.compose.empty.text')}></lcm-empty-state>`;
    }
    const slice = slicePage(projects, queryFromUrl(router.state.query).page);
    return html`<lcm-data-table external no-toolbar .columns=${this.#columns} .rows=${slice.items} .labels=${plainLabels(t('containers.compose.caption'), t('containers.compose.empty.title'))}></lcm-data-table>
      <lcm-list-pager
        page=${slice.page}
        page-size=${PAGE_SIZE}
        total=${slice.total}
        range-label=${t('containers.pager.range', { from: slice.from, to: slice.to, total: formatNumber(slice.total) })}
        previous-label=${t('containers.pager.previous')}
        next-label=${t('containers.pager.next')}
        @lcm-page=${(e: CustomEvent<{ page: number }>) => router.setQuery(queryPatch({ page: e.detail.page }))}
      ></lcm-list-pager>`;
  }

  override render() {
    const { data, error, loading } = this.#projects;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('containers.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="6" label=${t('containers.loading')}></lcm-skeleton>`;
    }
    return html`<div class=${`stack ${loading ? 'stale' : ''}`}>${this.#renderList(data)}</div>`;
  }
}
customElements.define('lcm-ctr-compose', CtrCompose);
