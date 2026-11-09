import { html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-data-table';
import '../../components/lcm-list-pager';
import type { Column, SortState, TableLabels } from '../../components/lcm-data-table';
import { t } from '../../lib/i18n';
import { router } from '../../lib/router';
import { PAGE_SIZE, pageQuery, pageSlice, sortQuery, sortRows, type TableView } from './lu-paging';

/** Beschriftungen einer schlichten Tabelle ohne Auswahl und Spaltenwahl. */
export function plainLabels(caption: string, empty: string): TableLabels {
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
    sortedAsc: t('linuxUsers.table.sortedAsc'),
    sortedDesc: t('linuxUsers.table.sortedDesc'),
  };
}

/** Die Tabelle führt Zeilen und Auswahl über Text-IDs. */
function withStringId<T extends { id: string | number }>(item: T) {
  return { ...item, id: String(item.id) };
}

export interface PagedTable<T extends { id: string | number }> {
  items: T[];
  columns: Column<T>[];
  labels: TableLabels;
  view: TableView;
  activeId: string;
  onActivate: (id: number) => void;
}

/** Kompakte Tabelle mit Sortierung und Seiten aus der Adresse; die Blätterleiste erscheint erst bei mehr als einer Seite. */
export function pagedTable<T extends { id: string | number }>(table: PagedTable<T>): TemplateResult {
  const slice = pageSlice(sortRows(table.items, table.columns, table.view.sort), table.view.page);
  return html`<lcm-data-table
      no-toolbar
      external
      density="compact"
      active-id=${table.activeId}
      .columns=${table.columns}
      .rows=${slice.rows.map(withStringId)}
      .sort=${table.view.sort}
      .labels=${table.labels}
      @lcm-sort=${(e: CustomEvent<SortState | null>) => router.setQuery(sortQuery(e.detail), { replace: true })}
      @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => table.onActivate(Number(e.detail.id))}
    ></lcm-data-table>
    ${slice.total > PAGE_SIZE
      ? html`<lcm-list-pager
          page=${slice.page}
          page-size=${PAGE_SIZE}
          total=${slice.total}
          range-label=${t('linuxUsers.range', { from: slice.from, to: slice.to, total: slice.total })}
          previous-label=${t('linuxUsers.previous')}
          next-label=${t('linuxUsers.next')}
          @lcm-page=${(e: CustomEvent<{ page: number }>) => router.setQuery(pageQuery(e.detail.page))}
        ></lcm-list-pager>`
      : nothing}`;
}
