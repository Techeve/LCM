import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import type { Column } from '../../components/lcm-data-table';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { LINUX_USERS_URL, normalizeUsers, type LinuxUser } from '../../api/linux-users';
import { refreshServer, serverSummaryUrl } from '../../api/servers';
import {
  assignableUsers,
  loginKind,
  normaliseOpsUsers,
  normalisePending,
  normaliseServerFlags,
  opsApi,
  opsUsersUrl,
  pendingSyncsUrl,
  userActions,
  type LoginKind,
  type OpsServerFlags,
  type OpsUser,
  type PendingSync,
} from '../../api/servers-ops';
import { auth } from '../../lib/auth';
import { SubscribeController } from '../../lib/controllers';
import { formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import '../security/sec-cell';
import './ops-user-logins';
import type { ActionPreviewData } from '../../api/servers';
import { effectImpacts } from './preview';
import './preview-panel';
import './set-action';
import './srv-action';
import { opsStyles } from './ops-styles';
import { tabStyles } from './srv-styles';
import { SrvTab, plainLabels, whenLoaded } from './srv-tab';

const LOGIN_KIND: Record<LoginKind, { key: string; tone: 'warn' | 'ok' | 'neutral' }> = {
  password: { key: 'serverOps.users.login.password', tone: 'warn' },
  keyOnly: { key: 'serverOps.users.login.keyOnly', tone: 'ok' },
  unknown: { key: 'serverOps.users.login.unknown', tone: 'neutral' },
  none: { key: 'serverOps.users.login.none', tone: 'neutral' },
};

/**
 * Benutzer: die anmeldefähigen Linux-Konten dieses Servers. Neu einlesen, sperren/entsperren, entfernen (nur
 * unverwaltete Konten), Anmeldehistorie (`?logins=<name>`), LCM-Benutzer zuordnen und ausstehende Abgleiche.
 * Die Konto-Aktionen wirken sofort (kein Job); das Backend liest den Bestand danach neu.
 */
export class SrvUsers extends SrvTab {
  static override properties = {
    _dialogError: { state: true },
    ...SrvTab.properties,
    _assign: { state: true },
    _assignBusy: { state: true },
    _remove: { state: true },
    _removeBusy: { state: true },
    _removePreview: { state: true },
  };
  static override styles = [
    baseStyles,
    tabStyles,
    opsStyles,
    css`
      .assign {
        display: flex;
        flex-wrap: wrap;
        align-items: end;
        gap: var(--space-3);
      }
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _assign: string;
  declare _assignBusy: boolean;
  declare _remove: OpsUser | null;
  declare _removeBusy: boolean;
  declare _removePreview: ActionPreviewData | undefined;

  readonly #users = new LiveData<OpsUser[]>(this, { url: () => opsUsersUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseOpsUsers });
  readonly #pending = new LiveData<PendingSync[]>(this, { url: () => pendingSyncsUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normalisePending });
  readonly #flags = new LiveData<OpsServerFlags>(this, { url: () => serverSummaryUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseServerFlags });
  readonly #linuxUsers = new LiveData<LinuxUser[]>(this, { url: () => (this.canWrite && auth.can('linuxusers:read') ? LINUX_USERS_URL : null), transform: normalizeUsers });

  constructor() {
    super();
    this._dialogError = '';
    this._assign = '';
    this._assignBusy = false;
    this._remove = null;
    this._removeBusy = false;
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  get #loginsFor(): string {
    return router.state.query.get('logins') ?? '';
  }

  // ---- Aktionen ----

  #toggleAction(user: OpsUser): TemplateResult {
    const s = this.server;
    const locked = user.disabled || user.blocked;
    const name = user.username;
    if (locked) {
      return html`<lcm-set-action
        variant="outline"
        icon="key"
        server-id=${s.id}
        server-name=${s.name}
        action="user-unlock"
        .params=${{ username: name }}
        label=${t('serverOps.users.enable.label')}
        help=${t('serverOps.users.enable.help', { name })}
        heading=${t('serverOps.users.enable.heading', { name })}
        done=${t('serverOps.users.enable.done', { name })}
        .run=${async () => void (await opsApi.userEnable(s.id, name))}
      ></lcm-set-action>`;
    }
    return html`<lcm-set-action
      variant="outline"
      icon="lock"
      server-id=${s.id}
      server-name=${s.name}
      action="user-lock"
      .params=${{ username: name }}
      label=${t('serverOps.users.disable.label')}
      help=${user.managed ? t('serverOps.users.disable.helpManaged', { name }) : t('serverOps.users.disable.help', { name })}
      heading=${t('serverOps.users.disable.heading', { name })}
      done=${t('serverOps.users.disable.done', { name })}
      .run=${async () => void (await opsApi.userDisable(s.id, name))}
    ></lcm-set-action>`;
  }

  #actionCell(user: OpsUser): TemplateResult | string {
    const can = userActions(user, this.server.service_user);
    if (!this.canWrite || (!can.toggle && !can.remove)) return '';
    return html`${can.toggle ? this.#toggleAction(user) : nothing}
    ${can.remove ? html`<lcm-button size="small" variant="ghost" icon="trash" icon-only label=${t('serverOps.users.remove.label', { name: user.username })} @click=${() => {
              this._removePreview = undefined;
              this._remove = user;
            }}></lcm-button>` : nothing}`;
  }

  async #removeNow(user: OpsUser): Promise<void> {
    this._removeBusy = true;
    this._dialogError = '';
    try {
      await opsApi.userRemove(this.server.id, user.username);
      toast({ tone: 'ok', title: t('serverOps.users.remove.done', { name: user.username }) });
      this._remove = null;
      refreshServer(this.server.id);
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._removeBusy = false;
    }
  }

  async #assignNow(): Promise<void> {
    const id = Number(this._assign);
    if (!id) return;
    this._assignBusy = true;
    try {
      await opsApi.assignUser(this.server.id, id);
      toast({ tone: 'ok', title: t('serverOps.users.assign.done') });
      this._assign = '';
      refreshServer(this.server.id);
      this.#linuxUsers.reload();
    } catch (error) {
      toast({ tone: 'crit', title: t('serverOps.users.assign.failed'), message: errorText(error) });
    } finally {
      this._assignBusy = false;
    }
  }

  // ---- Darstellung ----

  #nameCell(user: OpsUser): TemplateResult {
    return html`<lcm-sec-cell mono strong>${user.username}</lcm-sec-cell>
        ${user.managed ? html`<lcm-badge tone="info">${t('serverOps.users.managed')}</lcm-badge>` : nothing}
        ${user.username === this.server.service_user ? html`<lcm-badge>${t('serverOps.users.serviceUser')}</lcm-badge>` : nothing}
        ${user.disabled ? html`<lcm-badge tone="crit">${t('serverOps.users.disabled')}</lcm-badge>` : nothing}
      ${user.blocked ? html`<lcm-badge tone="warn">${t('serverOps.users.blocked')}</lcm-badge>` : nothing}
      <lcm-sec-cell block muted>${t('serverOps.users.uid', { uid: user.uid })} · ${user.shell}</lcm-sec-cell>`;
  }

  #lastLoginCell(user: OpsUser): TemplateResult {
    return html`${user.lastLogin ? formatRelative(user.lastLogin) : t('serverOps.users.never')}${user.loginsFromLcm ? html` <lcm-badge>${t('serverOps.users.fromLcm')}</lcm-badge>` : nothing}
    ${user.loginCount > 0
      ? html`<lcm-sec-cell block><lcm-button size="small" variant="ghost" icon="clock" @click=${() => router.setQuery({ logins: user.username })}>${t('serverOps.users.logins.show', { count: user.loginCount })}</lcm-button></lcm-sec-cell>`
      : nothing}`;
  }

  get #columns(): Column<OpsUser>[] {
    return [
      { key: 'username', label: t('serverOps.users.column.name'), sortable: true, render: (r) => this.#nameCell(r) },
      { key: 'login', label: t('serverOps.users.column.login'), render: (r) => html`<lcm-badge tone=${LOGIN_KIND[loginKind(r)].tone}>${t(LOGIN_KIND[loginKind(r)].key)}</lcm-badge>` },
      { key: 'sshKeys', label: t('serverOps.users.column.keys'), align: 'end' },
      { key: 'twoFactor', label: t('serverOps.users.column.twoFactor'), render: (r) => (r.twoFactor ? html`<lcm-badge tone="ok">${t('serverOps.users.twoFactorYes')}</lcm-badge>` : '–') },
      { key: 'lastLogin', label: t('serverOps.users.column.lastLogin'), sortable: true, sortValue: (r) => r.lastLogin ?? '', render: (r) => this.#lastLoginCell(r) },
      ...(this.canWrite ? [{ key: 'actions', label: t('serverOps.users.column.actions'), render: (r: OpsUser) => this.#actionCell(r) }] : []),
    ];
  }

  #renderToolbar(): TemplateResult {
    const s = this.server;
    return html`<div class="toolbar">
      ${this.#flags.data?.ssh2fa ? html`<lcm-badge tone="info">${t('serverOps.users.ssh2fa')}</lcm-badge>` : nothing}
      <span class="grow"></span>
      <lcm-button variant="ghost" icon="book" href="/docs/linux-benutzer">${t('serverOps.users.guide')}</lcm-button>
      ${this.canWrite
        ? html`<lcm-srv-action
            variant="outline"
            icon="refresh"
            no-confirm
            server-id=${s.id}
            server-name=${s.name}
            label=${t('serverOps.users.refresh.label')}
            help=${t('serverOps.users.refresh.help')}
            done=${t('serverOps.users.refresh.done')}
            .run=${async () => void (await opsApi.usersRefresh(s.id))}
          ></lcm-srv-action>`
        : nothing}
    </div>`;
  }

  #renderPending(): TemplateResult | typeof nothing {
    const pending = this.#pending.data ?? [];
    if (pending.length === 0) return nothing;
    return html`<lcm-notice tone="warn" heading=${t('serverOps.users.pending.title', { count: pending.length })}>
      ${t('serverOps.users.pending.hint')}
      <ul>
        ${pending.map((p) => html`<li>${p.username ? t('serverOps.users.pending.remove', { name: p.username }) : t('serverOps.users.pending.distribute')}${p.lastError ? html` · ${p.lastError}` : nothing}</li>`)}
      </ul>
    </lcm-notice>`;
  }

  #renderAssign(users: OpsUser[]): TemplateResult | typeof nothing {
    const linux = this.#linuxUsers.data;
    if (!this.canWrite || !linux) return nothing;
    const choices = assignableUsers(linux, users);
    if (choices.length === 0) return nothing;
    const options = [{ value: '', label: t('serverOps.users.assign.placeholder') }, ...choices.map((c) => ({ value: String(c.id), label: c.fullName ? `${c.username} – ${c.fullName}` : c.username }))];
    return html`<lcm-card heading=${t('serverOps.users.assign.title')}>
      <div class="assign">
        <lcm-select width="l" label=${t('serverOps.users.assign.label')} .options=${options} .value=${this._assign} @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._assign = e.detail.value)}></lcm-select>
        <lcm-button variant="primary" icon="plus" ?disabled=${!this._assign} ?busy=${this._assignBusy} @click=${() => void this.#assignNow()}>${t('serverOps.users.assign.button')}</lcm-button>
      </div>
    </lcm-card>`;
  }

  #removeImpacts(user: OpsUser): Impact[] {
    return [{ label: t('servers.confirm.server'), value: this.server.name }, { label: t('serverOps.users.remove.account'), value: user.username }, ...effectImpacts(this._removePreview)];
  }

  #renderRemove(): TemplateResult | typeof nothing {
    const user = this._remove;
    if (!this.canWrite) return nothing;
    return html`<lcm-dialog
      ?open=${user !== null}
      tone="danger"
      heading=${user ? t('serverOps.users.remove.heading', { name: user.username }) : ''}
      confirm-label=${t('serverOps.users.remove.confirm')}
      cancel-label=${t('servers.confirm.cancel')}
      expect=${user?.username ?? ''}
      expect-label=${t('serverOps.users.remove.expect', { name: user?.username ?? '' })}
      .impacts=${user ? this.#removeImpacts(user) : []}
      error=${this._dialogError}
      ?busy=${this._removeBusy}
      @lcm-cancel=${() => (this._remove = null)}
      @lcm-confirm=${() => user && void this.#removeNow(user)}
    >
      ${user
        ? html`<lcm-preview-panel
            server-id=${this.server.id}
            action="user-remove"
            .params=${{ username: user.username }}
            @lcm-preview=${(e: CustomEvent<ActionPreviewData>) => (this._removePreview = e.detail)}
          ></lcm-preview-panel>`
        : nothing}
    </lcm-dialog>`;
  }

  #render(users: OpsUser[]): TemplateResult {
    const loginsFor = this.#loginsFor;
    const fromLcm = users.find((u) => u.username === loginsFor)?.loginsFromLcm ?? false;
    return html`${this.#renderToolbar()}
      <div class="stack">
        ${this.#renderPending()} ${this.#renderAssign(users)}
        ${users.length === 0
          ? html`<lcm-empty-state level="3" icon="users" heading=${t('serverOps.users.empty.title')} text=${t('serverOps.users.empty.text')}></lcm-empty-state>`
          : html`<lcm-data-table no-toolbar density="compact" .columns=${this.#columns} .rows=${users} .labels=${plainLabels(t('serverOps.users.caption'), t('serverOps.users.empty.title'))}></lcm-data-table>`}
      </div>
      <lcm-ops-user-logins server-id=${this.server.id} username=${loginsFor} ?from-lcm=${fromLcm} @lcm-close=${() => router.setQuery({ logins: null }, { replace: true })}></lcm-ops-user-logins>
      ${this.#renderRemove()}`;
  }

  override render() {
    return whenLoaded(this.#users, (users) => this.#render(users));
  }
}
customElements.define('lcm-srv-users', SrvUsers);
