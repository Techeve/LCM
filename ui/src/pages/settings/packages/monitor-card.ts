import { LitElement, css, html, nothing } from 'lit';
import '../../../components/lcm-badge';
import '../../../components/lcm-button';
import '../../../components/lcm-card';
import { toast } from '../../../components/lcm-toaster';
import { baseStyles } from '../../../components/shared';
import { ALERT_EVENTS_URL, ALERT_RULES_URL, normaliseAlertEvents, normaliseAlertRules, settingsMoreApi, type AlertEventPage, type AlertRule } from '../../../api/settings-more';
import { auth } from '../../../lib/auth';
import { I18nController } from '../../../lib/controllers';
import { formatDateTime } from '../../../lib/format';
import { errorText, t } from '../../../lib/i18n';
import { LiveData } from '../../../lib/live-data';
import { monitorState, type MonitorState } from './kit';

const TONE: Record<MonitorState, string> = { active: 'ok', logOnly: 'warn', disabled: 'neutral', none: 'neutral' };

const STATE_KEYS: Record<MonitorState, [string, string]> = {
  active: ['settings.more.monitor.active', 'settings.more.monitor.activeHint'],
  logOnly: ['settings.more.monitor.logOnly', 'settings.more.monitor.logOnlyHint'],
  disabled: ['settings.more.monitor.disabled', 'settings.more.monitor.disabledHint'],
  none: ['settings.more.monitor.none', 'settings.more.monitor.noneHint'],
};

/**
 * <lcm-settings-monitor type="apt_cacher_down" intro="…" rule-name="…"> – Überwachung einer Selbstbeobachtung über
 * eine Alarmregel: Stand der Regel, letztes Ereignis, Regel mit einem Klick anlegen. Ohne `alerts:manage` nur ein Hinweis.
 */
export class LcmSettingsMonitor extends LitElement {
  static override properties = {
    type: { type: String },
    intro: { type: String },
    ruleName: { type: String, attribute: 'rule-name' },
    _creating: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      p {
        margin: 0 0 var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .state {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
        color: var(--text-1);
      }
      .tools {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
      }
    `,
  ];

  declare type: string;
  declare intro: string;
  declare ruleName: string;
  declare _creating: boolean;
  readonly #rules = new LiveData<AlertRule[]>(this, {
    url: () => (auth.can('alerts:manage') ? ALERT_RULES_URL : null),
    transform: normaliseAlertRules,
  });
  readonly #events = new LiveData<AlertEventPage>(this, {
    url: () => (auth.can('alerts:manage') && this.type ? `${ALERT_EVENTS_URL}?type=${encodeURIComponent(this.type)}&limit=1` : null),
    topics: () => (auth.can('alerts:manage') ? ['alerts'] : []),
    transform: normaliseAlertEvents,
  });

  constructor() {
    super();
    this.type = '';
    this.intro = '';
    this.ruleName = '';
    this._creating = false;
    new I18nController(this);
  }

  async #create(): Promise<void> {
    this._creating = true;
    try {
      await settingsMoreApi.saveAlertRule(null, {
        name: this.ruleName,
        type: this.type,
        enabled: true,
        group_ids: [],
        channel_id: null,
        severity: 'critical',
        threshold_percent: 0,
        forecast_days: 0,
        max_outdated: 0,
        min_severity: '',
        heartbeat_hours: 0,
        cooldown_minutes: 60,
      });
      toast({ tone: 'ok', title: t('settings.more.monitor.created'), message: t('settings.more.monitor.createdHint') });
    } catch (error) {
      toast({ tone: 'crit', title: t('settings.more.monitor.createFailed'), message: errorText(error) });
    } finally {
      this._creating = false;
    }
  }

  #body() {
    if (!auth.can('alerts:manage')) return html`<p>${t('settings.more.monitor.noPermission')}</p>`;
    const rules = this.#rules.data;
    if (!rules) return nothing;
    const state = monitorState(rules, this.type);
    const [label, hint] = STATE_KEYS[state];
    const last = this.#events.data?.items[0];
    return html`<p class="state"><lcm-badge tone=${TONE[state]}>${t(label)}</lcm-badge> ${t(hint)}</p>
      ${last ? html`<p>${t('settings.more.monitor.lastEvent', { time: formatDateTime(last.created_at), text: last.description })}</p>` : nothing}
      <div class="tools">
        ${state === 'none'
          ? html`<lcm-button variant="primary" size="small" icon="bell" ?busy=${this._creating} @click=${() => void this.#create()}>
              ${t('settings.more.monitor.create')}
            </lcm-button>`
          : nothing}
        <lcm-button variant="ghost" size="small" href="/settings/alerts">${t('settings.more.monitor.manage')}</lcm-button>
      </div>`;
  }

  override render() {
    return html`<lcm-card heading=${t('settings.more.monitor.title')}>
      <p>${this.intro}</p>
      ${this.#body()}
    </lcm-card>`;
  }
}
customElements.define('lcm-settings-monitor', LcmSettingsMonitor);
