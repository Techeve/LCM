import { LitElement, html, nothing, type TemplateResult } from 'lit';
import { repeat } from 'lit/directives/repeat.js';
import './lcm-os.js';
import './lcm-status.js';
import { FreshKeys } from '../lib/motion.js';
import { dataTableStyles } from './data-table-styles.js';
import { placeFloating } from './floating.js';
import { icon } from './icons.js';
import type { Option } from './lcm-select.js';
import { baseStyles, emit, uid, type StatusLevel } from './shared.js';
import { LIMITS } from '../lib/validate.js';

/** Zeile der Tabelle selbst: jedes Feld nachschlagbar, daher ohne feste Form. */
export type Row = { id: string } & Record<string, unknown>;

/**
 * Spalte für Zeilen vom Typ `T`. Jede Schnittstelle mit `id` passt, so braucht die Spalte
 * keinen Umweg über `Row`, um an die Felder zu kommen.
 */
export interface Column<T extends { id: string | number } = Row> {
  key: string;
  label: string;
  /** Wert für Sortieren und Filtern; Standard: row[key]. */
  value?: (row: T) => string | number;
  /** Eigener Sortierwert (z. B. Rang des Status); Standard: `value`. */
  sortValue?: (row: T) => string | number;
  /** Eigene Zelle; Standard je `kind`. */
  render?: (row: T) => TemplateResult | string | typeof nothing;
  kind?: 'text' | 'mono' | 'status' | 'os';
  sortable?: boolean;
  filter?: 'text' | 'select';
  options?: Option[];
  align?: 'start' | 'end';
  hidden?: boolean;
  locked?: boolean;
  /** Überschrift nur für Screenreader, wenn sich die Spalte durch ihr Symbol erklärt (Ampel, OS-Logo). */
  labelHidden?: boolean;
}

export interface TableLabels {
  caption: string;
  filter: string;
  all: string;
  columns: string;
  densityComfortable: string;
  densityCompact: string;
  selectAll: string;
  selectRow: string;
  selected: string;
  clear: string;
  empty: string;
  sortedAsc: string;
  sortedDesc: string;
}

export interface SortState {
  key: string;
  dir: 'asc' | 'desc';
}

const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });

/**
 * <lcm-data-table .columns=${…} .rows=${…} selectable> – Datentabelle: Sortieren, Filterzeile, Spaltenwahl,
 * Auswahl mit Sammelleiste (Slot `bulk`), Tastaturnavigation (Pfeile, Pos1/Ende, Enter, Leertaste), zwei Dichten.
 * Mit `external` sortiert und filtert der Aufrufer (Backend); die Tabelle meldet nur.
 * Ereignisse: `lcm-sort`, `lcm-filter`, `lcm-selection-change`, `lcm-row-activate`, `lcm-columns-change`, `lcm-density-change`.
 */
export class LcmDataTable extends LitElement {
  static override properties = {
    columns: { attribute: false },
    rows: { attribute: false },
    labels: { attribute: false },
    selectable: { type: Boolean },
    external: { type: Boolean },
    density: { type: String, reflect: true },
    sort: { attribute: false },
    filters: { attribute: false },
    selected: { attribute: false },
    activeId: { type: String, attribute: 'active-id' },
    hiddenColumns: { attribute: false },
    densityToggle: { type: Boolean, attribute: 'density-toggle' },
    noToolbar: { type: Boolean, attribute: 'no-toolbar' },
    noChooser: { type: Boolean, attribute: 'no-chooser' },
    _focusId: { state: true },
  };
  static override styles = [baseStyles, dataTableStyles];

  declare columns: Column[];
  declare rows: Row[];
  declare labels: TableLabels;
  declare selectable: boolean;
  declare external: boolean;
  declare density: 'comfortable' | 'compact' | '';
  declare sort: SortState | null;
  declare filters: Record<string, string>;
  declare selected: string[];
  declare activeId: string;
  declare hiddenColumns: string[] | null;
  declare densityToggle: boolean;
  declare noToolbar: boolean;
  declare noChooser: boolean;
  declare _focusId: string;
  private readonly chooserId = uid('chooser');
  /** Zeilen, die eine Live-Aktualisierung neu gebracht hat, blenden kurz ein. */
  private readonly fresh = new FreshKeys<Row>((row) => String(row.id));

  constructor() {
    super();
    this.columns = [];
    this.rows = [];
    this.labels = {
      caption: '',
      filter: '',
      all: '',
      columns: '',
      densityComfortable: '',
      densityCompact: '',
      selectAll: '',
      selectRow: '',
      selected: '',
      clear: '',
      empty: '',
      sortedAsc: '',
      sortedDesc: '',
    };
    this.selectable = false;
    this.external = false;
    this.density = '';
    this.sort = null;
    this.filters = {};
    this.selected = [];
    this.activeId = '';
    this.hiddenColumns = null;
    this.densityToggle = false;
    this.noToolbar = false;
    this.noChooser = false;
    this._focusId = '';
  }

  private get visibleColumns(): Column[] {
    const hidden = new Set(this.hiddenColumns ?? this.columns.filter((c) => c.hidden).map((c) => c.key));
    return this.columns.filter((c) => !hidden.has(c.key));
  }

  private cellValue(column: Column, row: Row): string | number {
    return column.value ? column.value(row) : (row[column.key] as string | number);
  }

  private get viewRows(): Row[] {
    if (this.external) return this.rows;
    let rows = this.rows;
    for (const column of this.columns) {
      const needle = (this.filters[column.key] ?? '').trim().toLowerCase();
      if (!needle) continue;
      rows = rows.filter((row) => {
        const text = String(this.cellValue(column, row)).toLowerCase();
        return column.filter === 'select' ? text === needle : text.includes(needle);
      });
    }
    const sort = this.sort;
    const column = sort ? this.columns.find((c) => c.key === sort.key) : undefined;
    if (!sort || !column) return rows;
    const factor = sort.dir === 'asc' ? 1 : -1;
    return [...rows].sort((a, b) => {
      const pick = column.sortValue ?? ((row) => this.cellValue(column, row));
      const x = pick(a);
      const y = pick(b);
      return factor * (typeof x === 'number' && typeof y === 'number' ? x - y : collator.compare(String(x), String(y)));
    });
  }

  private toggleSort(column: Column): void {
    const current = this.sort?.key === column.key ? this.sort.dir : null;
    this.sort = current === null ? { key: column.key, dir: 'asc' } : current === 'asc' ? { key: column.key, dir: 'desc' } : null;
    emit(this, 'lcm-sort', this.sort);
  }

  private setFilter(key: string, value: string): void {
    this.filters = { ...this.filters, [key]: value };
    emit(this, 'lcm-filter', { filters: this.filters });
  }

  private setSelected(ids: string[]): void {
    this.selected = ids;
    emit(this, 'lcm-selection-change', { selected: ids });
  }

  private toggleRow(id: string): void {
    this.setSelected(this.selected.includes(id) ? this.selected.filter((x) => x !== id) : [...this.selected, id]);
  }

  private toggleAll(rows: Row[], allSelected: boolean): void {
    const ids = rows.map((r) => r.id);
    this.setSelected(allSelected ? this.selected.filter((id) => !ids.includes(id)) : [...new Set([...this.selected, ...ids])]);
  }

  private toggleColumn(key: string): void {
    const hidden = new Set(this.hiddenColumns ?? this.columns.filter((c) => c.hidden).map((c) => c.key));
    if (hidden.has(key)) hidden.delete(key);
    else hidden.add(key);
    this.hiddenColumns = [...hidden];
    emit(this, 'lcm-columns-change', { hidden: this.hiddenColumns });
  }

  private onChooserToggle(event: Event): void {
    if ((event as ToggleEvent).newState !== 'open') return;
    const button = this.renderRoot.querySelector('.tool-btn.cols');
    if (button) placeFloating(button, event.target as HTMLElement, { align: 'start', gap: 6 });
  }

  private onRowKeyDown(event: KeyboardEvent, row: Row, rows: Row[]): void {
    if (event.target !== event.currentTarget) return;
    const index = rows.indexOf(row);
    const focusRow = (to: number) => {
      const target = rows[Math.min(Math.max(to, 0), rows.length - 1)];
      if (!target) return;
      event.preventDefault();
      this._focusId = target.id;
      this.updateComplete.then(() => this.renderRoot.querySelector<HTMLElement>(`tr[data-id="${CSS.escape(target.id)}"]`)?.focus());
    };
    if (event.key === 'ArrowDown') focusRow(index + 1);
    else if (event.key === 'ArrowUp') focusRow(index - 1);
    else if (event.key === 'Home') focusRow(0);
    else if (event.key === 'End') focusRow(rows.length - 1);
    else if (event.key === 'Enter') emit(this, 'lcm-row-activate', { id: row.id });
    else if (event.key === ' ' && this.selectable) {
      event.preventDefault();
      this.toggleRow(row.id);
    }
  }

  private renderCell(column: Column, row: Row) {
    if (column.render) return column.render(row);
    const value = row[column.key] as Record<string, string> | string | number | undefined;
    if (column.kind === 'status') {
      const status = value as { level: StatusLevel; label: string };
      return html`<lcm-status level=${status.level} label=${status.label}></lcm-status>`;
    }
    if (column.kind === 'os') {
      const os = value as { os: string; label: string };
      return html`<lcm-os os=${os.os} label=${os.label}></lcm-os>`;
    }
    return String(value ?? '');
  }

  private renderHeadCell(column: Column) {
    const sorted = this.sort?.key === column.key ? this.sort.dir : null;
    const ariaSort = sorted === 'asc' ? 'ascending' : sorted === 'desc' ? 'descending' : 'none';
    const align = column.align === 'end' ? 'end' : '';
    const label = column.labelHidden ? html`<span class="sr-only">${column.label}</span>` : column.label;
    if (!column.sortable) return html`<th class=${align} scope="col">${label}</th>`;
    return html`<th class=${align} scope="col" aria-sort=${ariaSort}>
      <button class="sort-btn" type="button" @click=${() => this.toggleSort(column)}>
        ${label}${icon(sorted === 'asc' ? 'sort-asc' : sorted === 'desc' ? 'sort-desc' : 'sort')}
      </button>
    </th>`;
  }

  private renderFilterCell(column: Column) {
    const value = this.filters[column.key] ?? '';
    const label = `${this.labels.filter} ${column.label}`;
    if (column.filter === 'select') {
      return html`<td><select class="filter-input" aria-label=${label} @change=${(e: Event) => this.setFilter(column.key, (e.target as HTMLSelectElement).value)}>
        <option value="">${this.labels.all}</option>
        ${(column.options ?? []).map((o) => html`<option value=${o.value.toLowerCase()} ?selected=${o.value.toLowerCase() === value}>${o.label}</option>`)}
      </select></td>`;
    }
    if (column.filter === 'text') {
      return html`<td><input class="filter-input" type="search" maxlength=${LIMITS.query} aria-label=${label} placeholder=${this.labels.filter} .value=${value} @input=${(e: Event) => this.setFilter(column.key, (e.target as HTMLInputElement).value)} /></td>`;
    }
    return html`<td></td>`;
  }

  private renderToolbar() {
    const hidden = new Set(this.hiddenColumns ?? this.columns.filter((c) => c.hidden).map((c) => c.key));
    return html`<div class="toolbar">
      <slot name="toolbar"></slot><span class="grow"></span>
      ${this.densityToggle
        ? html`<div class="seg" role="group" aria-label=${this.labels.caption}>
            <button type="button" aria-pressed=${String(this.density === 'comfortable')} @click=${() => this.setDensity('comfortable')}>${this.labels.densityComfortable}</button>
            <button type="button" aria-pressed=${String(this.density === 'compact')} @click=${() => this.setDensity('compact')}>${this.labels.densityCompact}</button>
          </div>`
        : nothing}
      ${this.noChooser
        ? nothing
        : html`<button class="tool-btn cols" type="button" popovertarget=${this.chooserId}>${icon('columns')}${this.labels.columns}</button>
            <div id=${this.chooserId} class="chooser" popover="auto" role="group" aria-label=${this.labels.columns} @toggle=${this.onChooserToggle}>
              ${this.columns.map(
                (c) => html`<label><input type="checkbox" .checked=${!hidden.has(c.key)} ?disabled=${c.locked} @change=${() => this.toggleColumn(c.key)} />${c.label}</label>`,
              )}
            </div>`}
    </div>`;
  }

  private setDensity(density: 'comfortable' | 'compact'): void {
    this.density = density;
    emit(this, 'lcm-density-change', { density });
  }

  private renderBulk() {
    const text = this.labels.selected.replace('{count}', String(this.selected.length));
    return html`<div class="bulk" role="region" aria-label=${text}>
      <span class="count" aria-live="polite">${text}</span>
      <slot name="bulk"></slot>
      <button class="tool-btn" type="button" @click=${() => this.setSelected([])}>${this.labels.clear}</button>
    </div>`;
  }

  override render() {
    const columns = this.visibleColumns;
    const rows = this.viewRows;
    const fresh = this.fresh.track(this.rows);
    const viewIds = rows.map((r) => r.id);
    const selectedInView = viewIds.filter((id) => this.selected.includes(id)).length;
    const allSelected = rows.length > 0 && selectedInView === rows.length;
    const hasFilters = columns.some((c) => c.filter);
    const focusId = viewIds.includes(this._focusId) ? this._focusId : (rows[0]?.id ?? '');
    return html`
      ${this.selectable && this.selected.length > 0 ? this.renderBulk() : this.noToolbar ? nothing : this.renderToolbar()}
      <div class="scroll">
        <table role="grid" aria-label=${this.labels.caption} aria-rowcount=${rows.length}>
          <thead>
            <tr class="labels">
              ${this.selectable
                ? html`<th class="sel" scope="col"><input type="checkbox" aria-label=${this.labels.selectAll} .checked=${allSelected} .indeterminate=${selectedInView > 0 && !allSelected} @change=${() => this.toggleAll(rows, allSelected)} /></th>`
                : nothing}
              ${columns.map((c) => this.renderHeadCell(c))}
            </tr>
            ${hasFilters
              ? html`<tr class="filters">${this.selectable ? html`<td class="sel"></td>` : nothing}${columns.map((c) => this.renderFilterCell(c))}</tr>`
              : nothing}
          </thead>
          <tbody>
            ${repeat(
              rows,
              (r) => r.id,
              (row) => html`<tr
                data-id=${row.id}
                data-active=${String(String(row.id) === this.activeId)}
                ?data-fresh=${fresh.has(String(row.id))}
                tabindex=${row.id === focusId ? '0' : '-1'}
                aria-selected=${this.selectable ? String(this.selected.includes(row.id)) : nothing}
                @keydown=${(e: KeyboardEvent) => this.onRowKeyDown(e, row, rows)}
                @focus=${() => (this._focusId = row.id)}
                @click=${() => emit(this, 'lcm-row-activate', { id: row.id })}
              >
                ${this.selectable
                  ? html`<td class="sel" @click=${(e: Event) => e.stopPropagation()}><input type="checkbox" aria-label=${this.labels.selectRow.replace('{name}', String(row.name ?? row.id))} .checked=${this.selected.includes(row.id)} @change=${() => this.toggleRow(row.id)} /></td>`
                  : nothing}
                ${columns.map((c) => html`<td class=${[c.kind === 'mono' ? 'mono' : '', c.align === 'end' ? 'end' : ''].join(' ').trim()}>${this.renderCell(c, row)}</td>`)}
              </tr>`,
            )}
          </tbody>
        </table>
        ${rows.length === 0 ? html`<div class="empty">${this.labels.empty}</div>` : nothing}
      </div>
    `;
  }
}
customElements.define('lcm-data-table', LcmDataTable);
