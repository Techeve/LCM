import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-switch';
import type { ServerRef } from '../../api/jobs';
import type { Option } from '../../components/lcm-select';
import { baseStyles, emit } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { kindLabel } from './job-labels';
import { hasFilters, type JobFilters } from './jobs-state';

const SEARCH_DELAY_MS = 300;

/**
 * <lcm-jobs-filters .filters=${…} .servers=${…} .kinds=${…}> – Filterleiste der Jobliste.
 * Meldet `lcm-filters-change` mit den geänderten Filtern (`Partial<JobFilters>`) und `lcm-filters-reset`.
 */
export class JobsFilters extends LitElement {
  static override properties = { filters: { attribute: false }, servers: { attribute: false }, kinds: { attribute: false }, triggers: { attribute: false } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .bar {
        display: grid;
        grid-template-columns: minmax(12rem, 2fr) repeat(5, minmax(8rem, 1fr));
        gap: var(--space-3);
        align-items: end;
      }
      .dates {
        display: contents;
      }
      .tail {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
        margin-block-start: var(--space-3);
      }
      @media (max-width: 64rem) {
        .bar {
          grid-template-columns: repeat(2, minmax(0, 1fr));
        }
      }
    `,
  ];

  declare filters: JobFilters;
  declare servers: ServerRef[];
  declare kinds: string[];
  declare triggers: string[];
  #searchTimer: ReturnType<typeof setTimeout> | undefined;

  constructor() {
    super();
    this.servers = [];
    this.kinds = [];
    this.triggers = [];
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    clearTimeout(this.#searchTimer);
  }

  #change(change: Partial<JobFilters>): void {
    emit(this, 'lcm-filters-change', change);
  }

  #search(value: string): void {
    clearTimeout(this.#searchTimer);
    this.#searchTimer = setTimeout(() => this.#change({ q: value.trim() }), SEARCH_DELAY_MS);
  }

  get #statusOptions(): Option[] {
    return [
      { value: '', label: t('jobs.filter.status.finished') },
      { value: 'problems', label: t('jobs.filter.status.problems') },
      { value: 'success', label: t('jobs.filter.status.success') },
      { value: 'failed', label: t('jobs.filter.status.failed') },
      { value: 'aborted', label: t('jobs.filter.status.aborted') },
      { value: 'blocked', label: t('jobs.filter.status.blocked') },
      { value: 'running', label: t('jobs.filter.status.running') },
      { value: 'pending', label: t('jobs.filter.status.pending') },
      { value: 'all', label: t('jobs.filter.status.all') },
    ];
  }

  get #rangeOptions(): Option[] {
    return [
      { value: '', label: t('jobs.filter.range.all') },
      { value: '1h', label: t('jobs.filter.range.1h') },
      { value: '24h', label: t('jobs.filter.range.24h') },
      { value: '7d', label: t('jobs.filter.range.7d') },
      { value: '30d', label: t('jobs.filter.range.30d') },
      { value: 'custom', label: t('jobs.filter.range.custom') },
    ];
  }

  #selected(event: Event): string {
    return (event as CustomEvent<{ value: string }>).detail.value;
  }

  #renderDates() {
    if (this.filters.range !== 'custom') return nothing;
    return html`<lcm-input type="date" width="full" label=${t('jobs.filter.since')} .value=${this.filters.since} @lcm-change=${(e: Event) => this.#change({ since: this.#selected(e) })}></lcm-input>
      <lcm-input type="date" width="full" label=${t('jobs.filter.until')} .value=${this.filters.until} @lcm-change=${(e: Event) => this.#change({ until: this.#selected(e) })}></lcm-input>`;
  }

  override render() {
    const f = this.filters;
    const servers: Option[] = [{ value: '', label: t('jobs.filter.server.all') }, ...this.servers.map((s) => ({ value: String(s.id), label: s.name }))];
    const kinds: Option[] = [{ value: '', label: t('jobs.filter.kind.all') }, ...this.kinds.map((k) => ({ value: k, label: kindLabel(k) }))];
    // Ein Auslöser aus einem kopierten Link bleibt wählbar, auch wenn er gerade nicht in den Optionen steht.
    const triggerValues = f.by && !this.triggers.includes(f.by) ? [f.by, ...this.triggers] : this.triggers;
    const triggers: Option[] = [{ value: '', label: t('jobs.byAll') }, ...triggerValues.map((by) => ({ value: by, label: by }))];
    return html`<div class="bar">
        <lcm-input maxlength=${LIMITS.query} icon="search" type="search" width="full" label=${t('jobs.filter.search')} placeholder=${t('jobs.filter.searchPlaceholder')} .value=${f.q} @lcm-input=${(e: Event) => this.#search(this.#selected(e))}></lcm-input>
        <lcm-select width="full" label=${t('jobs.filter.status.label')} .options=${this.#statusOptions} .value=${f.status} @lcm-change=${(e: Event) => this.#change({ status: this.#selected(e) })}></lcm-select>
        <lcm-select width="full" label=${t('jobs.filter.kind.label')} .options=${kinds} .value=${f.kind} @lcm-change=${(e: Event) => this.#change({ kind: this.#selected(e) })}></lcm-select>
        <lcm-select width="full" label=${t('jobs.filter.server.label')} .options=${servers} .value=${f.server} @lcm-change=${(e: Event) => this.#change({ server: this.#selected(e) })}></lcm-select>
        <lcm-select width="full" label=${t('jobs.by')} .options=${triggers} .value=${f.by} @lcm-change=${(e: Event) => this.#change({ by: this.#selected(e) })}></lcm-select>
        <lcm-select width="full" label=${t('jobs.filter.range.label')} .options=${this.#rangeOptions} .value=${f.range} @lcm-change=${(e: Event) => this.#change({ range: this.#selected(e) })}></lcm-select>
        <div class="dates">${this.#renderDates()}</div>
      </div>
      <div class="tail">
        <lcm-switch label=${t('jobs.filter.health')} .checked=${f.health} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#change({ health: e.detail.checked })}></lcm-switch>
        ${hasFilters(f) ? html`<lcm-button size="small" variant="ghost" icon="undo" @click=${() => emit(this, 'lcm-filters-reset')}>${t('jobs.filter.reset')}</lcm-button>` : nothing}
      </div>`;
  }
}
customElements.define('lcm-jobs-filters', JobsFilters);
