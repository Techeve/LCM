import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-dialog';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-switch';
import type { Option } from '../../components/lcm-select';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, emit } from '../../components/shared';
import type { Group } from '../../api/groups';
import { linuxUsersApi, type LinuxUser, type Profile } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import './form-modal';
import { buildImpacts } from './impacts';
import './user-access';
import './user-activation';
import './user-keys';
import './user-remove';
import { EXAMPLES } from './examples';
import {
  changesServers,
  createBody,
  emptyUserForm,
  isValid,
  sameUserForm,
  userBody,
  userFormOf,
  userServers,
  validateUserForm,
  type UserForm,
} from './user-model';

type UserErrors = ReturnType<typeof validateUserForm>;

/**
 * <lcm-lu-user-modal open .user=${…} .profiles=${…} .groups=${…} can-write can-sync> – Benutzer anlegen (ohne
 * `user`) oder bearbeiten. Beim Bearbeiten folgen unter dem Formular Zugang, Schlüssel, Server und Entfernen mit
 * eigenen, sofort wirksamen Aktionen. Ändert das Speichern etwas auf den Servern, fragt ein Dialog mit den betroffenen
 * Servern nach. Meldet `lcm-saved` mit { id }, `lcm-created` mit { id } und `lcm-close`.
 */
export class LcmLuUserModal extends LitElement {
  static override properties = {
    open: { type: Boolean },
    user: { attribute: false },
    profiles: { attribute: false },
    groups: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    canSync: { type: Boolean, attribute: 'can-sync' },
    _form: { state: true },
    _busy: { state: true },
    _error: { state: true },
    _attempted: { state: true },
    _confirm: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: contents;
      }
      .chips {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
      }
    `,
  ];

  declare open: boolean;
  declare user: LinuxUser | null;
  declare profiles: Profile[];
  declare groups: Group[];
  declare canWrite: boolean;
  declare canSync: boolean;
  declare _form: UserForm;
  declare _busy: boolean;
  declare _error: string;
  declare _attempted: boolean;
  declare _confirm: boolean;
  #initial: UserForm = emptyUserForm([]);
  #key = '';

  constructor() {
    super();
    this.open = false;
    this.user = null;
    this.profiles = [];
    this.groups = [];
    this.canWrite = false;
    this.canSync = false;
    this._form = this.#initial;
    this._busy = false;
    this._error = '';
    this._attempted = false;
    this._confirm = false;
    new I18nController(this);
  }

  /** Neu laden, wenn die Fenster aufgeht oder ein anderer Benutzer gemeint ist; Aktualisierungen derselben Daten lassen Eingaben stehen. */
  override willUpdate(): void {
    const key = this.open ? `${this.user?.id ?? 'new'}` : '';
    if (key === this.#key) return;
    this.#key = key;
    if (!this.open) return;
    this.#initial = this.user ? userFormOf(this.user) : emptyUserForm(this.profiles);
    this._form = this.#initial;
    this._error = '';
    this._attempted = false;
    this._confirm = false;
  }

  get #creating(): boolean {
    return this.user === null;
  }

  get #profile(): Profile | undefined {
    return this.profiles.find((p) => String(p.id) === this._form.profile_id);
  }

  #set(patch: Partial<UserForm>): void {
    this._form = { ...this._form, ...patch };
  }

  #message(key: string | undefined): string {
    return this._attempted && key ? t(key, { max: 32 }) : '';
  }

  /** Auf den Servern des Benutzers wirkt eine Änderung sofort: erst fragen, dann speichern. */
  #save(): void {
    this._attempted = true;
    if (!isValid(validateUserForm(this._form, this.#creating))) return;
    if (this.user && changesServers(this.#initial, this._form) && userServers(this.user, this.groups).length > 0) this._confirm = true;
    else void this.#commit();
  }

  async #commit(): Promise<void> {
    this._busy = true;
    this._error = '';
    try {
      if (this.user) {
        await linuxUsersApi.update(this.user.id, userBody(this._form));
        toast({ tone: 'ok', title: t('linuxUsers.notice.saved', { name: this.user.username }) });
        this._form = this.#initial;
        emit(this, 'lcm-saved', { id: this.user.id });
      } else {
        const created = await linuxUsersApi.create(createBody(this._form));
        toast({ tone: 'ok', title: t('linuxUsers.notice.created', { name: this._form.username.trim().toLowerCase() }) });
        this._form = this.#initial;
        emit(this, 'lcm-created', { id: created.id });
      }
      this._confirm = false;
    } catch (error) {
      this._confirm = false;
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  get #profileOptions(): Option[] {
    return this.profiles.map((p) => ({ value: String(p.id), label: p.name }));
  }

  #changeSummary(): string {
    const f = this._form;
    const i = this.#initial;
    const parts: string[] = [];
    if (f.profile_id !== i.profile_id) parts.push(t('linuxUsers.save.change.profile'));
    if (f.active !== i.active) parts.push(f.active ? t('linuxUsers.save.change.activated') : t('linuxUsers.save.change.deactivated'));
    if (f.shell !== i.shell) parts.push(t('linuxUsers.save.change.shell'));
    if (f.full_name !== i.full_name) parts.push(t('linuxUsers.save.change.name'));
    return parts.join(', ');
  }

  #confirmDialog() {
    const user = this.user;
    const servers = user ? userServers(user, this.groups) : [];
    return html`<lcm-dialog
      ?open=${this._confirm}
      heading=${t('linuxUsers.save.heading', { name: user?.username ?? '' })}
      message=${t('linuxUsers.save.message', { changes: this.#changeSummary() })}
      confirm-label=${t('linuxUsers.action.save')}
      cancel-label=${t('linuxUsers.action.cancel')}
      tone=${this._form.active ? 'default' : 'danger'}
      .impacts=${buildImpacts({ servers, effect: t('linuxUsers.save.effect'), reversible: true })}
      ?busy=${this._busy}
      @lcm-cancel=${() => (this._confirm = false)}
      @lcm-confirm=${() => void this.#commit()}
    ></lcm-dialog>`;
  }

  #fields(errors: UserErrors) {
    return html`${this.#usernameField(errors)} ${this.#personFields(errors)} ${this.#profileField()} ${this.#activeSwitch()}`;
  }

  #usernameField(errors: UserErrors) {
    if (!this.#creating) return nothing;
    return html`<lcm-input
      label=${t('linuxUsers.form.username')}
      half
      width="full"
      mono
      required
      maxlength=${LIMITS.linuxUser}
      .value=${this._form.username}
      .error=${this.#message(errors.username)}
      hint=${t('linuxUsers.form.usernameHint')}
      placeholder=${EXAMPLES.username}
      autocomplete="off"
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ username: e.detail.value })}
    ></lcm-input>`;
  }

  #personFields(errors: UserErrors) {
    const disabled = !this.canWrite;
    return html`<lcm-input
        label=${t('linuxUsers.form.fullName')}
        half
        width="full"
        maxlength=${LIMITS.personName}
        .value=${this._form.full_name}
        .error=${this.#message(errors.full_name)}
        ?disabled=${disabled}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ full_name: e.detail.value })}
      ></lcm-input>
      <lcm-input
        label=${t('linuxUsers.form.email')}
        half
        width="full"
        type="email"
        maxlength=${LIMITS.email}
        .value=${this._form.email}
        .error=${this.#message(errors.email)}
        hint=${t('linuxUsers.form.emailHint')}
        ?disabled=${disabled}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ email: e.detail.value })}
      ></lcm-input>
      <lcm-input
        label=${t('linuxUsers.form.shell')}
        half
        width="full"
        mono
        maxlength=${LIMITS.name}
        .value=${this._form.shell}
        .error=${this.#message(errors.shell)}
        ?disabled=${disabled}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ shell: e.detail.value })}
      ></lcm-input>`;
  }

  #profileField() {
    if (this.profiles.length === 0) return nothing;
    const profile = this.#profile;
    return html`<lcm-select
        label=${t('linuxUsers.form.profile')}
        half
        width="full"
        .options=${this.#profileOptions}
        .value=${this._form.profile_id}
        hint=${profile?.description || t('linuxUsers.form.profileHint')}
        ?disabled=${!this.canWrite}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ profile_id: e.detail.value })}
      ></lcm-select>
      ${profile?.grants_full_root ? html`<div class="chips"><lcm-badge tone="warn">${t('linuxUsers.form.fullRoot')}</lcm-badge></div>` : nothing}`;
  }

  #activeSwitch() {
    if (this.#creating) return nothing;
    return html`<lcm-switch
      label=${t('linuxUsers.form.active')}
      ?checked=${this._form.active}
      ?disabled=${!this.canWrite}
      on-label=${t('linuxUsers.form.activeOnLabel')}
      on-text=${t('linuxUsers.form.activeOn')}
      off-label=${t('linuxUsers.form.activeOffLabel')}
      off-text=${t('linuxUsers.form.activeOff')}
      @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => this.#set({ active: e.detail.checked })}
    ></lcm-switch>`;
  }

  #extra(user: LinuxUser) {
    const servers = userServers(user, this.groups);
    return html`
      <lcm-lu-user-activation slot="extra" .user=${user} ?can-write=${this.canWrite}></lcm-lu-user-activation>
      <lcm-lu-user-keys slot="extra" .user=${user} .servers=${servers} ?can-write=${this.canWrite}></lcm-lu-user-keys>
      <lcm-lu-user-access slot="extra" .user=${user} .profiles=${this.profiles} .groups=${this.groups} ?can-write=${this.canWrite} ?can-sync=${this.canSync}></lcm-lu-user-access>
      ${this.canWrite
        ? html`<lcm-lu-user-remove slot="extra" .user=${user} .servers=${servers} @lcm-deleted=${() => emit(this, 'lcm-close')}></lcm-lu-user-remove>`
        : nothing}
    `;
  }

  override render() {
    const errors = validateUserForm(this._form, this.#creating);
    const user = this.user;
    return html`<lcm-lu-form-modal
        ?open=${this.open}
        heading=${user ? user.username : t('linuxUsers.form.newTitle')}
        subtitle=${user ? user.full_name : t('linuxUsers.form.newSubtitle')}
        save-label=${this.#creating ? t('linuxUsers.action.create') : t('linuxUsers.action.save')}
        ?dirty=${this.canWrite && !sameUserForm(this._form, this.#initial)}
        ?busy=${this._busy}
        ?read-only=${!this.canWrite}
        error=${this._error}
        @lcm-submit=${this.#save}
      >
        ${this.#fields(errors)} ${user ? this.#extra(user) : nothing}
      </lcm-lu-form-modal>
      ${this.#confirmDialog()}`;
  }
}
customElements.define('lcm-lu-user-modal', LcmLuUserModal);
