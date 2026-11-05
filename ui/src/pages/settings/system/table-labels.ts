import type { TableLabels } from '../../../components/lcm-data-table';
import { t } from '../../../lib/i18n';

/** Beschriftungen einer schlichten Tabelle ohne Auswahl, Filter und Spaltenwahl. */
export function tableLabels(caption: string, empty: string): TableLabels {
  return {
    caption,
    filter: '',
    all: '',
    columns: '',
    densityComfortable: '',
    densityCompact: '',
    selectAll: '',
    selectRow: '',
    selected: '',
    clear: '',
    empty,
    sortedAsc: t('settings.form.sortedAsc'),
    sortedDesc: t('settings.form.sortedDesc'),
  };
}
