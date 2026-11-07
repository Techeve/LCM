import { html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-status';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { serverSummaryUrl } from '../../api/servers';
import { composeGroups, normaliseOpsDocker, normaliseServerFlags, opsApi, opsDockerUrl, supersededImageIds, vulnsByImage, type ComposeGroup, type OpsContainer, type OpsDocker, type OpsServerFlags } from '../../api/servers-ops';
import { stateLevel } from '../../api/containers';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { stateText } from '../containers/containers-state';
import '../security/sec-cell';
import './ops-cve-switch';
import './ops-docker-images';
import './set-action';
import './srv-action';
import { opsStyles } from './ops-styles';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, whenLoaded } from './srv-tab';

/**
 * Container: Container nach Compose-Projekt und die Images dieses Servers. Aktualisieren (Pull, Compose-Update),
 * Aufräumen und die CVE-Relevanz je Container laufen von hier; alles mit Job läuft über `lcm-srv-action`.
 */
export class SrvContainers extends SrvTab {
  static override styles = [baseStyles, tabStyles, opsStyles];

  readonly #docker = new LiveData<OpsDocker>(this, { url: () => opsDockerUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseOpsDocker });
  readonly #flags = new LiveData<OpsServerFlags>(this, { url: () => serverSummaryUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseServerFlags });

  get #updatesOff(): boolean {
    return this.#flags.data?.dockerUpdatesDisabled ?? false;
  }

  // ---- Aktionen ----

  #renderToolbar(data: OpsDocker): TemplateResult {
    const s = this.server;
    const off = this.#updatesOff;
    return html`<div class="toolbar">
      ${!data.hasCompose ? html`<lcm-badge>${t('serverOps.docker.noCompose')}</lcm-badge>` : nothing}
      ${off ? html`<lcm-badge tone="warn">${t('serverOps.docker.updatesOff')}</lcm-badge>` : nothing}
      <span class="grow"></span>
      ${this.canWrite
        ? html`<lcm-set-action
              variant="primary"
              icon="download"
              server-id=${s.id}
              server-name=${s.name}
              action="docker-pull-all"
              label=${t('serverOps.docker.pullAll.label')}
              help=${off ? t('serverOps.docker.updatesOffHelp') : t('serverOps.docker.pullAll.help')}
              ?disabled=${off}
              heading=${t('serverOps.docker.pullAll.heading', { name: s.name })}
              .run=${() => opsApi.dockerPullAll(s.id)}
            ></lcm-set-action>
            <lcm-srv-action
              variant="outline"
              icon="refresh"
              server-id=${s.id}
              server-name=${s.name}
              label=${t('serverOps.docker.refresh.label')}
              help=${t('serverOps.docker.refresh.help')}
              no-confirm
              .run=${() => opsApi.dockerRefresh(s.id)}
            ></lcm-srv-action>`
        : nothing}
    </div>`;
  }

  #composeAction(project: string, service = ''): TemplateResult | typeof nothing {
    const data = this.#docker.data;
    if (!this.canWrite || !data?.hasCompose) return nothing;
    const s = this.server;
    const target = service ? `${project}/${service}` : project;
    return html`<lcm-set-action
      variant=${service ? 'outline' : 'primary'}
      icon="refresh"
      server-id=${s.id}
      server-name=${s.name}
      action="docker-compose-update"
      .params=${{ project, service }}
      label=${service ? t('serverOps.docker.serviceUpdate.label') : t('serverOps.docker.projectUpdate.label')}
      help=${this.#updatesOff ? t('serverOps.docker.updatesOffHelp') : service ? t('serverOps.docker.serviceUpdate.help', { target }) : t('serverOps.docker.projectUpdate.help', { project })}
      ?disabled=${this.#updatesOff}
      heading=${t('serverOps.docker.composeHeading', { target, name: s.name })}
      .run=${() => opsApi.composeUpdate(s.id, project, service)}
    ></lcm-set-action>`;
  }

  // ---- Container ----

  #columns(group: ComposeGroup, data: OpsDocker): Column<OpsContainer>[] {
    const vulns = vulnsByImage(data.images);
    const superseded = supersededImageIds(data.images);
    const relevant = new Set(this.#flags.data?.cveRelevant ?? []);
    const withProject = group.project !== '';
    return [
      { key: 'state', label: t('serverOps.docker.column.state'), render: (r) => html`<lcm-status level=${stateLevel(r.state)} label=${stateText(r.state)}></lcm-status>${r.status ? html`<lcm-sec-cell block muted>${r.status}</lcm-sec-cell>` : nothing}` },
      { key: 'name', label: t('serverOps.docker.column.container'), sortable: true, render: (r) => html`<strong>${r.name}</strong>${r.service ? html`<lcm-sec-cell block muted>${t('serverOps.docker.service', { service: r.service })}</lcm-sec-cell>` : nothing}` },
      { key: 'image', label: t('serverOps.docker.column.image'), render: (r) => this.#imageCell(r, vulns.get(r.imageId || r.image), superseded.has(r.imageId)) },
      { key: 'ports', label: t('serverOps.docker.column.ports'), kind: 'mono', render: (r) => r.ports || '–' },
      {
        key: 'relevant',
        label: t('serverOps.docker.column.relevant'),
        render: (r) => html`<lcm-ops-cve-switch server-id=${this.server.id} name=${r.name} ?checked=${relevant.has(r.name.toLowerCase())} ?readonly=${!this.canWrite}></lcm-ops-cve-switch>`,
      },
      ...(withProject && this.canWrite && data.hasCompose ? [{ key: 'action', label: t('serverOps.docker.column.action'), render: (r: OpsContainer) => (r.service ? this.#composeAction(group.project, r.service) : '') }] : []),
    ];
  }

  #imageCell(container: OpsContainer, vulns: { critical: number; high: number } | undefined, stale: boolean): TemplateResult {
    return html`<lcm-sec-cell mono>${container.image}</lcm-sec-cell>${stale
      ? html` <lcm-badge tone="warn">${t('serverOps.docker.restartPending')}</lcm-badge>`
      : nothing}${vulns
      ? html` <lcm-badge tone=${vulns.critical > 0 ? 'crit' : 'warn'}>${vulns.critical > 0 ? t('serverOps.docker.critical', { count: vulns.critical }) : t('serverOps.docker.high', { count: vulns.high })}</lcm-badge>`
      : nothing}`;
  }

  #renderGroup(group: ComposeGroup, data: OpsDocker): TemplateResult {
    const heading = group.project ? t('serverOps.docker.project', { project: group.project }) : t('serverOps.docker.standalone');
    const subtitle = group.project ? '' : t('serverOps.docker.standaloneHint');
    return html`<lcm-card heading=${heading} subtitle=${subtitle}>
      ${group.project ? html`<div slot="actions">${this.#composeAction(group.project)}</div>` : nothing}
      <lcm-data-table no-toolbar density="compact" .columns=${this.#columns(group, data)} .rows=${group.containers} .labels=${plainLabels(heading, t('serverOps.docker.noContainers'))}></lcm-data-table>
    </lcm-card>`;
  }

  #render(data: OpsDocker): TemplateResult {
    if (data.containers.length === 0 && data.images.length === 0) {
      return html`${this.#renderToolbar(data)}<lcm-empty-state level="3" icon="container" heading=${t('serverOps.docker.empty.title')} text=${t('serverOps.docker.empty.text')}></lcm-empty-state>`;
    }
    return html`${this.#renderToolbar(data)}
      <div class="stack">
        ${data.containers.length === 0 ? html`<lcm-empty-state level="3" icon="container" heading=${t('serverOps.docker.noContainers')} text=${t('serverOps.docker.noContainersText')}></lcm-empty-state>` : composeGroups(data.containers).map((group) => this.#renderGroup(group, data))}
        <lcm-ops-docker-images .server=${this.server} .images=${data.images} ?can-write=${this.canWrite} ?has-docker=${data.hasDocker} ?updates-off=${this.#updatesOff}></lcm-ops-docker-images>
      </div>`;
  }

  override render() {
    return whenLoaded(this.#docker, (data) => this.#render(data));
  }
}
customElements.define('lcm-srv-containers', SrvContainers);
