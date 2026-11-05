import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-combobox';
import '../../components/lcm-dialog';
import '../../components/lcm-notice';
import '../../components/lcm-status';
import '../../components/lcm-term';
import { baseStyles } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import { USERS_URL, groupsApi, normalizeUserOptions, userLabel, type Group, type GroupManager, type Schedule, type UserOption } from '../../api/groups';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { formatDate } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { store } from '../../lib/store';
import { aclStatus } from './group-model';
import { groupPath } from './url-state';

/**
 * <lcm-groups-overview .group=${…} .schedules=${…} overridden="2" ?can-write> – Eckdaten, Verwalter und Inhalt einer Gruppe.
 */
export class LcmGroupsOverview extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    group: { attribute: false },
    schedules: { attribute: false },
    overridden: { type: Number },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _users: { state: true },
    _pick: { state: true },
    _remove: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr));
        gap: var(--gap);
        align-items: start;
      }
      dl {
        display: grid;
        grid-template-columns: max-content 1fr;
        gap: var(--space-3) var(--space-5);
        margin: 0;
      }
      dt {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      dd {
        margin: 0;
      }
      .big {
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
        font-variant-numeric: tabular-nums;
      }
      .sub {
        margin: var(--space-1) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      ul {
        display: flex;
        flex-direction: column;
        gap: var(--space-1);
        margin: 0 0 var(--space-4);
        padding: 0;
        list-style: none;
      }
      li.person {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
        padding: var(--space-1) 0;
      }
      .mail {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .add {
        display: flex;
        align-items: end;
        gap: var(--space-3);
      }
      .links {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .links a {
        display: flex;
        justify-content: space-between;
        padding: var(--space-3) var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-2);
        color: var(--text-1);
        text-decoration: none;
      }
      .links a:hover {
        background: var(--surface-3);
      }
      .links strong {
        font-variant-numeric: tabular-nums;
      }
      lcm-notice {
        margin-block-start: var(--space-3);
      }
      .note {
        margin: 0 0 var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare group: Group;
  declare schedules: Schedule[];
  declare overridden: number;
  declare canWrite: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _users: UserOption[];
  declare _pick: string;
  declare _remove: GroupManager | null;
  declare _busy: boolean;
  #stop: (() => void) | undefined;

  constructor() {
    super();
    this._dialogError = '';
    this.group = undefined as unknown as Group;
    this.schedules = [];
    this.overridden = 0;
    this.canWrite = false;
    this._users = [];
    this._pick = '';
    this._remove = null;
    this._busy = false;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    if (!auth.can('users:read')) return;
    const apply = (data: unknown) => (this._users = normalizeUserOptions(data));
    this.#stop = store.subscribe(USERS_URL, apply);
    void store.get(USERS_URL).then(apply, () => {});
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stop?.();
  }

  async #assign(user: UserOption): Promise<void> {
    this._busy = true;
    try {
      await groupsApi.assignManager(this.group.id, user.id);
      toast({ tone: 'ok', title: t('groups.managers.added', { name: user.username }) });
      this._pick = '';
    } catch (error) {
      toast({ tone: 'crit', title: t('groups.managers.failed'), message: errorText(error) });
    } finally {
      this._busy = false;
    }
  }

  async #removed(): Promise<void> {
    const manager = this._remove;
    if (!manager) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await groupsApi.removeManager(this.group.id, manager.id);
      toast({ tone: 'ok', title: t('groups.managers.removed', { name: manager.username }) });
      this._remove = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #facts() {
    const group = this.group;
    const acl = aclStatus(group);
    return html`<lcm-card heading=${t('groups.overview.facts')}>
      <dl>
        <dt><lcm-term heading=${t('groups.term.priority.title')} definition=${t('groups.term.priority.definition')}>${t('groups.overview.priority')}</lcm-term></dt>
        <dd><span class="big">${group.priority}</span><p class="sub">${t('groups.overview.priorityHint')}</p></dd>
        <dt><lcm-term heading=${t('groups.term.acl.title')} definition=${t('groups.term.acl.definition')}>${t('groups.overview.acl')}</lcm-term></dt>
        <dd>${acl ? html`<lcm-status level=${acl.level} label=${t(acl.key, acl.params)}></lcm-status><p class="sub">${t('groups.overview.aclHint')}</p>` : html`<span class="sub">${t('groups.overview.aclNone')}</span>`}</dd>
        ${group.description ? html`<dt>${t('groups.overview.description')}</dt><dd>${group.description}</dd>` : nothing}
        <dt>${t('groups.overview.created')}</dt>
        <dd>${formatDate(group.created_at)}</dd>
      </dl>
    </lcm-card>`;
  }

  #managers() {
    const group = this.group;
    const available = this._users.filter((user) => !group.managers.some((manager) => manager.id === user.id));
    const picked = available.find((user) => String(user.id) === this._pick);
    const canAssign = this.canWrite && auth.can('users:read');
    return html`<lcm-card heading=${t('groups.managers.title')} subtitle=${t('groups.managers.subtitle')}>
      ${group.managers.length === 0
        ? html`<p class="note">${t('groups.managers.none')}</p>`
        : html`<ul>
            ${group.managers.map(
              (manager) => html`<li class="person">
                <span>${userLabel(manager)}<br /><span class="mail">${manager.email}</span></span>
                ${this.canWrite
                  ? html`<lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('groups.managers.remove', { name: manager.username })} @click=${() => (this._remove = manager)}></lcm-button>`
                  : nothing}
              </li>`,
            )}
          </ul>`}
      ${canAssign
        ? html`<div class="add">
            <lcm-combobox
              label=${t('groups.managers.add')}
              width="l"
              placeholder=${t('groups.managers.addPlaceholder')}
              empty-text=${t('groups.managers.noneLeft')}
              .options=${available.map((user) => ({ value: String(user.id), label: userLabel(user) }))}
              .value=${this._pick}
              @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._pick = e.detail.value)}
            ></lcm-combobox>
            <lcm-button variant="primary" icon="plus" ?busy=${this._busy} ?disabled=${!picked} @click=${() => picked && this.#assign(picked)}>${t('groups.action.add')}</lcm-button>
          </div>`
        : nothing}
    </lcm-card>`;
  }

  #content() {
    const group = this.group;
    const enforce = group.rules.filter((rule) => rule.enforce).length;
    const link = (tab: 'servers' | 'schedules' | 'rules', label: string, value: string) =>
      html`<a href=${groupPath(group.id, tab)}><span>${label}</span><strong>${value}</strong></a>`;
    return html`<lcm-card heading=${t('groups.overview.content')}>
      <div class="links">
        ${link('servers', t('groups.tabs.servers'), String(group.servers.length))}
        ${auth.can('rules:manage')
          ? html`${link('schedules', t('groups.tabs.schedules'), String(this.schedules.length))}
            ${link('rules', t('groups.tabs.rules'), t('groups.overview.ruleCount', { count: group.rules.length, enforce }))}`
          : nothing}
      </div>
      ${this.overridden > 0 ? html`<lcm-notice tone="warn">${t('groups.overview.overridden', { count: this.overridden })}</lcm-notice>` : nothing}
    </lcm-card>`;
  }

  override render() {
    if (!this.group) return nothing;
    return html`${this.#facts()} ${this.#content()} ${this.#managers()}
      <lcm-dialog
        ?open=${this._remove !== null}
        heading=${t('groups.managers.removeTitle', { name: this._remove?.username ?? '' })}
        message=${t('groups.managers.removeMessage')}
        .impacts=${[
          { label: t('groups.managers.impactSees'), value: t('groups.managers.impactSeesValue', { group: this.group.name }) },
          { label: t('groups.run.reversible'), value: t('groups.run.yes'), tone: 'ok' },
        ]}
        confirm-label=${t('groups.action.remove')}
        cancel-label=${t('groups.action.cancel')}
        tone="danger"
        ?busy=${this._busy}
        error=${this._dialogError}
        @lcm-confirm=${this.#removed}
        @lcm-cancel=${() => (this._remove = null)}
      ></lcm-dialog>`;
  }
}
customElements.define('lcm-groups-overview', LcmGroupsOverview);
