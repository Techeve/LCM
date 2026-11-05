import { LitElement, html, type TemplateResult } from 'lit';
import '../../components/lcm-empty-state';
import '../../components/lcm-skeleton';
import type { TableLabels } from '../../components/lcm-data-table';
import type { StatusLevel } from '../../components/shared';
import type { ServerItem } from '../../api/servers';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import type { LiveData } from '../../lib/live-data';

/** Grundlage der Reiter: der Server kommt von außen, Sprachwechsel rendert neu. */
export abstract class SrvTab extends LitElement {
  static override properties = {
    server: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
  };

  declare server: ServerItem;
  declare canWrite: boolean;

  constructor() {
    super();
    this.canWrite = false;
    new I18nController(this);
  }
}

/** Lade-, Fehler- und Datenzustand eines Reiters. */
export function whenLoaded<T>(live: LiveData<T>, render: (data: T) => TemplateResult, lines = 4): TemplateResult {
  const { data, error, loading } = live;
  if (data === undefined) {
    return error
      ? html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${error}></lcm-empty-state>`
      : html`<lcm-skeleton shape="row" lines=${lines} label=${t('servers.loading')}></lcm-skeleton>`;
  }
  return loading ? html`<div class="stale">${render(data)}</div>` : render(data);
}

/** Beschriftungen einer schlichten Tabelle ohne Auswahl und Spaltenwahl. */
export function plainLabels(caption: string, empty: string): TableLabels {
  return {
    caption,
    filter: '',
    all: '',
    columns: '',
    densityComfortable: '',
    densityCompact: '',
    selectAll: t('servers.packages.selectAll'),
    selectRow: t('servers.packages.selectRow'),
    selected: t('servers.packages.selected'),
    clear: t('servers.packages.clear'),
    empty,
    sortedAsc: t('servers.table.sortedAsc'),
    sortedDesc: t('servers.table.sortedDesc'),
  };
}

/** Schweregrad einer Meldung als Ampelstufe. */
export function severityLevel(severity: string): StatusLevel {
  if (severity === 'critical') return 'crit';
  if (severity === 'high' || severity === 'warning') return 'warn';
  if (severity === 'medium') return 'warn';
  return 'unknown';
}
