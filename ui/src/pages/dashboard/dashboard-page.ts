import { LitElement, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-empty-state';
import '../../components/lcm-skeleton';
import '../../components/lcm-status-tile';
import { DASHBOARD_URL, type Dashboard } from '../../api/dashboard';
import { baseStyles, type StatusLevel } from '../../components/shared';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { FadeOnChange, FreshKeys } from '../../lib/motion';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { alertsColumn, attentionColumn, itemKey, jobsColumn, upcomingColumn } from './dash-columns';
import { dashStyles } from './dash-styles';

interface TileDef {
  field: 'excellent' | 'green' | 'yellow' | 'red' | 'incapable';
  level: StatusLevel;
  label: string;
  hint: string;
  href: string;
}

// Jede Kachel führt zur gefilterten Serverliste.
const TILES: TileDef[] = [
  { field: 'excellent', level: 'great', label: 'dashboard.tile.excellent.label', hint: 'dashboard.tile.excellent.hint', href: '/servers?status=excellent' },
  { field: 'green', level: 'ok', label: 'dashboard.tile.green.label', hint: 'dashboard.tile.green.hint', href: '/servers?status=green' },
  { field: 'yellow', level: 'warn', label: 'dashboard.tile.yellow.label', hint: 'dashboard.tile.yellow.hint', href: '/servers?status=yellow' },
  { field: 'red', level: 'crit', label: 'dashboard.tile.red.label', hint: 'dashboard.tile.red.hint', href: '/servers?status=red' },
  { field: 'incapable', level: 'unknown', label: 'dashboard.tile.incapable.label', hint: 'dashboard.tile.incapable.hint', href: '/servers?incapable=1' },
];

/** Dashboard: Zustand der Flotte auf einen Blick, live über das Thema `dashboard`. */
export class DashboardPage extends LitElement {
  static override styles = [baseStyles, dashStyles];

  readonly #dashboard = new LiveData<Dashboard>(this, { url: () => DASHBOARD_URL, topics: () => ['dashboard'] });
  readonly #fresh = {
    attention: new FreshKeys(itemKey.attention),
    jobs: new FreshKeys(itemKey.job),
    upcoming: new FreshKeys(itemKey.run),
    alerts: new FreshKeys(itemKey.alert),
  };

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new FadeOnChange(this, () => this.#dashboard.data !== undefined, () => this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    if (auth.can('servers:write')) {
      palette.register(this, [{ id: 'dashboard.add-server', label: t('servers.add'), icon: 'plus', key: 'n', keywords: ['join', 'onboarding'], run: () => void router.navigate('/servers/join') }]);
    }
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  #renderTiles(data: Dashboard): TemplateResult {
    return html`<div class="tiles">
      ${TILES.map((tile) => html`<lcm-status-tile
        level=${tile.level}
        label=${t(tile.label)}
        hint=${t(tile.hint)}
        count=${data.tiles[tile.field]}
        href=${tile.href}
      ></lcm-status-tile>`)}
    </div>`;
  }

  #renderColumns(data: Dashboard): TemplateResult {
    const withAlerts = auth.can('alerts:manage');
    return html`<div class="columns ${withAlerts ? '' : 'no-alerts'}">
      ${attentionColumn(data.attention, data.tiles.total, this.#fresh.attention.track(data.attention))}
      ${jobsColumn(data.recent_jobs, this.#fresh.jobs.track(data.recent_jobs))} ${upcomingColumn(data.upcoming, this.#fresh.upcoming.track(data.upcoming))}
      ${withAlerts ? alertsColumn(data.recent_alerts, this.#fresh.alerts.track(data.recent_alerts)) : nothing}
    </div>`;
  }

  #renderLoading(): TemplateResult {
    const label = t('dashboard.loading');
    return html`<div class="tiles">${TILES.map(() => html`<div class="tile-skeleton"><lcm-skeleton lines="2"></lcm-skeleton></div>`)}</div>
      <div class="columns no-alerts">
        ${[0, 1, 2].map(() => html`<lcm-card level="2"><lcm-skeleton lines="6" label=${label}></lcm-skeleton></lcm-card>`)}
      </div>`;
  }

  #renderFailure(message: string): TemplateResult {
    return html`<lcm-card level="2" class="error">
      <lcm-empty-state level="3" icon="warning" heading=${t('dashboard.error')} text=${message}>
        <lcm-button slot="action" icon="refresh" @click=${() => this.#dashboard.reload()}>${t('dashboard.retry')}</lcm-button>
      </lcm-empty-state>
    </lcm-card>`;
  }

  #renderBody(): TemplateResult {
    const { data, error } = this.#dashboard;
    if (data) return html`${this.#renderTiles(data)}${this.#renderColumns(data)}`;
    return error ? this.#renderFailure(error) : this.#renderLoading();
  }

  override render() {
    const total = this.#dashboard.data?.tiles.total;
    return html`<div class="head">
        <div>
          <h1>${t('dashboard.title')}</h1>
          ${total === undefined ? nothing : html`<p class="live">${t('dashboard.summary', { count: total })}</p>`}
        </div>
        ${auth.can('servers:write') ? html`<lcm-button variant="primary" icon="plus" href="/servers/join">${t('servers.add')}</lcm-button>` : nothing}
      </div>
      ${this.#renderBody()}`;
  }
}
customElements.define('lcm-page-dashboard', DashboardPage);
