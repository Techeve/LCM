import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-explainer';
import '../../components/lcm-input';
import '../../components/lcm-list-pager';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import '../../components/lcm-switch';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import {
  ALERT_RULES_URL,
  CHANNELS_URL,
  SERVER_GROUPS_URL,
  alertEventsUrl,
  normaliseAlertEvents,
  normaliseAlertRules,
  normaliseChannels,
  normaliseGroupOptions,
  settingsMoreApi,
  type AlertEvent,
  type AlertEventPage,
  type AlertRule,
  type Channel,
  type GroupOption,
} from '../../api/settings-more';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatNumber } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import {
  ALERT_RANGES,
  ALERT_TYPES,
  CVE_SEVERITY_KEYS,
  SEVERITY_KEYS,
  SEVERITY_TONES,
  alertTypeLabel,
  emptyRuleForm,
  isSelfType,
  ruleBody,
  ruleFormOf,
  scopeText,
  silentRules,
  thresholdText,
  validateRuleForm,
  type RuleForm,
} from './messages/models';
import './packages/cells';
import { ModalForm, explainerToggle, generalError, sectionStyles, tableLabels } from './packages/kit';
import './packages/settings-modal';

const PAGE_SIZE = 50;

const FIELDS: Record<string, string> = { 'field:name': 'name', 'alert.name_required': 'name' };

function pageOf(query: URLSearchParams): number {
  const page = Number(query.get('page'));
  return Number.isInteger(page) && page > 1 ? page : 1;
}

/** Einstellungen → Alarme: Alarmregeln, ihre Kanäle und der Verlauf ausgelöster Alarme. */
export class SettingsAlerts extends LitElement {
  static override properties = { _dialogError: { state: true }, _delete: { state: true }, _busy: { state: true }, _evaluating: { state: true } };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .chips {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
      }
      .chip {
        padding: 0.25rem 0.75rem;
        border: 0;
        border-radius: var(--radius-pill);
        background: var(--surface-1);
        box-shadow: inset 0 0 0 1px var(--line-strong);
        color: var(--text-1);
        font: inherit;
        font-size: var(--fs-s);
        cursor: pointer;
      }
      .chip:hover {
        background: var(--surface-3);
      }
      .chip[aria-pressed='true'] {
        background: var(--accent-soft);
        box-shadow: none;
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
      }
      .field-label {
        margin: 0 0 var(--space-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .field-hint {
        margin: var(--space-2) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _delete: AlertRule | null;
  declare _busy: boolean;
  declare _evaluating: boolean;
  readonly #rules = new LiveData<AlertRule[]>(this, { url: () => ALERT_RULES_URL, transform: normaliseAlertRules });
  readonly #channels = new LiveData<Channel[]>(this, {
    url: () => (auth.can('notifications:manage') ? CHANNELS_URL : null),
    transform: normaliseChannels,
  });
  readonly #groups = new LiveData<GroupOption[]>(this, {
    url: () => (auth.can('groups:read') ? SERVER_GROUPS_URL : null),
    transform: normaliseGroupOptions,
  });
  readonly #events = new LiveData<AlertEventPage>(this, {
    url: () => alertEventsUrl(pageOf(router.state.query), PAGE_SIZE),
    topics: () => ['alerts'],
    transform: normaliseAlertEvents,
  });
  readonly #modal = new ModalForm<RuleForm>(this, (modal) => {
    if (modal.kind === 'new') return emptyRuleForm();
    if (modal.kind !== 'edit' || !this.#rules.data) return this.#rules.loading ? undefined : null;
    const rule = this.#rules.data.find((r) => r.id === modal.id);
    return rule ? ruleFormOf(rule) : null;
  });

  constructor() {
    super();
    this._dialogError = '';
    this._delete = null;
    this._busy = false;
    this._evaluating = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    palette.register(this, [
      { id: 'alert-new', key: 'n', label: t('settings.alerts.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) },
      { id: 'alert-evaluate', label: t('settings.alerts.evaluate'), icon: 'play', run: () => void this.#evaluate() },
    ]);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  override willUpdate(): void {
    this.#modal.sync();
  }

  get #editing(): AlertRule | undefined {
    const id = this.#modal.editId;
    return id ? this.#rules.data?.find((r) => r.id === id) : undefined;
  }

  #channelName(id: number | null): string {
    if (!id) return '';
    return this.#channels.data?.find((c) => c.id === id)?.name ?? `#${id}`;
  }

  async #save(): Promise<void> {
    const form = this.#modal.form;
    if (!form) return;
    const id = this.#modal.editId;
    const valid = Object.keys(validateRuleForm(form)).length === 0;
    if (await this.#modal.submit(valid, () => settingsMoreApi.saveAlertRule(id, ruleBody(form)), FIELDS)) {
      toast({ tone: 'ok', title: t(id ? 'settings.alerts.notice.saved' : 'settings.alerts.notice.created', { name: form.name.trim() }) });
    }
  }

  async #evaluate(): Promise<void> {
    this._evaluating = true;
    try {
      await settingsMoreApi.evaluateAlerts();
      toast({ tone: 'ok', title: t('settings.alerts.notice.evaluated') });
    } catch (error) {
      toast({ tone: 'crit', title: t('settings.alerts.notice.failed'), message: errorText(error) });
    } finally {
      this._evaluating = false;
    }
  }

  async #confirmDelete(): Promise<void> {
    const rule = this._delete;
    if (!rule) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.deleteAlertRule(rule.id);
      toast({ tone: 'ok', title: t('settings.alerts.notice.deleted', { name: rule.name }) });
      this._delete = null;
      this.#modal.close();
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  get #ruleColumns(): Column<AlertRule>[] {
    return [
      {
        key: 'name',
        label: t('settings.alerts.column.name'),
        sortable: true,
        render: (r) =>
          html`<lcm-settings-cell sub=${alertTypeLabel(r.type)}
            ><strong>${r.name}</strong>${r.enabled ? nothing : html`<lcm-badge>${t('settings.alerts.off')}</lcm-badge>`}</lcm-settings-cell
          >`,
      },
      { key: 'threshold', label: t('settings.alerts.column.threshold'), render: (r) => html`<lcm-settings-cell sub=${scopeText(r)}>${thresholdText(r)}</lcm-settings-cell>` },
      {
        key: 'channel',
        label: t('settings.alerts.column.channel'),
        render: (r) => (r.channel_id ? this.#channelName(r.channel_id) : html`<lcm-badge tone="warn">${t('settings.alerts.logOnly')}</lcm-badge>`),
      },
      {
        key: 'severity',
        label: t('settings.alerts.column.severity'),
        sortable: true,
        render: (r) => html`<lcm-badge tone=${SEVERITY_TONES[r.severity] ?? 'info'}>${t(SEVERITY_KEYS[r.severity] ?? 'settings.alerts.severity.warning')}</lcm-badge>`,
      },
    ];
  }

  get #eventColumns(): Column<AlertEvent>[] {
    return [
      {
        key: 'rule_name',
        label: t('settings.alerts.history.rule'),
        render: (r) =>
          html`<lcm-settings-cell sub=${[r.server_name, formatDateTime(r.created_at)].filter(Boolean).join(' · ')}
            ><span>${r.rule_name}</span>
            <lcm-badge tone=${SEVERITY_TONES[r.severity] ?? 'info'}>${t(SEVERITY_KEYS[r.severity] ?? 'settings.alerts.severity.warning')}</lcm-badge></lcm-settings-cell
          >`,
      },
      { key: 'description', label: t('settings.alerts.history.description'), render: (r) => html`<lcm-settings-cell><span title=${r.description}>${r.description}</span></lcm-settings-cell>` },
      {
        key: 'notified',
        label: t('settings.alerts.history.notified'),
        render: (r) => {
          if (r.notified) return html`<lcm-badge tone="ok">${t('settings.alerts.history.sent')}</lcm-badge>`;
          if (r.notify_error) return html`<lcm-badge tone="crit" title=${r.notify_error}>${t('settings.alerts.history.error')}</lcm-badge>`;
          return html`<span class="muted">–</span>`;
        },
      },
    ];
  }

  #rulesCard() {
    const rules = this.#rules.data;
    let body;
    if (!rules) body = this.#rules.error ? html`<p class="callout warn">${this.#rules.error}</p>` : html`<lcm-skeleton lines="4"></lcm-skeleton>`;
    else if (rules.length === 0) body = html`<lcm-empty-state level="3" icon="bell" heading=${t('settings.alerts.empty.title')} text=${t('settings.alerts.empty.text')}></lcm-empty-state>`;
    else {
      body = html`<lcm-data-table
        no-toolbar
        density="comfortable"
        active-id=${String(this.#modal.editId ?? '')}
        .columns=${this.#ruleColumns}
        .rows=${rules.map((r) => ({ ...r, id: String(r.id) }))}
        .labels=${tableLabels(t('settings.alerts.caption'), t('settings.alerts.empty.title'))}
        @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#modal.show({ kind: 'edit', id: Number(e.detail.id) })}
      ></lcm-data-table>`;
    }
    return html`<lcm-card level="2" heading=${t('settings.alerts.rules')}>
      <span slot="actions" class="tools">
        <lcm-button variant="ghost" size="small" icon="play" ?busy=${this._evaluating} @click=${() => void this.#evaluate()}>${t('settings.alerts.evaluate')}</lcm-button>
        <lcm-button variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>${t('settings.alerts.add')}</lcm-button>
      </span>
      ${body}
    </lcm-card>`;
  }

  #silentWarning() {
    const count = silentRules(this.#rules.data ?? []);
    if (count === 0) return nothing;
    const noChannels = this.#channels.data?.length === 0;
    return html`<p class="callout warn" role="status">
      ${t('settings.alerts.silent', { count })}
      ${noChannels ? html`<a href="/settings/notifications">${t('settings.alerts.silentLink')}</a>` : nothing}
    </p>`;
  }

  #historyCard() {
    const page = this.#events.data;
    const current = pageOf(router.state.query);
    let body;
    if (!page) body = this.#events.error ? html`<p class="callout warn">${this.#events.error}</p>` : html`<lcm-skeleton lines="4"></lcm-skeleton>`;
    else if (page.items.length === 0) body = html`<p class="muted small">${t('settings.alerts.history.empty')}</p>`;
    else {
      const from = (current - 1) * PAGE_SIZE + 1;
      body = html`<lcm-data-table
          no-toolbar
          density="comfortable"
          .columns=${this.#eventColumns}
          .rows=${page.items}
          .labels=${tableLabels(t('settings.alerts.history.title'), t('settings.alerts.history.empty'))}
        ></lcm-data-table>
        <lcm-list-pager
          page=${current}
          page-size=${PAGE_SIZE}
          total=${page.total}
          range-label=${t('settings.alerts.history.range', { from: formatNumber(from), to: formatNumber(from + page.items.length - 1), total: formatNumber(page.total) })}
          previous-label=${t('settings.alerts.history.previous')}
          next-label=${t('settings.alerts.history.next')}
          @lcm-page=${(e: CustomEvent<{ page: number }>) => router.setQuery({ page: e.detail.page > 1 ? e.detail.page : null })}
        ></lcm-list-pager>`;
    }
    return html`<lcm-card level="2" heading=${t('settings.alerts.history.title')}>${body}</lcm-card>`;
  }

  #number(form: RuleForm, errors: Record<string, string | undefined>, field: keyof typeof ALERT_RANGES, label: string) {
    return html`<lcm-input
      label=${label}
      digits
      maxlength=${String(ALERT_RANGES[field].max).length}
      width="s"
      error=${this.#modal.fieldText(errors, field, ALERT_RANGES[field])}
      .value=${String(form[field])}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ [field]: e.detail.value } as Partial<RuleForm>)}
    ></lcm-input>`;
  }

  #thresholdField(form: RuleForm, errors: Record<string, string | undefined>) {
    switch (ALERT_TYPES[form.type]?.threshold) {
      case 'percent':
        return this.#number(form, errors, 'threshold_percent', t('settings.alerts.form.percent'));
      case 'days':
        return this.#number(form, errors, 'forecast_days', t('settings.alerts.form.days'));
      case 'outdated':
        return this.#number(form, errors, 'max_outdated', t('settings.alerts.form.outdated'));
      case 'hours':
        return this.#number(form, errors, 'heartbeat_hours', t('settings.alerts.form.hours'));
      case 'severity':
        return html`<lcm-select
          label=${t('settings.alerts.form.minSeverity')}
          width="s"
          .options=${Object.entries(CVE_SEVERITY_KEYS).map(([value, key]) => ({ value, label: t(key) }))}
          .value=${form.min_severity}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ min_severity: e.detail.value })}
        ></lcm-select>`;
      default:
        return nothing;
    }
  }

  #toggleGroup(form: RuleForm, id: number): void {
    const group_ids = form.group_ids.includes(id) ? form.group_ids.filter((g) => g !== id) : [...form.group_ids, id];
    this.#modal.set({ group_ids });
  }

  #groupsField(form: RuleForm) {
    if (isSelfType(form.type)) {
      return html`<div><p class="field-label">${t('settings.alerts.form.groups')}</p><p class="field-hint">${t('settings.alerts.form.selfScope')}</p></div>`;
    }
    const groups = this.#groups.data ?? [];
    return html`<div>
      <p class="field-label">${t('settings.alerts.form.groups')}</p>
      <div class="chips" role="group" aria-label=${t('settings.alerts.form.groups')}>
        ${groups.map(
          (group) => html`<button class="chip" type="button" aria-pressed=${String(form.group_ids.includes(group.id))} @click=${() => this.#toggleGroup(form, group.id)}>
            ${group.name}
          </button>`,
        )}
      </div>
      <p class="field-hint">${form.group_ids.length ? t('settings.alerts.form.groupsSome', { count: form.group_ids.length }) : t('settings.alerts.form.groupsAll')}</p>
    </div>`;
  }

  #form(form: RuleForm) {
    const errors = validateRuleForm(form);
    const type = ALERT_TYPES[form.type];
    const channels = this.#channels.data ?? [];
    const cooldown = Number(form.cooldown_minutes);
    return html`${this.#input(form, errors)}
      <lcm-select
          label=${t('settings.alerts.form.type')}
          width="l"
          .options=${Object.entries(ALERT_TYPES).map(([value, info]) => ({ value, label: t(info.label) }))}
          .value=${form.type}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ type: e.detail.value })}
        ></lcm-select>
      ${type ? html`<p class="callout">${t(type.hint)}</p>` : nothing} ${this.#thresholdField(form, errors)} ${this.#groupsField(form)}
      <lcm-select
        label=${t('settings.alerts.form.channel')}
        hint=${form.channel_id ? '' : t('settings.alerts.form.channelNone')}
        .options=${[{ value: '', label: t('settings.alerts.form.logOnly') }, ...channels.map((c) => ({ value: String(c.id), label: c.name }))]}
        .value=${form.channel_id}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ channel_id: e.detail.value })}
      ></lcm-select>
      <lcm-select
          label=${t('settings.alerts.form.severity')}
          width="s"
          .options=${Object.entries(SEVERITY_KEYS).map(([value, key]) => ({ value, label: t(key) }))}
          .value=${form.severity}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ severity: e.detail.value })}
        ></lcm-select>
        <lcm-input
          label=${t('settings.alerts.form.cooldown')}
          width="s"
          digits
          maxlength=${String(ALERT_RANGES.cooldown_minutes.max).length}
          hint=${cooldown === 0 ? t('settings.alerts.form.cooldownZero') : t('settings.alerts.form.cooldownHint')}
          error=${this.#modal.fieldText(errors, 'cooldown_minutes', ALERT_RANGES.cooldown_minutes)}
          .value=${form.cooldown_minutes}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ cooldown_minutes: e.detail.value })}
        ></lcm-input>
      <lcm-switch
        label=${t('settings.alerts.form.enabled')}
        on-label=${t('settings.more.switch.on')}
        off-label=${t('settings.more.switch.off')}
        on-text=${t('settings.alerts.form.enabledOn')}
        off-text=${t('settings.alerts.form.enabledOff')}
        .checked=${form.enabled}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#modal.set({ enabled: e.detail.checked })}
      ></lcm-switch>`;
  }

  #input(form: RuleForm, errors: Record<string, string | undefined>) {
    return html`<lcm-input
      label=${t('settings.alerts.form.name')}
      placeholder=${t('settings.alerts.form.namePlaceholder')}
      maxlength=${LIMITS.name}
      error=${this.#modal.fieldText(errors, 'name', LIMITS.name)}
      .value=${form.name}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ name: e.detail.value })}
    ></lcm-input>`;
  }

  #modalView() {
    const form = this.#modal.form;
    const isNew = this.#modal.editId === null;
    return html`<lcm-settings-modal
      ?open=${form !== null}
      heading=${t(isNew ? 'settings.alerts.form.newTitle' : 'settings.alerts.form.editTitle')}
      save-label=${t(isNew ? 'settings.more.modal.create' : 'settings.more.modal.save')}
      ?dirty=${this.#modal.dirty}
      ?busy=${this.#modal.busy}
      error=${generalError(this.#modal.error)}
      @lcm-submit=${() => void this.#save()}
      @lcm-close=${() => this.#modal.close()}
    >
      ${form ? this.#form(form) : nothing}
      ${this.#editing
        ? html`<lcm-button slot="footer-start" variant="ghost" icon="trash" @click=${() => (this._delete = this.#editing ?? null)}>${t('settings.alerts.delete.label')}</lcm-button>`
        : nothing}
    </lcm-settings-modal>`;
  }

  #deleteDialog() {
    const rule = this._delete;
    return html`<lcm-dialog
      ?open=${rule !== null}
      heading=${t('settings.alerts.delete.title', { name: rule?.name ?? '' })}
      message=${t('settings.alerts.delete.message')}
      .impacts=${[
        { label: t('settings.alerts.delete.history'), value: t('settings.alerts.delete.historyValue'), tone: 'ok' },
        { label: t('settings.more.impact.reversible'), value: t('settings.alerts.delete.reversible'), tone: 'warn' },
      ]}
      confirm-label=${t('settings.alerts.delete.confirm')}
      cancel-label=${t('settings.more.modal.cancel')}
      tone="danger"
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-confirm=${() => void this.#confirmDelete()}
      @lcm-cancel=${() => (this._delete = null)}
    ></lcm-dialog>`;
  }

  #explainer() {
    const toggle = explainerToggle();
    return html`<lcm-explainer topic="settings-alerts" heading=${t('settings.alerts.explainer.title')} collapse-label=${toggle.collapse} expand-label=${toggle.expand}>
      <p>${t('settings.alerts.explainer.intro')}</p>
      <ul>
        <li><strong>${t('settings.alerts.explainer.scopeTitle')}</strong> ${t('settings.alerts.explainer.scope')}</li>
        <li><strong>${t('settings.alerts.explainer.channelTitle')}</strong> ${t('settings.alerts.explainer.channel')}</li>
        <li><strong>${t('settings.alerts.explainer.cooldownTitle')}</strong> ${t('settings.alerts.explainer.cooldown')}</li>
      </ul>
      <p>${t('settings.alerts.explainer.when')}</p>
    </lcm-explainer>`;
  }

  override render() {
    return html`<div class="layout explain">
        <div class="main">${this.#silentWarning()} ${this.#rulesCard()} ${this.#historyCard()}</div>
        ${this.#explainer()}
      </div>
      ${this.#modalView()} ${this.#deleteDialog()}`;
  }
}
customElements.define('lcm-settings-alerts', SettingsAlerts);
