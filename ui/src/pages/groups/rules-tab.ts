import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-dialog';
import '../../components/lcm-icon';
import '../../components/lcm-server-action';
import '../../components/lcm-switch';
import '../../components/lcm-term';
import type { Impact } from '../../components/lcm-dialog';
import { baseStyles, emit } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import {
  APT_CACHES_URL,
  CUSTOM_ACTIONS_URL,
  groupsApi,
  inOrder,
  normalizeAptCaches,
  normalizeCustomActions,
  type AptCache,
  type CustomAction,
  type Group,
  type Rule,
  type Schedule,
} from '../../api/groups';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { jobsLink } from '../../lib/job-link';
import { store } from '../../lib/store';
import { cronText } from './cron-describe';
import type { RuleConflict } from './conflicts';
import { ruleSummary } from './rule-summary';
import { actionLabels, runImpacts, runPreview, typeLabel } from './run-support';
import { modalQuery, type Modal } from './url-state';

type ConfirmKind = 'run' | 'delete' | 'enable';
interface Confirm {
  kind: ConfirmKind;
  rule: Rule;
}

/** Der Auslöser einer Gruppe von Regeln: ein Zeitplan oder die Grundsatz-Regeln. */
type TriggerKey = 'enforce' | number;

/**
 * <lcm-groups-rules .group=${…} .schedules=${…} .conflicts=${…} ?can-manage> – die Regeln einer Gruppe je Auslöser
 * in Ausführungsreihenfolge: Umsortieren per Ziehen oder Pfeil, Ausführen, Ein-/Ausschalten, Bearbeiten, Löschen.
 * Meldet `lcm-modal-request` mit dem gewünschten `Modal`.
 */
export class LcmGroupsRules extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    group: { attribute: false },
    schedules: { attribute: false },
    conflicts: { attribute: false },
    canManage: { type: Boolean, attribute: 'can-manage' },
    _confirm: { state: true },
    _busy: { state: true },
    _order: { state: true },
    _dragging: { state: true },
    _over: { state: true },
    _actions: { state: true },
    _caches: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
        min-inline-size: 0;
      }
      ol {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      li.rule {
        display: grid;
        grid-template-columns: auto auto minmax(0, 1fr) auto;
        gap: var(--space-3);
        align-items: center;
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
      }
      li.rule[draggable='true'] {
        cursor: grab;
      }
      li.rule.off .info {
        opacity: 0.6;
      }
      li.rule.over {
        box-shadow: 0 -3px 0 var(--accent);
      }
      .grip {
        display: grid;
        color: var(--text-3);
      }
      .pos {
        display: grid;
        place-items: center;
        inline-size: 1.75rem;
        block-size: 1.75rem;
        border-radius: 50%;
        background: var(--surface-1);
        color: var(--text-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
        font-variant-numeric: tabular-nums;
      }
      .info {
        display: flex;
        flex-direction: column;
        gap: 0.15rem;
        min-inline-size: 0;
      }
      .name {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
        font-weight: var(--fw-semibold);
      }
      .detail {
        color: var(--text-2);
        font-size: var(--fs-s);
        overflow-wrap: anywhere;
      }
      .detail .mono {
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
      }
      .conflict {
        margin: var(--space-1) 0 0;
        padding: var(--space-2) var(--space-3);
        border-radius: var(--radius-s);
        background: var(--warn-soft);
        color: var(--warn-text);
        font-size: var(--fs-s);
      }
      .conflict.win {
        background: var(--info-soft);
        color: var(--info-text);
      }
      .tools {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        justify-content: flex-end;
        gap: var(--space-1);
      }
      .empty {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .cron {
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
      }
      @media (max-width: 48rem) {
        li.rule {
          grid-template-columns: auto minmax(0, 1fr);
        }
        .grip {
          display: none;
        }
        .tools {
          grid-column: 1 / -1;
          justify-content: flex-start;
        }
      }
    `,
  ];

  declare group: Group;
  declare schedules: Schedule[];
  declare conflicts: Map<number, RuleConflict>;
  declare canManage: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _confirm: Confirm | null;
  declare _busy: boolean;
  declare _order: Map<string, number[]>;
  declare _dragging: number | null;
  declare _over: number | null;
  declare _actions: CustomAction[];
  declare _caches: AptCache[];
  #unsubscribe: Array<() => void> = [];

  constructor() {
    super();
    this._dialogError = '';
    this.group = undefined as unknown as Group;
    this.schedules = [];
    this.conflicts = new Map();
    this.canManage = false;
    this._confirm = null;
    this._busy = false;
    this._order = new Map();
    this._dragging = null;
    this._over = null;
    this._actions = [];
    this._caches = [];
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#unsubscribe.push(
      store.subscribe(CUSTOM_ACTIONS_URL, (data) => (this._actions = normalizeCustomActions(data))),
      store.subscribe(APT_CACHES_URL, (data) => (this._caches = normalizeAptCaches(data))),
    );
    void store.get(CUSTOM_ACTIONS_URL).then((d) => (this._actions = normalizeCustomActions(d)), () => {});
    if (auth.can('servers:read')) void store.get(APT_CACHES_URL).then((d) => (this._caches = normalizeAptCaches(d)), () => {});
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#unsubscribe.forEach((stop) => stop());
    this.#unsubscribe = [];
  }

  override willUpdate(changed: Map<string, unknown>): void {
    // Neue Daten vom Server lösen die vorläufige Reihenfolge ab.
    if (changed.has('group') && this._order.size > 0) this._order = new Map();
  }

  #key(trigger: TriggerKey): string {
    return String(trigger);
  }

  /** Die Regeln eines Auslösers in Reihenfolge, mit einer noch nicht bestätigten Umsortierung. */
  #rulesOf(trigger: TriggerKey): Rule[] {
    const rules = this.group.rules.filter((rule) => (trigger === 'enforce' ? rule.enforce : rule.schedule_id === trigger));
    const pending = this._order.get(this.#key(trigger));
    if (!pending) return inOrder(rules);
    return pending.map((id) => rules.find((rule) => rule.id === id)).filter((rule): rule is Rule => rule !== undefined);
  }

  #request(modal: Modal): void {
    emit(this, 'lcm-modal-request', modal);
    void modalQuery;
  }

  async #move(trigger: TriggerKey, from: number, to: number): Promise<void> {
    const rules = this.#rulesOf(trigger);
    if (from === to || to < 0 || to >= rules.length) return;
    const ids = rules.map((rule) => rule.id);
    const [moved] = ids.splice(from, 1);
    ids.splice(to, 0, moved as number);
    this._order = new Map(this._order).set(this.#key(trigger), ids);
    try {
      await groupsApi.reorderRules(this.group.id, ids);
      toast({ tone: 'ok', title: t('rules.notice.reordered') });
    } catch (error) {
      this._order = new Map();
      toast({ tone: 'crit', title: t('rules.notice.reorderFailed'), message: errorText(error) });
    }
  }

  async #toggle(rule: Rule, enabled: boolean): Promise<void> {
    try {
      await groupsApi.setRuleEnabled(rule.id, enabled);
      toast({ tone: 'ok', title: t(enabled ? 'rules.notice.enabled' : 'rules.notice.disabled', { name: rule.name }) });
    } catch (error) {
      toast({ tone: 'crit', title: t('rules.notice.changeFailed'), message: errorText(error) });
    }
  }

  #onSwitch(rule: Rule, enabled: boolean): void {
    if (enabled && rule.enforce) {
      // Aktivieren wirkt bei der nächsten Verbindung auf die Server: erst bestätigen lassen.
      this._confirm = { kind: 'enable', rule };
      return;
    }
    void this.#toggle(rule, enabled);
  }

  async #confirmed(): Promise<void> {
    const confirm = this._confirm;
    if (!confirm) return;
    this._busy = true;
    this._dialogError = '';
    try {
      if (confirm.kind === 'delete') {
        await groupsApi.removeRule(confirm.rule.id);
        toast({ tone: 'ok', title: t('rules.notice.deleted', { name: confirm.rule.name }) });
      } else if (confirm.kind === 'enable') {
        await groupsApi.setRuleEnabled(confirm.rule.id, true);
        toast({ tone: 'ok', title: t('rules.notice.enabled', { name: confirm.rule.name }) });
      } else {
        await groupsApi.triggerRule(confirm.rule.id);
        toast({
          tone: 'info',
          title: t('rules.notice.started', { name: confirm.rule.name }),
          ...jobsLink(t('groups.run.showJobs')),
        });
      }
      this._confirm = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #confirmDialog() {
    const confirm = this._confirm;
    const rule = confirm?.rule;
    const servers = this.group.servers;
    let heading = '';
    let message = '';
    let impacts: Impact[] = [];
    let confirmLabel = '';
    let tone: 'default' | 'danger' = 'default';
    if (rule && confirm) {
      if (confirm.kind === 'run') {
        heading = t('rules.run.title', { name: rule.name });
        message = t('rules.run.message');
        impacts = [...runImpacts([rule], servers), ...this.#conflictImpacts(rule)];
        confirmLabel = t('groups.run.start');
      } else if (confirm.kind === 'enable') {
        heading = t('rules.enable.title', { name: rule.name });
        message = t('rules.enable.message');
        impacts = [
          { label: t('groups.run.servers'), value: String(servers.length) },
          { label: t('rules.enable.when'), value: t('rules.enable.whenValue') },
          { label: t('groups.run.reversible'), value: t('groups.run.yes'), tone: 'ok' },
        ];
        confirmLabel = t('rules.enable.confirm');
      } else {
        heading = t('rules.delete.title', { name: rule.name });
        message = t('rules.delete.message');
        impacts = [
          { label: t('rules.delete.trigger'), value: this.#triggerName(rule) },
          { label: t('rules.delete.effect'), value: t('rules.delete.effectValue') },
          { label: t('groups.run.reversible'), value: t('groups.run.no'), tone: 'crit' },
        ];
        confirmLabel = t('groups.action.delete');
        tone = 'danger';
      }
    }
    return html`<lcm-dialog
      ?open=${confirm !== null}
      heading=${heading}
      message=${message}
      .impacts=${impacts}
      confirm-label=${confirmLabel}
      cancel-label=${t('groups.action.cancel')}
      tone=${tone}
      ?busy=${this._busy}
      error=${this._dialogError}
      @lcm-confirm=${this.#confirmed}
      @lcm-cancel=${() => (this._confirm = null)}
    ></lcm-dialog>`;
  }

  #triggerName(rule: Rule): string {
    if (rule.enforce) return t('rules.trigger.enforce');
    return this.schedules.find((schedule) => schedule.id === rule.schedule_id)?.name ?? '';
  }

  #serverNames(ids: number[]): string {
    return ids.map((id) => this.group.servers.find((server) => server.id === id)?.name ?? `#${id}`).join(', ');
  }

  /** Im Bestätigungsdialog: Auf diesen Servern überspringt der Lauf die Regel, eine andere Gruppe führt. */
  #conflictImpacts(rule: Rule): Impact[] {
    const conflict = this.conflicts.get(rule.id);
    if (!conflict || conflict.overriddenOn.length === 0) return [];
    const groups = conflict.overriddenBy.map((o) => `${o.groupName} (${o.priority})`).join(', ');
    return [{ label: t('rules.conflict.runLabel'), value: t('rules.conflict.overridden', { servers: this.#serverNames(conflict.overriddenOn), groups }), tone: 'warn' }];
  }

  #conflictNote(rule: Rule) {
    const conflict = this.conflicts.get(rule.id);
    if (!conflict) return nothing;
    const others = (list: RuleConflict['overriddenBy']) => list.map((o) => `${o.groupName} (${o.priority})`).join(', ');
    return html`
      ${conflict.overriddenOn.length > 0
        ? html`<p class="conflict" role="note">
            ${t('rules.conflict.overridden', { servers: this.#serverNames(conflict.overriddenOn), groups: others(conflict.overriddenBy) })}
          </p>`
        : nothing}
      ${conflict.winsOn.length > 0
        ? html`<p class="conflict win" role="note">${t('rules.conflict.wins', { servers: this.#serverNames(conflict.winsOn), groups: others(conflict.winsOver) })}</p>`
        : nothing}
      ${conflict.tie ? html`<p class="conflict" role="note">${t('rules.conflict.tie')}</p>` : nothing}
    `;
  }

  #renderRule(trigger: TriggerKey, rule: Rule, index: number, total: number) {
    const context = { customActions: this._actions, aptCaches: this._caches };
    const summary = ruleSummary(rule, context);
    const draggable = this.canManage && total > 1;
    return html`<li
      class=${`rule${rule.enabled ? '' : ' off'}${this._over === rule.id && this._dragging !== rule.id ? ' over' : ''}`}
      draggable=${draggable ? 'true' : 'false'}
      @dragstart=${(e: DragEvent) => {
        this._dragging = rule.id;
        if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
      }}
      @dragover=${(e: DragEvent) => {
        if (this._dragging === null) return;
        e.preventDefault();
        this._over = rule.id;
      }}
      @drop=${(e: DragEvent) => {
        e.preventDefault();
        const rules = this.#rulesOf(trigger);
        const from = rules.findIndex((entry) => entry.id === this._dragging);
        this._dragging = null;
        this._over = null;
        if (from >= 0) void this.#move(trigger, from, index);
      }}
      @dragend=${() => {
        this._dragging = null;
        this._over = null;
      }}
    >
      <span class="grip" aria-hidden="true" title=${draggable ? t('rules.row.dragHint') : ''}>${draggable ? html`<lcm-icon name="columns"></lcm-icon>` : nothing}</span>
      <span class="pos" title=${t('rules.row.position', { number: index + 1 })}>${index + 1}</span>
      <div class="info">
        <span class="name">${rule.name}${rule.is_system ? html`<lcm-badge>${t('groups.system')}</lcm-badge>` : nothing}${rule.enabled ? nothing : html`<lcm-badge tone="warn">${t('rules.row.off')}</lcm-badge>`}</span>
        <span class="detail">${typeLabel(rule.type)}${summary ? html` · <span class=${rule.type === 'script' || rule.type === 'packages' ? 'mono' : ''}>${summary}</span>` : nothing}</span>
        ${this.#conflictNote(rule)}
      </div>
      ${this.canManage ? this.#renderTools(trigger, rule, index, total) : nothing}
    </li>`;
  }

  #renderTools(trigger: TriggerKey, rule: Rule, index: number, total: number) {
    const noServers = this.group.servers.length === 0;
    return html`<div class="tools">
      <lcm-button variant="ghost" size="small" icon="chevron-up" icon-only label=${t('rules.row.moveUp')} ?disabled=${index === 0} @click=${() => this.#move(trigger, index, index - 1)}></lcm-button>
      <lcm-button variant="ghost" size="small" icon="chevron-down" icon-only label=${t('rules.row.moveDown')} ?disabled=${index === total - 1} @click=${() => this.#move(trigger, index, index + 1)}></lcm-button>
      <lcm-server-action
        label=${t('rules.run.label')}
        icon="play"
        variant="ghost"
        ?disabled=${noServers}
        help=${t('rules.run.help')}
        meaning=${t('rules.run.meaning')}
        .labels=${actionLabels()}
        .preview=${runPreview([{ ...rule, enabled: true }], this.group.servers)}
        @click=${() => (this._confirm = { kind: 'run', rule })}
      ></lcm-server-action>
      <lcm-switch
        label=${t('rules.row.active')}
        .checked=${rule.enabled}
        on-label=${t('rules.row.onLabel')}
        off-label=${t('rules.row.offLabel')}
        on-text=${t(rule.enforce ? 'rules.row.onEnforce' : 'rules.row.onScheduled')}
        off-text=${t('rules.row.offText')}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#onSwitch(rule, e.detail.checked)}
      ></lcm-switch>
      ${rule.is_system
        ? nothing
        : html`<lcm-button variant="ghost" size="small" icon="edit" icon-only label=${t('groups.action.edit')} @click=${() => this.#request({ kind: 'edit-rule', id: rule.id })}></lcm-button>
            <lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('groups.action.delete')} @click=${() => (this._confirm = { kind: 'delete', rule })}></lcm-button>`}
    </div>`;
  }

  #renderTrigger(trigger: TriggerKey, heading: string, subtitle: string, schedule?: Schedule) {
    const rules = this.#rulesOf(trigger);
    return html`<lcm-card heading=${heading} subtitle=${subtitle}>
      ${schedule && !schedule.enabled ? html`<lcm-badge slot="actions" tone="warn">${t('schedules.off')}</lcm-badge>` : nothing}
      ${this.canManage
        ? html`<lcm-button slot="actions" variant="outline" size="small" icon="plus" @click=${() => this.#request({ kind: 'new-rule', trigger })}>${t('rules.add')}</lcm-button>`
        : nothing}
      ${rules.length === 0
        ? html`<p class="empty">${t(trigger === 'enforce' ? 'rules.emptyEnforce' : 'rules.emptySchedule')}</p>`
        : html`<ol>${rules.map((rule, index) => this.#renderRule(trigger, rule, index, rules.length))}</ol>`}
    </lcm-card>`;
  }

  override render() {
    if (!this.group) return nothing;
    return html`
      ${this.schedules.map((schedule) =>
        this.#renderTrigger(schedule.id, schedule.name, [schedule.cron_expr, cronText(schedule.cron_expr)].filter(Boolean).join(' · '), schedule),
      )}
      ${this.#renderTrigger('enforce', t('rules.trigger.enforce'), t('rules.trigger.enforceHint'))}
      ${this.#confirmDialog()}
    `;
  }
}
customElements.define('lcm-groups-rules', LcmGroupsRules);
