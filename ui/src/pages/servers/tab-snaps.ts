import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import { serversApi, type PreviewAction } from '../../api/servers';
import { normaliseSnaps, snapsUrl, type SnapItem } from '../../api/server-data';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import './srv-action';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, whenLoaded } from './srv-tab';

/** Snaps: installierte Snaps mit verfügbaren Aktualisierungen; alle oder die ausgewählten aktualisieren, Ausgewählte entfernen. */
export class SrvSnaps extends SrvTab {
  static override properties = { ...SrvTab.properties, _selected: { state: true } };
  static override styles = [baseStyles, tabStyles, css`.bulk-actions { display: contents; }`];

  declare _selected: string[];

  readonly #snaps = new LiveData<SnapItem[]>(this, { url: () => snapsUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseSnaps });

  constructor() {
    super();
    this._selected = [];
  }

  get #columns(): Column<SnapItem>[] {
    return [
      { key: 'name', label: t('servers.snaps.column.name'), sortable: true, locked: true, render: (r) => html`<strong>${r.name}</strong>` },
      { key: 'version', label: t('servers.snaps.column.version'), kind: 'mono', sortable: true },
      { key: 'channel', label: t('servers.snaps.column.channel'), kind: 'mono' },
      { key: 'publisher', label: t('servers.snaps.column.publisher') },
      { key: 'candidate', label: t('servers.snaps.column.available'), kind: 'mono', render: (r) => (r.candidate ? html`<lcm-badge tone="warn">${r.candidate}</lcm-badge>` : '–') },
    ];
  }

  #action(key: string, action: PreviewAction, run: () => ReturnType<typeof serversApi.snapsRefreshAll>, icon: 'refresh' | 'trash', names: string[] = [], danger = false): TemplateResult {
    return html`<lcm-srv-action
      variant=${danger ? 'danger' : 'outline'}
      icon=${icon}
      ?danger=${danger}
      server-id=${this.server.id}
      server-name=${this.server.name}
      action=${action}
      .params=${names.length ? { package: names } : {}}
      label=${t(`servers.snaps.${key}.label`)}
      help=${t(`servers.snaps.${key}.help`)}
      meaning=${t(`servers.snaps.${key}.meaning`)}
      .run=${run}
    ></lcm-srv-action>`;
  }

  #render(snaps: SnapItem[]): TemplateResult {
    if (snaps.length === 0) return html`<lcm-empty-state level="3" icon="package" heading=${t('servers.snaps.empty.title')} text=${t('servers.snaps.empty.text')}></lcm-empty-state>`;
    const id = this.server.id;
    const names = this._selected;
    return html`<div class="stack">
      <lcm-data-table density="compact" no-chooser ?selectable=${this.canWrite} .columns=${this.#columns} .rows=${snaps} .labels=${plainLabels(t('servers.snaps.caption'), t('servers.snaps.empty.title'))} .selected=${names} @lcm-selection-change=${(e: CustomEvent<{ selected: string[] }>) => (this._selected = e.detail.selected)}>
        ${this.canWrite ? html`<span slot="toolbar">${this.#action('refreshAll', 'snaps-refresh-all', () => serversApi.snapsRefreshAll(id), 'refresh')}</span>` : nothing}
        ${this.canWrite && names.length > 0
          ? html`<span slot="bulk" class="bulk-actions">
              ${this.#action('refreshSelected', 'snaps-refresh', () => serversApi.snapsRefresh(id, names), 'refresh', names)}
              ${this.#action('removeSelected', 'snaps-remove', () => serversApi.snapsRemove(id, names), 'trash', names, true)}
            </span>`
          : nothing}
      </lcm-data-table>
    </div>`;
  }

  override render() {
    return whenLoaded(this.#snaps, (snaps) => this.#render(snaps));
  }
}
customElements.define('lcm-srv-snaps', SrvSnaps);
