import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-explainer';
import '../../components/lcm-input';
import '../../components/lcm-skeleton';
import '../../components/lcm-tabs';
import '../../components/lcm-term';
import type { TabItem } from '../../components/lcm-tabs';
import { baseStyles } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import {
  ALL_RULES_URL,
  GROUPS_URL,
  groupUrl,
  groupsApi,
  normalizeGroup,
  normalizeGroups,
  normalizeRules,
  normalizeSchedules,
  scheduleUrl,
  type Group,
  type Rule,
  type Schedule,
} from '../../api/groups';
import { ApiError } from '../../lib/api';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { events } from '../../lib/events';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { FadeOnChange } from '../../lib/motion';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { store } from '../../lib/store';
import { findConflicts, overriddenCount, type RuleConflict } from './conflicts';
import { filterGroups } from './group-model';
import './group-modal';
import './groups-list';
import './overview-tab';
import './rule-modal';
import './rules-tab';
import './schedule-modal';
import './schedules-tab';
import './servers-tab';
import { modalQuery, groupPath, parseModal, parseGroupId, parseTab, type Modal, type GroupTab } from './url-state';

const MAX_WATCHED_COUNTS = 40;

interface Counts {
  managers?: number;
  schedules?: number;
}

/**
 * Gruppen: Liste und Detail nebeneinander. Das Detail hat Reiter (Übersicht, Server, Zeitpläne, Regeln), die im Pfad
 * stehen; Suche und offene Fenster stehen in der Query. Auf Zeitplänen und Regeln erklärt eine Spalte, wie beides
 * zusammenspielt.
 */
export class GroupsPage extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    _groups: { state: true },
    _group: { state: true },
    _schedules: { state: true },
    _allRules: { state: true },
    _counts: { state: true },
    _loaded: { state: true },
    _notFound: { state: true },
    _failed: { state: true },
    _disband: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .head {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3) var(--space-5);
        margin-block-end: var(--space-5);
      }
      h1 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .head lcm-input {
        flex: 1;
        min-inline-size: 14rem;
        max-inline-size: 24rem;
        margin-inline-start: auto;
      }
      .layout {
        display: grid;
        grid-template-columns: minmax(0, 1fr);
        gap: var(--gap);
        align-items: start;
      }
      .layout.detail {
        grid-template-columns: minmax(15rem, 21rem) minmax(0, 1fr);
      }
      .list {
        min-inline-size: 0;
      }
      .detail-pane {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
        min-inline-size: 0;
      }
      .back {
        display: none;
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      .title-row {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3) var(--space-4);
      }
      .title-row h2 {
        margin: 0;
        font-size: var(--fs-3xl);
        letter-spacing: var(--head-tracking);
        line-height: 1.1;
      }
      .actions {
        display: flex;
        gap: var(--space-2);
        margin-inline-start: auto;
      }
      .facts {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2) var(--space-5);
        margin-block-start: var(--space-2);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .desc {
        margin: var(--space-2) 0 0;
        color: var(--text-2);
      }
      .tab-body.explain {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: var(--gap);
        align-items: start;
      }
      .tab-main {
        min-inline-size: 0;
      }
      lcm-explainer ul {
        margin: 0 0 var(--space-3);
        padding-inline-start: 1.1rem;
      }
      lcm-explainer li {
        margin-block-end: var(--space-2);
      }
      @media (max-width: 75rem) {
        .tab-body.explain {
          grid-template-columns: minmax(0, 1fr);
        }
        .tab-body.explain lcm-explainer {
          inline-size: auto;
        }
      }
      @media (max-width: 60rem) {
        .layout.detail {
          grid-template-columns: minmax(0, 1fr);
        }
        .layout.detail .list {
          display: none;
        }
        .back {
          display: inline;
        }
      }
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _groups: Group[];
  declare _group: Group | null;
  declare _schedules: Schedule[];
  declare _allRules: Rule[];
  declare _counts: Map<number, Counts>;
  declare _loaded: boolean;
  declare _notFound: boolean;
  declare _failed: boolean;
  declare _disband: boolean;
  declare _busy: boolean;
  #listStop: (() => void) | undefined;
  #detailStops: Array<() => void> = [];
  #countStops = new Map<number, Array<() => void>>();
  #detailId: number | null = null;
  #eventStops: Array<() => void> = [];

  constructor() {
    super();
    this._dialogError = '';
    this._groups = [];
    this._group = null;
    this._schedules = [];
    this._allRules = [];
    this._counts = new Map();
    this._loaded = false;
    this._notFound = false;
    this._failed = false;
    this._disband = false;
    this._busy = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new FadeOnChange(this, () => this._group?.id, () => this.renderRoot.querySelector('.detail-pane'));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#watch(GROUPS_URL, (data) => {
      this._groups = normalizeGroups(data);
      this._loaded = true;
      this._failed = false;
      this.#watchCounts(this._groups);
    }, () => (this._failed = true)).then((stop) => (this.#listStop = stop));
    this.#eventStops.push(
      events.subscribe(['servers', 'jobs']),
      events.onEvent((message) => {
        if (message.event === 'job.finished') store.invalidate(GROUPS_URL);
      }),
    );
    this.#registerPalette();
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#listStop?.();
    this.#detailStops.forEach((stop) => stop());
    this.#countStops.forEach((stops) => stops.forEach((stop) => stop()));
    this.#countStops.clear();
    this.#eventStops.forEach((stop) => stop());
    this.#eventStops = [];
    palette.unregister(this);
  }

  /** Abonniert eine Adresse und lädt sie einmal; die Rückgabe löst das Abonnement. */
  async #watch(url: string, apply: (data: unknown) => void, fail?: (error: unknown) => void): Promise<() => void> {
    const stop = store.subscribe(url, apply, fail);
    store.get(url).then(apply, fail ?? (() => {}));
    return stop;
  }

  /** Verwalter- und Zeitplan-Zahlen, die die Liste nicht nennt, kommen aus den Details. */
  #watchCounts(groups: Group[]): void {
    for (const group of groups.slice(0, MAX_WATCHED_COUNTS)) {
      if (this.#countStops.has(group.id) || (group.manager_count !== null && group.schedule_count !== null)) continue;
      const set = (patch: Counts) => (this._counts = new Map(this._counts).set(group.id, { ...this._counts.get(group.id), ...patch }));
      const stops: Array<() => void> = [];
      this.#countStops.set(group.id, stops);
      if (group.manager_count === null) {
        void this.#watch(groupUrl(group.id), (data) => set({ managers: normalizeGroup(data).manager_count ?? 0 })).then((stop) => stops.push(stop));
      }
      if (group.schedule_count === null && auth.can('rules:manage')) {
        void this.#watch(scheduleUrl(group.id), (data) => set({ schedules: normalizeSchedules(data).length })).then((stop) => stops.push(stop));
      }
    }
  }

  get #groupId(): number | null {
    return parseGroupId(router.state.match?.params.id);
  }

  get #tab(): GroupTab {
    return parseTab(router.state.match?.params.tab);
  }

  get #query(): string {
    return router.state.query.get('q') ?? '';
  }

  get #modal(): Modal {
    return parseModal(router.state.query, this.#groupId !== null);
  }

  override willUpdate(): void {
    const id = this.#groupId;
    if (id === this.#detailId) return;
    this.#detailId = id;
    this.#detailStops.forEach((stop) => stop());
    this.#detailStops = [];
    this._group = null;
    this._schedules = [];
    this._notFound = false;
    if (id === null) return;
    const gone = (error: unknown) => {
      if (error instanceof ApiError && error.status === 404 && this.#detailId === id) this._notFound = true;
    };
    void this.#watch(groupUrl(id), (data) => this.#detailId === id && (this._group = normalizeGroup(data)), gone).then((stop) => this.#detailStops.push(stop));
    if (auth.can('rules:manage')) {
      void this.#watch(scheduleUrl(id), (data) => this.#detailId === id && (this._schedules = normalizeSchedules(data))).then((stop) => this.#detailStops.push(stop));
      void this.#watch(ALL_RULES_URL, (data) => (this._allRules = normalizeRules(data))).then((stop) => this.#detailStops.push(stop));
    }
  }

  #registerPalette(): void {
    palette.register(this, [
      ...(auth.can('groups:write')
        ? [{ id: 'groups:create', key: 'n', label: t('groups.palette.create'), icon: 'plus' as const, run: () => this.#openModal({ kind: 'new-group' }) }]
        : []),
      ...(auth.can('rules:manage') && this.#groupId !== null
        ? [{ id: 'groups:add-rule', label: t('groups.palette.addRule'), icon: 'plus' as const, run: () => this.#openModal({ kind: 'new-rule', trigger: null }) }]
        : []),
    ]);
  }

  override updated(): void {
    this.#registerPalette();
  }

  #openModal(modal: Modal): void {
    router.setQuery(modalQuery(modal));
  }

  #closeModal(): void {
    router.setQuery(modalQuery({ kind: 'none' }), { replace: true });
  }

  #display(): Group[] {
    return this._groups.map((group) => {
      const counts = this._counts.get(group.id);
      return {
        ...group,
        manager_count: group.manager_count ?? counts?.managers ?? null,
        schedule_count: group.schedule_count ?? counts?.schedules ?? null,
      };
    });
  }

  #gotoTab(tab: GroupTab): void {
    const id = this.#groupId;
    if (id === null) return;
    const q = this.#query;
    void router.navigate(groupPath(id, tab) + (q ? `?q=${encodeURIComponent(q)}` : ''));
  }

  async #disbandConfirmed(): Promise<void> {
    const group = this._group;
    if (!group) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await groupsApi.disband(group.id);
      toast({ tone: 'ok', title: t('groups.notice.disbanded', { name: group.name }) });
      this._disband = false;
      void router.navigate('/groups');
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #tabs(group: Group): TabItem[] {
    const canRules = auth.can('rules:manage');
    return [
      { id: 'overview', label: t('groups.tabs.overview') },
      { id: 'servers', label: t('groups.tabs.servers'), badge: String(group.servers.length) },
      ...(canRules
        ? [
            { id: 'schedules', label: t('groups.tabs.schedules'), badge: String(this._schedules.length) },
            { id: 'rules', label: t('groups.tabs.rules'), badge: String(group.rules.length) },
          ]
        : []),
    ];
  }

  #conflicts(group: Group): Map<number, RuleConflict> {
    if (!this._groups.some((entry) => entry.has_members) || this._allRules.length === 0) return new Map();
    return findConflicts(group.id, this._groups, this._allRules);
  }

  #renderTab(group: Group, tab: GroupTab, conflicts: Map<number, RuleConflict>) {
    const canWrite = auth.can('groups:write');
    const canRules = auth.can('rules:manage');
    const request = (e: CustomEvent<Modal>) => this.#openModal(e.detail);
    switch (tab) {
      case 'servers':
        return html`<lcm-groups-servers .group=${group} ?can-write=${canWrite && !group.is_system}></lcm-groups-servers>`;
      case 'schedules':
        return canRules
          ? html`<lcm-groups-schedules .group=${group} .schedules=${this._schedules} can-manage @lcm-modal-request=${request}></lcm-groups-schedules>`
          : nothing;
      case 'rules':
        return canRules
          ? html`<lcm-groups-rules .group=${group} .schedules=${this._schedules} .conflicts=${conflicts} can-manage @lcm-modal-request=${request}></lcm-groups-rules>`
          : nothing;
      default:
        return html`<lcm-groups-overview .group=${group} .schedules=${this._schedules} overridden=${overriddenCount(conflicts)} ?can-write=${canWrite && !group.is_system}></lcm-groups-overview>`;
    }
  }

  #explainer(tab: GroupTab) {
    if (tab === 'rules') {
      return html`<lcm-explainer
        topic="group-rules"
        heading=${t('groups.explainer.rules.title')}
        collapse-label=${t('groups.explainer.collapse')}
        expand-label=${t('groups.explainer.expand')}
      >
        <p>${t('groups.explainer.rules.intro')}</p>
        <ul>
          <li><strong>${t('groups.explainer.rules.scheduledTitle')}</strong> ${t('groups.explainer.rules.scheduled')}</li>
          <li>
            <lcm-term heading=${t('groups.term.enforce.title')} definition=${t('groups.term.enforce.definition')}>${t('groups.term.enforce.title')}</lcm-term>:
            ${t('groups.explainer.rules.enforce')}
          </li>
        </ul>
        <p>
          ${t('groups.explainer.rules.priorityBefore')}
          <lcm-term heading=${t('groups.term.priority.title')} definition=${t('groups.term.priority.definition')}>${t('groups.term.priority.title')}</lcm-term>
          ${t('groups.explainer.rules.priorityAfter')}
        </p>
        <p>${t('groups.explainer.rules.order')}</p>
      </lcm-explainer>`;
    }
    return html`<lcm-explainer
      topic="group-schedules"
      heading=${t('groups.explainer.schedules.title')}
      collapse-label=${t('groups.explainer.collapse')}
      expand-label=${t('groups.explainer.expand')}
    >
      <p>${t('groups.explainer.schedules.intro')}</p>
      <ul>
        <li>${t('groups.explainer.schedules.run')}</li>
        <li>${t('groups.explainer.schedules.spread')}</li>
        <li>${t('groups.explainer.schedules.timezone')}</li>
      </ul>
      <p>${t('groups.explainer.schedules.rules')}</p>
    </lcm-explainer>`;
  }

  /** Die Erklärspalte steht unter der Reiterleiste, damit Kopf und Reiter auf allen Reitern gleich breit bleiben. */
  #renderTabBody(group: Group, tab: GroupTab, conflicts: Map<number, RuleConflict>) {
    const explain = auth.can('rules:manage') && (tab === 'rules' || tab === 'schedules');
    if (!explain) return this.#renderTab(group, tab, conflicts);
    return html`<div class="tab-body explain">
      <div class="tab-main">${this.#renderTab(group, tab, conflicts)}</div>
      ${this.#explainer(tab)}
    </div>`;
  }

  #renderDetail(group: Group, tab: GroupTab) {
    const canWrite = auth.can('groups:write') && !group.is_system;
    const conflicts = this.#conflicts(group);
    return html`<div class="detail-pane">
      <div>
        <a class="back" href=${`/groups${this.#query ? `?q=${encodeURIComponent(this.#query)}` : ''}`}>${t('groups.back')}</a>
        <div class="title-row">
          <h2>${group.name}</h2>
          ${group.is_system ? html`<lcm-badge>${t('groups.system')}</lcm-badge>` : nothing}
          ${canWrite
            ? html`<div class="actions">
                <lcm-button variant="outline" icon="edit" @click=${() => this.#openModal({ kind: 'edit-group' })}>${t('groups.action.edit')}</lcm-button>
                <lcm-button variant="danger" icon="trash" @click=${() => (this._disband = true)}>${t('groups.disband.label')}</lcm-button>
              </div>`
            : nothing}
        </div>
        ${group.description ? html`<p class="desc">${group.description}</p>` : nothing}
      </div>
      <lcm-tabs label=${t('groups.tabs.label')} .tabs=${this.#tabs(group)} value=${tab} @lcm-tab-change=${(e: CustomEvent<{ id: string }>) => this.#gotoTab(parseTab(e.detail.id))}>
        ${this.#renderTabBody(group, tab, conflicts)}
      </lcm-tabs>
    </div>`;
  }

  #renderDisbandDialog() {
    const group = this._group;
    const rules = group?.rules.length ?? 0;
    return html`<lcm-dialog
      ?open=${this._disband && group !== null}
      heading=${t('groups.disband.title', { name: group?.name ?? '' })}
      message=${t('groups.disband.message')}
      .impacts=${[
        { label: t('groups.disband.servers'), value: t('groups.disband.serversValue', { count: group?.servers.length ?? 0 }) },
        { label: t('groups.disband.rules'), value: t('groups.disband.rulesValue', { count: rules }), tone: rules > 0 ? 'warn' : 'ok' },
        { label: t('groups.run.reversible'), value: t('groups.run.no'), tone: 'crit' },
      ]}
      expect=${group?.name ?? ''}
      expect-label=${t('groups.disband.expect', { name: group?.name ?? '' })}
      confirm-label=${t('groups.disband.confirm')}
      cancel-label=${t('groups.action.cancel')}
      tone="danger"
      ?busy=${this._busy}
      error=${this._dialogError}
      @lcm-confirm=${this.#disbandConfirmed}
      @lcm-cancel=${() => (this._disband = false)}
    ></lcm-dialog>`;
  }

  #renderModals() {
    const modal = this.#modal;
    const group = this._group;
    const schedule = modal.kind === 'edit-schedule' ? (this._schedules.find((entry) => entry.id === modal.id) ?? null) : null;
    const rule = modal.kind === 'edit-rule' ? (group?.rules.find((entry) => entry.id === modal.id) ?? null) : null;
    const trigger =
      modal.kind === 'new-rule' ? (modal.trigger ?? this._schedules.find((entry) => !entry.is_system)?.id ?? 'enforce') : null;
    return html`
      <lcm-groups-group-modal
        ?open=${modal.kind === 'new-group' || modal.kind === 'edit-group'}
        .group=${modal.kind === 'edit-group' ? group : null}
        @lcm-close=${this.#closeModal}
        @lcm-saved=${(e: CustomEvent<{ id: number }>) => (modal.kind === 'new-group' ? void router.navigate(groupPath(e.detail.id)) : this.#closeModal())}
      ></lcm-groups-group-modal>
      <lcm-groups-schedule-modal
        ?open=${modal.kind === 'new-schedule' || (modal.kind === 'edit-schedule' && schedule !== null)}
        group-id=${group?.id ?? 0}
        .schedule=${schedule}
        @lcm-close=${this.#closeModal}
        @lcm-saved=${this.#closeModal}
      ></lcm-groups-schedule-modal>
      <lcm-groups-rule-modal
        ?open=${(modal.kind === 'new-rule' && group !== null) || (modal.kind === 'edit-rule' && rule !== null)}
        .group=${group}
        .schedules=${this._schedules}
        .rule=${rule}
        .trigger=${trigger}
        @lcm-close=${this.#closeModal}
        @lcm-saved=${this.#closeModal}
      ></lcm-groups-rule-modal>
    `;
  }

  #renderList(groups: Group[], visible: Group[], id: number | null) {
    if (!this._loaded) return html`<lcm-skeleton shape="row" lines="5" label=${t('groups.loading')}></lcm-skeleton>`;
    if (this._failed && groups.length === 0) {
      return html`<lcm-empty-state level="2" icon="warning" heading=${t('groups.loadFailed.title')} text=${t('groups.loadFailed.text')}></lcm-empty-state>`;
    }
    if (groups.length === 0) {
      return html`<lcm-empty-state level="2" icon="users" heading=${t('groups.empty.title')} text=${t('groups.empty.text')}>
        ${auth.can('groups:write')
          ? html`<lcm-button slot="action" variant="primary" icon="plus" @click=${() => this.#openModal({ kind: 'new-group' })}>${t('groups.create')}</lcm-button>`
          : nothing}
      </lcm-empty-state>`;
    }
    if (visible.length === 0) {
      return html`<lcm-empty-state level="2" icon="search" heading=${t('groups.noMatch.title')} text=${t('groups.noMatch.text', { query: this.#query })}></lcm-empty-state>`;
    }
    return html`<lcm-groups-list .groups=${visible} active-id=${id ?? 0} ?compact=${id !== null}></lcm-groups-list>`;
  }

  override render() {
    const id = this.#groupId;
    const tab = this.#tab;
    const groups = this.#display();
    const visible = filterGroups(groups, this.#query);
    const rawId = router.state.match?.params.id;
    return html`
      <div class="head">
        <h1>${t('groups.title')}</h1>
        <lcm-input maxlength=${LIMITS.query}
          icon="search"
          label=${t('groups.search.label')}
          width="full"
          placeholder=${t('groups.search.placeholder')}
          .value=${this.#query}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => router.setQuery({ q: e.detail.value }, { replace: true })}
        ></lcm-input>
        ${auth.can('groups:write') ? html`<lcm-button variant="primary" icon="plus" @click=${() => this.#openModal({ kind: 'new-group' })}>${t('groups.create')}</lcm-button>` : nothing}
      </div>
      <div class=${`layout${id !== null || rawId !== undefined ? ' detail' : ''}`}>
        <div class="list">${this.#renderList(groups, visible, id)}</div>
        ${rawId !== undefined
          ? id === null || this._notFound
            ? html`<lcm-empty-state level="2" icon="search" heading=${t('groups.notFound.title')} text=${t('groups.notFound.text')}>
                <lcm-button slot="action" href="/groups">${t('groups.back')}</lcm-button>
              </lcm-empty-state>`
            : this._group
              ? this.#renderDetail(this._group, tab)
              : html`<lcm-skeleton shape="block" label=${t('groups.loading')}></lcm-skeleton>`
          : nothing}
      </div>
      ${this.#renderDisbandDialog()} ${this.#renderModals()}
    `;
  }
}
customElements.define('lcm-page-groups', GroupsPage);
