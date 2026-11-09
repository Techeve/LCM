import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-dialog';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import type { Option } from '../../components/lcm-select';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import type { Group } from '../../api/groups';
import { assignmentsUrl, linuxUsersApi, normalizeAssignments, type GroupAssignment, type LinuxUser, type Profile } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { buildImpacts, serverNames } from './impacts';
import { sectionStyles } from './lu-styles';
import './lu-action';
import { aclShortfall, groupServers, userServers } from './user-model';

interface Change {
  groupId: number;
  /** ID des neuen Profils als Text; leer = Standardprofil des Benutzers. */
  profileId: string;
}

/**
 * <lcm-lu-user-access .user=${…} .profiles=${…} .groups=${…} can-write can-sync> – wo ein Benutzer eingerichtet ist: Gruppen mit
 * je eigenem Profil, direkt zugeordnete Server, Abgleich auf einen Schlag. Jede Änderung wirkt sofort auf die Server
 * und fragt deshalb mit den betroffenen Servern nach.
 */
export class LcmLuUserAccess extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    user: { attribute: false },
    profiles: { attribute: false },
    groups: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    canSync: { type: Boolean, attribute: 'can-sync' },
    _assignGroup: { state: true },
    _assignProfile: { state: true },
    _change: { state: true },
  };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .assign {
        display: grid;
        grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
        gap: var(--space-3);
        align-items: end;
      }
      @media (max-width: 40rem) {
        .assign {
          grid-template-columns: minmax(0, 1fr);
        }
      }
      .group-profile {
        flex: 1 1 12rem;
        min-inline-size: 0;
      }
      .server-name {
        font-weight: var(--fw-semibold);
      }
    `,
  ];

  declare user: LinuxUser;
  declare profiles: Profile[];
  declare groups: Group[];
  declare canWrite: boolean;
  declare canSync: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _assignGroup: string;
  declare _assignProfile: string;
  declare _change: Change | null;

  readonly #assignments = new LiveData<GroupAssignment[]>(this, {
    url: () => (this.user ? assignmentsUrl(this.user.id) : null),
    transform: normalizeAssignments,
  });

  constructor() {
    super();
    this._dialogError = '';
    this.profiles = [];
    this.groups = [];
    this.canWrite = false;
    this.canSync = false;
    this._assignGroup = '';
    this._assignProfile = '';
    this._change = null;
    new I18nController(this);
  }

  #group(id: number): Group | undefined {
    return this.groups.find((g) => g.id === id);
  }

  #profileOf(groupId: number): string {
    const id = this.#assignments.data?.find((a) => a.group_id === groupId)?.profile_id;
    return id ? String(id) : '';
  }

  get #profileOptions(): Option[] {
    return [{ value: '', label: t('linuxUsers.access.defaultProfile') }, ...this.profiles.map((p) => ({ value: String(p.id), label: p.name }))];
  }

  #profileName(id: string): Profile | undefined {
    return this.profiles.find((p) => String(p.id) === id);
  }

  /** Warnung, wenn die Pfadregeln des Profils auf Servern der Gruppe wirkungslos blieben. */
  #aclNote(groupId: number, profileId: string) {
    const shortfall = aclShortfall(this.#profileName(profileId || String(this.user.default_profile_id ?? '')), this.#group(groupId));
    return shortfall ? html`<lcm-notice tone="warn">${t('linuxUsers.access.aclIncomplete', { ok: shortfall.capable, total: shortfall.total })}</lcm-notice>` : nothing;
  }

  async #confirmChange(): Promise<void> {
    const change = this._change;
    if (!change) return;
    this._dialogError = '';
    try {
      await linuxUsersApi.setGroupProfile(this.user.id, change.groupId, change.profileId ? Number(change.profileId) : null);
      toast({ tone: 'ok', title: t('linuxUsers.access.profileSet') });
      this._change = null;
    } catch (error) {
      this._dialogError = errorText(error);
    }
  }

  async #sync(): Promise<void> {
    const failed: string[] = [];
    for (const server of userServers(this.user, this.groups).filter((s) => !s.user_sync_disabled)) {
      try {
        await linuxUsersApi.syncServer(server.id);
      } catch {
        failed.push(server.name);
      }
    }
    if (failed.length > 0) throw new Error(t('linuxUsers.access.sync.partial', { names: failed.join(', ') }));
  }

  async #assign(): Promise<void> {
    await linuxUsersApi.assignGroup(this.user.id, Number(this._assignGroup), this._assignProfile ? Number(this._assignProfile) : null);
    this._assignGroup = this._assignProfile = '';
  }

  #groupRow(ref: LinuxUser['groups'][number]) {
    const group = this.#group(ref.id);
    const servers = groupServers(group);
    const profile = this.#profileOf(ref.id);
    const note = this.#aclNote(ref.id, profile);
    return html`<li>
        <span class="name grow">${ref.name} <lcm-badge>${t('linuxUsers.access.servers', { count: group?.server_count ?? servers.length })}</lcm-badge></span>
        <lcm-select
          class="group-profile"
          label=${t('linuxUsers.access.profile')}
          width="full"
          .options=${this.#profileOptions}
          .value=${profile}
          ?disabled=${!this.canWrite}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._change = { groupId: ref.id, profileId: e.detail.value })}
        ></lcm-select>
        ${this.canWrite
          ? html`<lcm-lu-action
              label=${t('linuxUsers.access.remove.label')}
              help=${t('linuxUsers.access.remove.help')}
              meaning=${t('linuxUsers.access.remove.meaning')}
              icon="x"
              variant="danger"
              danger
              confirm-heading=${t('linuxUsers.access.remove.heading', { group: ref.name })}
              confirm-label=${t('linuxUsers.access.remove.confirm')}
              done=${t('linuxUsers.access.remove.done')}
              .impacts=${buildImpacts({ servers, effect: t('linuxUsers.access.remove.effect'), reversible: false })}
              .run=${() => linuxUsersApi.removeGroup(this.user.id, ref.id)}
            ></lcm-lu-action>`
          : nothing}
      </li>
      ${note === nothing ? nothing : html`<li>${note}</li>`}`;
  }

  #assignForm() {
    const taken = new Set(this.user.groups.map((g) => g.id));
    const options: Option[] = [
      { value: '', label: t('linuxUsers.access.chooseGroup') },
      ...this.groups.filter((g) => !g.is_system && !taken.has(g.id)).map((g) => ({ value: String(g.id), label: g.name })),
    ];
    const group = this.#group(Number(this._assignGroup));
    const servers = groupServers(group);
    return html`<div class="assign">
        <lcm-select
          label=${t('linuxUsers.access.group')}
          width="full"
          .options=${options}
          .value=${this._assignGroup}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._assignGroup = e.detail.value)}
        ></lcm-select>
        <lcm-select
          label=${t('linuxUsers.access.profile')}
          width="full"
          .options=${this.#profileOptions}
          .value=${this._assignProfile}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._assignProfile = e.detail.value)}
        ></lcm-select>
        <lcm-lu-action
          label=${t('linuxUsers.access.assign.label')}
          help=${t('linuxUsers.access.assign.help')}
          meaning=${t('linuxUsers.access.assign.meaning')}
          icon="plus"
          variant="primary"
          ?disabled=${!this._assignGroup}
          confirm-heading=${t('linuxUsers.access.assign.heading', { group: group?.name ?? '' })}
          confirm-label=${t('linuxUsers.access.assign.confirm')}
          done=${t('linuxUsers.access.assign.done')}
          .impacts=${buildImpacts({ servers, effect: t('linuxUsers.access.assign.effect'), reversible: true })}
          .run=${() => this.#assign()}
        ></lcm-lu-action>
      </div>
      ${this._assignGroup ? this.#aclNote(Number(this._assignGroup), this._assignProfile) : nothing}`;
  }

  #changeDialog() {
    const change = this._change;
    const group = change ? this.#group(change.groupId) : undefined;
    const profile = change ? this.#profileName(change.profileId) : undefined;
    return html`<lcm-dialog
      ?open=${change !== null}
      heading=${t('linuxUsers.access.change.heading', { group: group?.name ?? '' })}
      message=${t('linuxUsers.access.change.message', { profile: profile?.name ?? t('linuxUsers.access.defaultProfile') })}
      confirm-label=${t('linuxUsers.access.change.confirm')}
      cancel-label=${t('linuxUsers.action.cancel')}
      .impacts=${buildImpacts({ servers: groupServers(group), effect: t('linuxUsers.access.change.effect'), reversible: true })}
      @lcm-cancel=${() => (this._change = null)}
      error=${this._dialogError}
      @lcm-confirm=${() => void this.#confirmChange()}
    ></lcm-dialog>`;
  }

  #syncAction(servers: ReturnType<typeof userServers>) {
    return html`<lcm-lu-action
      label=${t('linuxUsers.access.sync.label')}
      help=${t('linuxUsers.access.sync.help')}
      meaning=${t('linuxUsers.access.sync.meaning')}
      icon="refresh"
      ?disabled=${servers.filter((s) => !s.user_sync_disabled).length === 0}
      confirm-heading=${t('linuxUsers.access.sync.heading', { name: this.user.username })}
      confirm-label=${t('linuxUsers.access.sync.confirm')}
      done=${t('linuxUsers.access.sync.done')}
      .impacts=${buildImpacts({ servers, effect: t('linuxUsers.access.sync.effect'), reversible: true })}
      .run=${() => this.#sync()}
    ></lcm-lu-action>`;
  }

  override render() {
    const servers = userServers(this.user, this.groups);
    return html`<section>
        <div class="head">
          <h3>${t('linuxUsers.access.title')}</h3>
          ${this.canSync ? this.#syncAction(servers) : nothing}
        </div>
        <p>${t('linuxUsers.access.intro')}</p>
        ${this.user.groups.length === 0
          ? html`<p class="empty">${t('linuxUsers.access.noGroups')}</p>`
          : html`<ul class="rows">${this.user.groups.map((g) => this.#groupRow(g))}</ul>`}
        ${this.user.servers.length > 0
          ? html`<p class="note">${t('linuxUsers.access.direct', { names: serverNames(this.user.servers, 8) })}</p>`
          : nothing}
        ${this.canWrite ? this.#assignForm() : nothing}
      </section>
      ${this.#changeDialog()}`;
  }
}
customElements.define('lcm-lu-user-access', LcmLuUserAccess);
