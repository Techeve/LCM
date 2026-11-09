import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-empty-state';
import type { Column } from '../../components/lcm-data-table';
import { baseStyles, emit, utilStyles } from '../../components/shared';
import type { LinuxUser, Profile } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import type { TableView } from './lu-paging';
import { pagedTable, plainLabels } from './lu-table';
import { userStatus, type UserStatus } from './user-model';

const STATUS_TONE: Record<UserStatus, 'neutral' | 'ok' | 'info'> = { inactive: 'neutral', password: 'ok', keys: 'info' };
const STATUS_LABEL: Record<UserStatus, string> = {
  inactive: 'linuxUsers.users.status.inactive',
  password: 'linuxUsers.users.status.password',
  keys: 'linuxUsers.users.status.keys',
};

/**
 * <lcm-lu-users .users=${…} .profiles=${…} active-id="3"> – Tabelle der Linux-Benutzer. Meldet `lcm-open-row`
 * mit { id }, wenn eine Zeile gewählt wird.
 */
export class LcmLuUsers extends LitElement {
  static override properties = {
    users: { attribute: false },
    profiles: { attribute: false },
    activeId: { type: String, attribute: 'active-id' },
    view: { attribute: false },
  };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .access {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-1) var(--space-2);
      }
      .sub {
        display: block;
        color: var(--text-2);
        font-size: var(--fs-xs);
      }
    `,
  ];

  declare users: LinuxUser[];
  declare profiles: Profile[];
  declare activeId: string;
  declare view: TableView;

  constructor() {
    super();
    this.users = [];
    this.profiles = [];
    this.activeId = '';
    this.view = { page: 1, sort: null };
    new I18nController(this);
  }

  #profile(id: number | null): Profile | undefined {
    return this.profiles.find((p) => p.id === id);
  }

  #access(user: LinuxUser) {
    if (user.servers.length === 0 && user.groups.length === 0) return html`<span class="muted">${t('linuxUsers.users.nowhere')}</span>`;
    return html`<span class="access">
      ${user.servers.length > 0 ? html`<span>${t('linuxUsers.users.servers', { count: user.servers.length })}</span>` : nothing}
      ${user.groups.map((g) => html`<lcm-badge tone="accent">${g.name}</lcm-badge>`)}
    </span>`;
  }

  get #columns(): Column<LinuxUser>[] {
    return [
      { key: 'username', label: t('linuxUsers.users.column.username'), sortable: true, kind: 'mono', render: (r) => html`<strong>${r.username}</strong>` },
      {
        key: 'full_name',
        label: t('linuxUsers.users.column.name'),
        sortable: true,
        render: (r) => html`${r.full_name || html`<span class="muted">–</span>`}${r.email ? html`<span class="sub">${r.email}</span>` : nothing}`,
      },
      {
        key: 'profile',
        label: t('linuxUsers.users.column.profile'),
        value: (r) => this.#profile(r.default_profile_id)?.name ?? '',
        sortable: true,
        render: (r) => {
          const profile = this.#profile(r.default_profile_id);
          return profile ? html`<lcm-badge tone=${profile.grants_full_root ? 'warn' : 'neutral'}>${profile.name}</lcm-badge>` : html`<span class="muted">–</span>`;
        },
      },
      { key: 'keys', label: t('linuxUsers.users.column.keys'), align: 'end', sortable: true, value: (r) => r.ssh_keys.length, render: (r) => String(r.ssh_keys.length) },
      { key: 'access', label: t('linuxUsers.users.column.access'), value: (r) => r.servers.length + r.groups.length, sortable: true, render: (r) => this.#access(r) },
      {
        key: 'status',
        label: t('linuxUsers.users.column.status'),
        value: (r) => userStatus(r),
        sortable: true,
        render: (r) => {
          const status = userStatus(r);
          return html`<lcm-badge tone=${STATUS_TONE[status]}>${t(STATUS_LABEL[status])}</lcm-badge>`;
        },
      },
    ];
  }

  override render() {
    return pagedTable({
      items: this.users,
      columns: this.#columns,
      labels: plainLabels(t('linuxUsers.users.caption'), t('linuxUsers.users.empty.title')),
      view: this.view,
      activeId: this.activeId,
      onActivate: (id) => emit(this, 'lcm-open-row', { id }),
    });
  }
}
customElements.define('lcm-lu-users', LcmLuUsers);
