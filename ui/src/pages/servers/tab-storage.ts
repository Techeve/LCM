import { html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-empty-state';
import '../../components/lcm-meter';
import '../../components/lcm-status';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles } from '../../components/shared';
import {
  DEFAULT_WARN_PERCENT,
  monitorMode,
  normaliseOpsStorage,
  normaliseStorageHistory,
  opsVolumesUrl,
  storageHistoryUrl,
  type OpsPool,
  type OpsStorage,
  type OpsVolume,
  type StorageHistoryData,
} from '../../api/servers-ops';
import { SubscribeController } from '../../lib/controllers';
import { formatBytes, formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import '../security/sec-cell';
import './ops-storage-chart';
import './ops-volume-monitor';
import { opsStyles } from './ops-styles';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, whenLoaded } from './srv-tab';

const MB = 1024 * 1024;
const HEALTHY = new Set(['online', 'healthy', 'ok', 'optimal']);
const POOL_KINDS: Record<string, string> = {
  zfs: 'serverOps.storage.kind.zfs',
  btrfs: 'serverOps.storage.kind.btrfs',
  mdraid: 'serverOps.storage.kind.mdraid',
  lvm_thin: 'serverOps.storage.kind.lvmThin',
  dsm_pool: 'serverOps.storage.kind.dsmPool',
  dsm_volume: 'serverOps.storage.kind.dsmVolume',
  dsm_disk: 'serverOps.storage.kind.dsmDisk',
};

function poolLevel(state: string): 'ok' | 'warn' | 'crit' | 'unknown' {
  if (state === 'faulted') return 'crit';
  if (state === 'degraded') return 'warn';
  return HEALTHY.has(state) ? 'ok' : 'unknown';
}

/**
 * Speicher: Verlauf der Belegung von „/“ (Tagesdurchschnitte mit Prognose), alle Laufwerke mit Überwachung je
 * Laufwerk (eigene Warnschwelle, `?volume=<mount>` öffnet das Fenster) und der Zustand der Speicherverbünde.
 */
export class SrvStorage extends SrvTab {
  static override styles = [baseStyles, tabStyles, opsStyles];

  readonly #storage = new LiveData<OpsStorage>(this, { url: () => opsVolumesUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseOpsStorage });
  readonly #history = new LiveData<StorageHistoryData>(this, { url: () => storageHistoryUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseStorageHistory });

  constructor() {
    super();
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  // ---- Verlauf ----

  #forecastBadge(data: StorageHistoryData): TemplateResult | typeof nothing {
    const forecast = data.forecast;
    if (!forecast) return nothing;
    if (forecast.insufficient) return html`<lcm-badge>${t('serverOps.storage.forecast.insufficient')}</lcm-badge>`;
    if (forecast.unlimited) return html`<lcm-badge tone="ok">${t('serverOps.storage.forecast.unlimited')}</lcm-badge>`;
    const tone = forecast.days <= 30 ? 'crit' : forecast.days <= 90 ? 'warn' : 'info';
    return html`<lcm-badge tone=${tone}>${t('serverOps.storage.forecast.days', { count: forecast.days })}</lcm-badge>`;
  }

  #summary(data: StorageHistoryData): string {
    const peak = Math.max(...data.points.map((p) => p.percent));
    const parts = [t('serverOps.storage.captured', { count: data.points.length, peak: formatNumber(peak, { maximumFractionDigits: 0 }) })];
    const forecast = data.forecast;
    if (forecast && !forecast.insufficient) parts.push(forecast.unlimited ? t('serverOps.storage.trend.flat') : t('serverOps.storage.trend.linear', { count: forecast.days }));
    return parts.join(' · ');
  }

  #renderHistory(): TemplateResult | typeof nothing {
    const data = this.#history.data;
    if (!data) return nothing;
    const subtitle = data.currentTotalMb > 0 ? t('serverOps.storage.current', { percent: data.currentPercent, used: formatBytes(data.currentUsedMb * MB), total: formatBytes(data.currentTotalMb * MB) }) : '';
    return html`<lcm-card heading=${t('serverOps.storage.historyTitle')} subtitle=${subtitle}>
      <div slot="actions">${this.#forecastBadge(data)}</div>
      ${data.points.length === 0
        ? html`<p class="muted">${t('serverOps.storage.historyEmpty')}</p>`
        : html`<lcm-ops-storage-chart .points=${data.points} warn=${DEFAULT_WARN_PERCENT}></lcm-ops-storage-chart>
            <p class="muted">${this.#summary(data)}</p>`}
    </lcm-card>`;
  }

  // ---- Laufwerke ----

  #mountCell(v: OpsVolume): TemplateResult {
    return html`<lcm-sec-cell mono strong>${v.mountpoint}</lcm-sec-cell>
      ${v.mountpoint === '/' ? html`<lcm-badge>${t('serverOps.storage.root')}</lcm-badge>` : nothing}
      ${v.readOnly ? html`<lcm-badge tone="crit">${t('servers.storageTab.readOnly')}</lcm-badge>` : nothing}
      <lcm-sec-cell block muted mono>${v.device}</lcm-sec-cell>`;
  }

  #monitorCell(v: OpsVolume): TemplateResult | string {
    const mode = monitorMode(v);
    if (mode === 'root') return html`<lcm-sec-cell muted>${t('serverOps.storage.monitor.always')}</lcm-sec-cell>`;
    if (mode === 'network') return html`<lcm-sec-cell muted>${t('serverOps.storage.monitor.notPossible')}</lcm-sec-cell>`;
    const text = mode === 'monitored' ? html`<lcm-badge tone="info">${t('serverOps.storage.monitor.from', { percent: v.monitor?.warn || DEFAULT_WARN_PERCENT })}</lcm-badge>` : html`<lcm-sec-cell muted>${t('serverOps.storage.monitor.offShort')}</lcm-sec-cell>`;
    if (!this.canWrite) return text;
    return html`${text} <lcm-button size="small" variant="ghost" icon=${mode === 'monitored' ? 'edit' : 'bell'} @click=${() => router.setQuery({ volume: v.mountpoint })}>${mode === 'monitored' ? t('serverOps.storage.monitor.change') : t('serverOps.storage.monitor.enable')}</lcm-button>`;
  }

  get #volumeColumns(): Column<OpsVolume>[] {
    return [
      { key: 'mountpoint', label: t('servers.storageTab.column.mount'), sortable: true, render: (r) => this.#mountCell(r) },
      { key: 'fstype', label: t('servers.storageTab.column.type'), render: (r) => html`<lcm-sec-cell mono>${r.fstype}</lcm-sec-cell>${r.isNetwork ? html` <lcm-badge>${t('serverOps.storage.network')}</lcm-badge>` : nothing}` },
      { key: 'usage', label: t('servers.storageTab.column.usage'), render: (r) => html`<lcm-meter value=${r.percent} label=${t('servers.storage.used', { percent: r.percent })}></lcm-meter><lcm-sec-cell block muted>${formatBytes(r.usedMb * MB)} / ${formatBytes(r.totalMb * MB)}</lcm-sec-cell>` },
      { key: 'inodes', label: t('servers.storageTab.column.inodes'), align: 'end', render: (r) => (r.inodesTotal > 0 ? `${r.inodePercent} %` : '–') },
      { key: 'monitor', label: t('serverOps.storage.column.monitor'), render: (r) => this.#monitorCell(r) },
    ];
  }

  get #poolColumns(): Column<OpsPool>[] {
    return [
      { key: 'state', label: t('servers.storageTab.column.state'), render: (r) => html`<lcm-status level=${poolLevel(r.state)} label=${r.rawState || r.state || '–'}></lcm-status>` },
      { key: 'name', label: t('servers.storageTab.column.pool'), render: (r) => html`<lcm-sec-cell mono strong>${r.name}</lcm-sec-cell>` },
      { key: 'kind', label: t('servers.storageTab.column.kind'), render: (r) => (POOL_KINDS[r.kind] ? t(POOL_KINDS[r.kind]!) : r.kind) },
      { key: 'message', label: t('servers.storageTab.column.message'), render: (r) => this.#poolMessage(r) },
    ];
  }

  #poolMessage(pool: OpsPool): TemplateResult | string {
    const fill = [pool.percent > 0 ? t('serverOps.storage.fillData', { percent: pool.percent }) : '', pool.metaPercent > 0 ? t('serverOps.storage.fillMeta', { percent: pool.metaPercent }) : ''].filter(Boolean).join(', ');
    if (!pool.message && !fill) return '–';
    return html`${pool.message}${fill ? html`<lcm-sec-cell block muted>${fill}</lcm-sec-cell>` : nothing}`;
  }

  #renderVolumes(data: OpsStorage): TemplateResult {
    const open = router.state.query.get('volume');
    const volume = data.volumes.find((v) => v.mountpoint === open && v.monitorable) ?? null;
    return html`<lcm-card heading=${t('servers.storageTab.volumes')}>
        ${data.volumes.length === 0
          ? html`<p class="muted">${t('serverOps.storage.volumesEmpty')}</p>`
          : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#volumeColumns} .rows=${data.volumes} .labels=${plainLabels(t('servers.storageTab.volumes'), t('serverOps.storage.volumesEmpty'))}></lcm-data-table>`}
      </lcm-card>
      ${this.canWrite ? html`<lcm-ops-volume-monitor server-id=${this.server.id} .volume=${volume} @lcm-close=${() => router.setQuery({ volume: null }, { replace: true })}></lcm-ops-volume-monitor>` : nothing}`;
  }

  #render(data: OpsStorage): TemplateResult {
    const history = this.#history.data;
    if (data.volumes.length === 0 && data.pools.length === 0 && (!history || history.points.length === 0)) {
      return html`<lcm-empty-state level="3" icon="disk" heading=${t('servers.storageTab.empty.title')} text=${t('servers.storageTab.empty.text')}></lcm-empty-state>`;
    }
    return html`<div class="stack">
      ${this.#renderHistory()} ${this.#renderVolumes(data)}
      ${data.pools.length > 0
        ? html`<lcm-card heading=${t('servers.storageTab.pools')}>
            <lcm-data-table no-toolbar density="compact" .columns=${this.#poolColumns} .rows=${data.pools} .labels=${plainLabels(t('servers.storageTab.pools'), t('servers.storageTab.empty.title'))}></lcm-data-table>
          </lcm-card>`
        : nothing}
    </div>`;
  }

  override render() {
    return whenLoaded(this.#storage, (data) => this.#render(data));
  }
}
customElements.define('lcm-srv-storage', SrvStorage);
