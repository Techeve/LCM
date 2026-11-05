import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { appsUrl, normaliseApps, type AppItem, type AppsData } from '../../api/server-data';
import { detailApi, normaliseAppActions, type AppActions } from '../../api/server-detail';
import { normaliseUnknownApps, type UnknownApp } from '../../api/servers-packages';
import { auth } from '../../lib/auth';
import './srv-action';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, whenLoaded } from './srv-tab';

/**
 * Apps: erkannte Anwendungen mit ihrer Version und der neuesten bekannten; sichern und aktualisieren, wo LCM es kann.
 * Darunter die laufenden Dienste, deren Unit-Datei keinem Paket gehört (an der Paketverwaltung vorbei installiert).
 */
export class SrvApps extends SrvTab {
  static override styles = [baseStyles, tabStyles, css`p { margin: var(--space-3) 0 0; }`];

  readonly #apps = new LiveData<AppsData>(this, { url: () => appsUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseApps });
  readonly #actions = new LiveData<Map<string, AppActions>>(this, { url: () => appsUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseAppActions });
  readonly #unknown = new LiveData<UnknownApp[]>(this, { url: () => appsUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseUnknownApps });

  #action(app: AppItem, kind: 'backup' | 'update'): TemplateResult {
    const s = this.server;
    const slug = this.#actions.data?.get(app.id)?.slug ?? '';
    const backup = kind === 'backup';
    return html`<lcm-srv-action
      variant=${!backup && app.updateAvailable ? 'primary' : 'outline'}
      icon=${backup ? 'download' : 'refresh'}
      server-id=${s.id}
      server-name=${s.name}
      label=${backup ? t('serverDetail.apps.backup.label') : t('serverDetail.apps.update.label')}
      help=${backup ? t('serverDetail.apps.backup.help', { name: app.name }) : t('serverDetail.apps.update.help', { name: app.name })}
      confirm-heading=${backup ? t('serverDetail.apps.backup.heading', { name: app.name }) : t('serverDetail.apps.update.heading', { name: app.name })}
      .run=${() => detailApi.runAppAction(s.id, slug, kind)}
    ></lcm-srv-action>`;
  }

  #renderActions(app: AppItem): TemplateResult | string {
    const actions = this.#actions.data?.get(app.id);
    if (!actions || (!actions.can_backup && !actions.can_update)) return '–';
    return html`<span class="actions">${actions.can_backup ? this.#action(app, 'backup') : ''}${actions.can_update ? this.#action(app, 'update') : ''}</span>`;
  }

  get #columns(): Column<AppItem>[] {
    return [
      { key: 'name', label: t('servers.apps.column.name'), sortable: true, render: (r) => html`<strong>${r.name}</strong>` },
      { key: 'version', label: t('servers.apps.column.version'), kind: 'mono' },
      { key: 'latest', label: t('servers.apps.column.latest'), kind: 'mono', render: (r) => (r.updateAvailable ? html`<lcm-badge tone="warn">${r.latest}</lcm-badge>` : r.latest || '–') },
      { key: 'path', label: t('servers.apps.column.path'), kind: 'mono' },
      ...(this.canWrite ? [{ key: 'actions', label: t('serverDetail.apps.actions'), render: (r: AppItem) => this.#renderActions(r) }] : []),
    ];
  }

  get #unknownColumns(): Column<UnknownApp>[] {
    return [
      { key: 'unit', label: t('serverPackages.apps.unknown.column.unit'), kind: 'mono', sortable: true },
      { key: 'execPath', label: t('serverPackages.apps.unknown.column.exec'), kind: 'mono', render: (r) => r.execPath || '–' },
      { key: 'fragmentPath', label: t('serverPackages.apps.unknown.column.file'), render: (r) => html`<span class="mono muted">${r.fragmentPath}</span>` },
    ];
  }

  #renderDetected(data: AppsData): TemplateResult {
    if (data.detected.length === 0 && (this.#unknown.data?.length ?? 0) > 0) {
      return html`<lcm-card heading=${t('servers.apps.empty.title')}><p class="muted">${t('servers.apps.empty.text')}</p></lcm-card>`;
    }
    if (data.detected.length === 0) {
      return html`<lcm-empty-state level="3" icon="settings" heading=${t('servers.apps.empty.title')} text=${t('servers.apps.empty.text')}></lcm-empty-state>`;
    }
    return html`<lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${data.detected} .labels=${plainLabels(t('servers.apps.caption'), t('servers.apps.empty.title'))}></lcm-data-table>`;
  }

  #renderUnknown(): TemplateResult | typeof nothing {
    const unknown = this.#unknown.data;
    if (!unknown) return nothing;
    return html`<lcm-card heading=${t('serverPackages.apps.unknown.title')} subtitle=${t('serverPackages.apps.unknown.subtitle')}>
      ${auth.can('settings:manage') ? html`<lcm-button slot="actions" size="small" variant="ghost" icon="arrow-right" href="/settings/apps">${t('serverPackages.apps.unknown.catalog')}</lcm-button>` : nothing}
      ${unknown.length === 0
        ? html`<p class="muted">${t('serverPackages.apps.unknown.empty')}</p>`
        : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#unknownColumns} .rows=${unknown} .labels=${plainLabels(t('serverPackages.apps.unknown.title'), t('serverPackages.apps.unknown.empty'))}></lcm-data-table>`}
    </lcm-card>`;
  }

  #render(data: AppsData): TemplateResult {
    return html`<div class="stack">${this.#renderDetected(data)} ${this.#renderUnknown()}</div>`;
  }

  override render() {
    return whenLoaded(this.#apps, (data) => this.#render(data));
  }
}
customElements.define('lcm-srv-apps', SrvApps);
