import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-menu';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import type { Column, Row } from '../../components/lcm-data-table';
import type { MenuItem } from '../../components/lcm-menu';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { ROLES_URL, USERS_URL, readRoles, readUsers, settingsApi, type ActivationLink, type LoginUser, type Role } from '../../api/settings';
import { ApiError } from '../../lib/api';
import { auth } from '../../lib/auth';
import { copyText } from '../../lib/clipboard';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { formatDateTime, formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import type { PasswordIdentity } from '../account/password-policy';
import './access/form-modal';
import { blockDialog, deleteDialog, passwordDialog } from './user-dialogs';
import {
  activationUrl,
  assignableRoles,
  emptyUserForm,
  fullName,
  identityOfForm,
  needsCurrentPassword,
  newUserBody,
  profileBody,
  profileChanged,
  rolesChanged,
  sameUserForm,
  toggleRole,
  userActions,
  userFieldForError,
  userFormOf,
  validateNewPassword,
  validateUserForm,
  type UserForm,
  type UserFormErrors,
} from './access/users-model';
import { passwordField } from './system/password-field';
import { settingsStyles } from './system/settings-styles';
import { tableLabels } from './system/table-labels';

type Modal = { mode: 'create' } | { mode: 'edit'; user: LoginUser };
type Dialog =
  | { kind: 'password'; user: LoginUser }
  | { kind: 'block'; user: LoginUser }
  | { kind: 'unblock'; user: LoginUser }
  | { kind: 'delete'; user: LoginUser }
  | { kind: 'link'; user: LoginUser; link: ActivationLink };

const DEFAULT_ROLES = ['manager'];

/** Einstellungen → Benutzer: LCM-Konten anlegen, bearbeiten, einladen, sperren, löschen; Rollen und Passwort. */
export class SettingsUsers extends LitElement {
  static override properties = {
    _modal: { state: true },
    _form: { state: true },
    _errors: { state: true },
    _error: { state: true },
    _busy: { state: true },
    _dialog: { state: true },
    _password: { state: true },
    _confirm: { state: true },
    _dialogErrors: { state: true },
    _dialogError: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      fieldset {
        margin: 0;
        padding: 0;
        border: 0;
      }
      legend {
        margin-block-end: var(--space-2);
        color: var(--text-1);
        font-size: var(--fs-s);
        font-weight: var(--fw-medium);
      }
      .role {
        display: flex;
        align-items: flex-start;
        gap: var(--space-2);
        padding-block: var(--space-1);
        font-size: var(--fs-s);
      }
      .role input {
        margin-block-start: 3px;
        accent-color: var(--accent);
      }
      .role span {
        display: flex;
        flex-direction: column;
      }
      .role small {
        color: var(--text-2);
      }
      .field-error {
        margin: calc(var(--space-2) * -1) 0 0;
        color: var(--crit-text);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare _modal: Modal | undefined;
  declare _form: UserForm;
  declare _errors: UserFormErrors;
  declare _error: string;
  declare _busy: boolean;
  declare _dialog: Dialog | undefined;
  declare _password: string;
  declare _confirm: string;
  declare _dialogErrors: { password?: string; confirm?: string };
  declare _dialogError: string;

  readonly #users = new LiveData<LoginUser[]>(this, { url: () => USERS_URL, transform: readUsers });
  readonly #roles = new LiveData<Role[]>(this, { url: () => (auth.can('roles:read') ? ROLES_URL : null), transform: readRoles });
  #initial: UserForm = emptyUserForm();

  constructor() {
    super();
    this._modal = undefined;
    this._form = emptyUserForm();
    this._errors = {};
    this._error = '';
    this._busy = false;
    this._dialog = undefined;
    this._password = '';
    this._confirm = '';
    this._dialogErrors = {};
    this._dialogError = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  get #selfId(): number | undefined {
    return auth.user?.id;
  }

  #can = (permission: string): boolean => auth.can(permission);

  // ---- Fenster: anlegen und bearbeiten ----

  #openCreate(): void {
    this.#initial = emptyUserForm(DEFAULT_ROLES);
    this.#openModal({ mode: 'create' });
  }

  #openEdit(user: LoginUser): void {
    this.#initial = userFormOf(user);
    this.#openModal({ mode: 'edit', user });
  }

  #openModal(modal: Modal): void {
    this._form = { ...this.#initial, roles: [...this.#initial.roles] };
    this._errors = {};
    this._error = '';
    this._modal = modal;
  }

  #closeModal(): void {
    this._modal = undefined;
  }

  #setForm(patch: Partial<UserForm>): void {
    this._form = { ...this._form, ...patch };
    const touched = Object.keys(patch) as (keyof UserFormErrors)[];
    if (touched.some((key) => this._errors[key])) {
      const errors = { ...this._errors };
      for (const key of touched) delete errors[key];
      this._errors = errors;
    }
  }

  async #submit(): Promise<void> {
    const modal = this._modal;
    if (!modal || this._busy) return;
    const needsCurrent = modal.mode === 'edit' && needsCurrentPassword(modal.user, this._form, this.#selfId);
    const errors = validateUserForm(this._form, modal.mode, needsCurrent);
    if (Object.keys(errors).length > 0) {
      this._errors = errors;
      this._error = t('settings.form.fixErrors');
      return;
    }
    this._busy = true;
    this._error = '';
    try {
      if (modal.mode === 'create') await this.#create();
      else await this.#saveEdit(modal.user);
      this._modal = undefined;
    } catch (error) {
      this.#showFormError(error);
    } finally {
      this._busy = false;
    }
  }

  async #create(): Promise<void> {
    await settingsApi.createUser(newUserBody(this._form));
    toast({ tone: 'ok', title: t('settings.users.created', { name: this._form.username.trim() }) });
  }

  async #saveEdit(user: LoginUser): Promise<void> {
    if (profileChanged(user, this._form)) await settingsApi.updateProfile(user.id, profileBody(this._form));
    if (userActions(user, this.#selfId, this.#can).roles && rolesChanged(user, this._form.roles)) await settingsApi.updateRoles(user.id, this._form.roles);
    toast({ tone: 'ok', title: t('settings.users.saved', { name: user.username }) });
  }

  #showFormError(error: unknown): void {
    const message = errorText(error);
    const field = error instanceof ApiError ? userFieldForError(error.code, error.params) : undefined;
    if (field) this._errors = { ...this._errors, [field]: message };
    this._error = field ? t('settings.form.fixErrors') : message;
  }

  /** Prüfschlüssel werden übersetzt, alles andere ist schon der Text des Servers; `max` füllt `check.tooLong`. */
  #message(key: string | undefined, max = 0): string {
    if (!key) return '';
    return key.startsWith('settings.') || key.startsWith('check.') ? t(key, { max }) : key;
  }

  #roleChoices(): TemplateResult | typeof nothing {
    const modal = this._modal;
    const canAssign = modal?.mode === 'create' ? this.#can('roles:read') : modal ? userActions(modal.user, this.#selfId, this.#can).roles : false;
    const roles = assignableRoles(this.#roles.data ?? []);
    if (!canAssign || roles.length === 0) return nothing;
    return html`<fieldset>
      <legend>${t('settings.users.form.roles')}</legend>
      ${roles.map(
        (role) => html`<label class="role">
          <input type="checkbox" .checked=${this._form.roles.includes(role.name)} @change=${() => this.#setForm({ roles: toggleRole(this._form.roles, role.name) })} />
          <span>${role.name}${role.description ? html`<small>${role.description}</small>` : nothing}</span>
        </label>`,
      )}
    </fieldset>`;
  }

  #modalView(): TemplateResult {
    const modal = this._modal;
    const editing = modal?.mode === 'edit' ? modal.user : undefined;
    return html`<lcm-settings-form-modal
      ?open=${!!modal}
      heading=${editing ? t('settings.users.editTitle', { name: editing.username }) : t('settings.users.createTitle')}
      subtitle=${editing ? '' : t('settings.users.createSubtitle')}
      save-label=${editing ? t('settings.form.save') : t('settings.users.createConfirm')}
      ?dirty=${!sameUserForm(this._form, this.#initial)}
      ?busy=${this._busy}
      error=${this._error}
      @lcm-submit=${() => void this.#submit()}
      @lcm-close=${() => this.#closeModal()}
    >
      ${modal ? this.#formFields(editing) : nothing}
    </lcm-settings-form-modal>`;
  }

  #formFields(editing: LoginUser | undefined): TemplateResult {
    const form = this._form;
    const errors = this._errors;
    return html`${editing ? nothing : this.#usernameField(errors)} ${this.#emailField(errors)} ${this.#currentPasswordField(editing, errors)} ${this.#nameRow(errors)}
      ${editing ? nothing : this.#newPasswordFields(form.password, form.confirm, errors, identityOfForm(form), (patch) => this.#setForm(patch))} ${this.#roleChoices()}`;
  }

  #usernameField(errors: UserFormErrors): TemplateResult {
    return html`<lcm-input
      label=${t('settings.users.form.username')}
      hint=${t('settings.users.form.usernameHint')}
      error=${this.#message(errors.username)}
      autocomplete="off"
      mono
      required
      maxlength=${LIMITS.name}
      .value=${this._form.username}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setForm({ username: e.detail.value })}
    ></lcm-input>`;
  }

  #emailField(errors: UserFormErrors): TemplateResult {
    return html`<lcm-input
      label=${t('settings.users.form.email')}
      width="l"
      type="email"
      hint=${t('settings.users.form.emailHint')}
      error=${this.#message(errors.email)}
      maxlength=${LIMITS.email}
      autocomplete="off"
      .value=${this._form.email}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setForm({ email: e.detail.value })}
    ></lcm-input>`;
  }

  #currentPasswordField(editing: LoginUser | undefined, errors: UserFormErrors): TemplateResult | typeof nothing {
    if (!editing || !needsCurrentPassword(editing, this._form, this.#selfId)) return nothing;
    return html`<lcm-input
      label=${t('settings.users.form.current')}
      type="password"
      hint=${t('settings.users.form.currentHint')}
      error=${this.#message(errors.current)}
      maxlength=${LIMITS.password}
      autocomplete="current-password"
      .value=${this._form.current}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setForm({ current: e.detail.value })}
    ></lcm-input>`;
  }

  #nameRow(errors: UserFormErrors): TemplateResult {
    const form = this._form;
    return html`<div class="field-row">
      <lcm-input
        label=${t('settings.users.form.firstName')}
        autocomplete="off"
        maxlength=${LIMITS.personName}
        error=${this.#message(errors.firstName, LIMITS.personName)}
        .value=${form.firstName}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setForm({ firstName: e.detail.value })}
      ></lcm-input>
      <lcm-input
        label=${t('settings.users.form.lastName')}
        autocomplete="off"
        maxlength=${LIMITS.personName}
        error=${this.#message(errors.lastName, LIMITS.personName)}
        .value=${form.lastName}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#setForm({ lastName: e.detail.value })}
      ></lcm-input>
    </div>`;
  }

  /** Passwort mit Stärkeanzeige und Wiederholung; Fehler stehen unter dem jeweiligen Feld. */
  #newPasswordFields(password: string, confirm: string, errors: { password?: string; confirm?: string }, identity: PasswordIdentity, set: (patch: { password?: string; confirm?: string }) => void): TemplateResult {
    return html`${passwordField(t('settings.users.form.password'), password, (value) => set({ password: value }), t('settings.users.form.passwordHint'), LIMITS.newPassword, identity)}
      ${errors.password ? html`<p class="field-error" role="alert">${this.#message(errors.password, LIMITS.newPassword)}</p>` : nothing}
      <lcm-input
        label=${t('settings.users.form.confirm')}
        type="password"
        autocomplete="new-password"
        maxlength=${LIMITS.newPassword}
        error=${this.#message(errors.confirm)}
        .value=${confirm}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => set({ confirm: e.detail.value })}
      ></lcm-input>`;
  }

  // ---- Bestätigungen und Ergebnisse ----

  #openDialog(dialog: Dialog): void {
    this._password = '';
    this._confirm = '';
    this._dialogErrors = {};
    this._dialogError = '';
    this._dialog = dialog;
  }

  #closeDialog(): void {
    this._dialog = undefined;
    this._password = '';
    this._confirm = '';
  }

  async #run(action: () => Promise<void>): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await action();
      this.#closeDialog();
    } catch (error) {
      const message = errorText(error);
      if (error instanceof ApiError && userFieldForError(error.code, error.params) === 'password') this._dialogErrors = { password: message };
      else this._dialogError = message;
    } finally {
      this._busy = false;
    }
  }

  #resetPassword(user: LoginUser): void {
    const errors = validateNewPassword(this._password, this._confirm, identityOfForm(user));
    this._dialogErrors = errors;
    if (Object.keys(errors).length > 0) return;
    void this.#run(async () => {
      await settingsApi.resetPassword(user.id, this._password);
      toast({ tone: 'ok', title: t('settings.users.password.done', { name: user.username }) });
    });
  }

  #setActive(user: LoginUser, active: boolean): void {
    void this.#run(async () => {
      await settingsApi.setActive(user.id, active);
      toast({ tone: 'ok', title: t(active ? 'settings.users.unblock.done' : 'settings.users.block.done', { name: user.username }) });
    });
  }

  #delete(user: LoginUser): void {
    void this.#run(async () => {
      await settingsApi.deleteUser(user.id);
      toast({ tone: 'ok', title: t('settings.users.delete.done', { name: user.username }) });
    });
  }

  async #invite(user: LoginUser): Promise<void> {
    try {
      const link = await settingsApi.createActivationLink(user.id, user.email !== '');
      this.#openDialog({ kind: 'link', user, link });
    } catch (error) {
      toast({ tone: 'crit', title: errorText(error) });
    }
  }

  async #copyAndClose(url: string): Promise<void> {
    if (await copyText(url)) toast({ tone: 'ok', title: t('settings.users.link.copied') });
    this.#closeDialog();
  }

  #linkDialog(user: LoginUser, link: ActivationLink): TemplateResult {
    const mailState = link.mailed
      ? html`<lcm-notice tone="ok">${t('settings.users.link.mailed', { email: user.email })}</lcm-notice>`
      : link.mailError
        ? html`<lcm-notice tone="warn">${t('settings.users.link.mailFailed', { reason: link.mailError })}</lcm-notice>`
        : html`<lcm-notice>${t('settings.users.link.noMail')}</lcm-notice>`;
    const url = activationUrl(location.origin, link.token);
    return html`<lcm-dialog
      open
      heading=${t('settings.users.link.title', { name: user.username })}
      message=${t('settings.users.link.message', { until: formatDateTime(link.expiresAt) })}
      confirm-label=${t('settings.users.link.copyClose')}
      cancel-label=${t('settings.form.close')}
      @lcm-confirm=${() => void this.#copyAndClose(url)}
      @lcm-cancel=${() => this.#closeDialog()}
    >
      <div class="fields">
        ${mailState}
        <lcm-copy-field block value=${url}></lcm-copy-field>
      </div>
    </lcm-dialog>`;
  }

  #dialogView(): TemplateResult | typeof nothing {
    const dialog = this._dialog;
    if (!dialog) return nothing;
    const state = { busy: this._busy, error: this._dialogError, onCancel: () => this.#closeDialog() };
    const user = dialog.user;
    switch (dialog.kind) {
      case 'link':
        return this.#linkDialog(user, dialog.link);
      case 'password':
        return passwordDialog(user, this.#passwordFields(user), state, () => this.#resetPassword(user));
      case 'block':
      case 'unblock':
        return blockDialog(user, dialog.kind === 'block', state, () => this.#setActive(user, dialog.kind === 'unblock'));
      default:
        return deleteDialog(user, state, () => this.#delete(user));
    }
  }

  #passwordFields(user: LoginUser): TemplateResult {
    return this.#newPasswordFields(this._password, this._confirm, this._dialogErrors, identityOfForm(user), (patch) => {
      if (patch.password !== undefined) this._password = patch.password;
      if (patch.confirm !== undefined) this._confirm = patch.confirm;
      this._dialogErrors = {};
    });
  }

  // ---- Tabelle ----

  #menuItems(user: LoginUser): MenuItem[] {
    const can = userActions(user, this.#selfId, this.#can);
    const items: MenuItem[] = [];
    if (can.invite) items.push({ id: 'invite', icon: 'key', label: t('settings.users.menu.invite'), hint: t('settings.users.menu.inviteHint') });
    if (can.password) items.push({ id: 'password', icon: 'lock', label: t('settings.users.menu.password'), hint: t('settings.users.menu.passwordHint') });
    if (can.block && user.active) items.push({ id: 'block', icon: 'power', label: t('settings.users.menu.block'), hint: t('settings.users.menu.blockHint'), separatorBefore: items.length > 0 });
    if (can.block && !user.active) items.push({ id: 'unblock', icon: 'undo', label: t('settings.users.menu.unblock'), separatorBefore: items.length > 0 });
    if (can.remove) items.push({ id: 'delete', icon: 'trash', label: t('settings.users.menu.delete'), danger: true });
    return items;
  }

  #onMenu(user: LoginUser, id: string): void {
    if (id === 'invite') void this.#invite(user);
    else if (id === 'password') this.#openDialog({ kind: 'password', user });
    else if (id === 'block') this.#openDialog({ kind: 'block', user });
    else if (id === 'unblock') this.#openDialog({ kind: 'unblock', user });
    else if (id === 'delete') this.#openDialog({ kind: 'delete', user });
  }

  #rowActions(user: LoginUser): TemplateResult {
    const can = userActions(user, this.#selfId, this.#can);
    const items = this.#menuItems(user);
    return html`<span class="row end">
      ${can.edit
        ? html`<lcm-button variant="ghost" size="small" icon="edit" @click=${() => this.#openEdit(user)}>${t('settings.users.edit')}</lcm-button>`
        : nothing}
      ${items.length > 0
        ? html`<lcm-menu
            icon="menu"
            label=${t('settings.users.more', { name: user.username })}
            .items=${items}
            @lcm-select=${(e: CustomEvent<{ id: string }>) => this.#onMenu(user, e.detail.id)}
          ></lcm-menu>`
        : nothing}
    </span>`;
  }

  /** Ein Zustand je Konto, der wichtigste zuerst. */
  #state(user: LoginUser): TemplateResult {
    if (!user.active) return html`<lcm-badge tone="crit">${t('settings.users.state.blocked')}</lcm-badge>`;
    if (user.isSystem) return html`<lcm-badge>${t('settings.users.state.system')}</lcm-badge>`;
    if (user.mustChangePassword) return html`<lcm-badge tone="warn">${t('settings.users.state.mustChange')}</lcm-badge>`;
    return html`<lcm-badge tone="ok">${t('settings.users.state.active')}</lcm-badge>`;
  }

  get #columns(): Column[] {
    return [
      {
        key: 'username',
        label: t('settings.users.col.user'),
        sortable: true,
        locked: true,
        render: (row) => {
          const user = row.user as LoginUser;
          return html`<strong>${user.username}</strong>${user.id === this.#selfId ? html` <lcm-badge tone="info">${t('settings.users.you')}</lcm-badge>` : nothing}`;
        },
      },
      { key: 'name', label: t('settings.users.col.name'), sortable: true },
      { key: 'email', label: t('settings.users.col.email'), sortable: true },
      {
        key: 'roles',
        label: t('settings.users.col.roles'),
        render: (row) => html`${(row.user as LoginUser).roles.map((role) => html`<lcm-badge tone=${role === 'admin' ? 'accent' : 'neutral'}>${role}</lcm-badge> `)}`,
      },
      {
        key: 'twofa',
        label: t('settings.users.col.twofa'),
        sortable: true,
        render: (row) => ((row.user as LoginUser).totpEnabled ? html`<lcm-badge tone="ok">${t('settings.users.twofa.on')}</lcm-badge>` : html`<span class="muted">${t('settings.users.twofa.off')}</span>`),
      },
      { key: 'state', label: t('settings.users.col.state'), render: (row) => this.#state(row.user as LoginUser) },
      {
        key: 'lastLogin',
        label: t('settings.users.col.lastLogin'),
        sortable: true,
        sortValue: (row) => String(row.lastLogin ?? ''),
        render: (row) => {
          const at = row.lastLogin as string | null;
          return at ? html`<span title=${formatDateTime(at)}>${formatRelative(at)}</span>` : html`<span class="muted">${t('settings.users.never')}</span>`;
        },
      },
      { key: 'actions', label: t('settings.users.col.actions'), align: 'end', locked: true, render: (row) => this.#rowActions(row.user as LoginUser) },
    ];
  }

  #table(users: LoginUser[]): TemplateResult {
    const rows: Row[] = users.map((user) => ({ id: String(user.id), username: user.username, name: fullName(user), email: user.email, twofa: user.totpEnabled ? 1 : 0, lastLogin: user.lastLoginAt, user }));
    return html`<lcm-card level="2" heading=${t('settings.users.list.title')} subtitle=${t('settings.users.list.subtitle')}>
      ${this.#can('users:write')
        ? html`<lcm-button slot="actions" variant="primary" icon="plus" @click=${() => this.#openCreate()}>${t('settings.users.create')}</lcm-button>`
        : nothing}
      <lcm-data-table density="compact" no-toolbar no-chooser .columns=${this.#columns} .rows=${rows} .labels=${tableLabels(t('settings.users.list.title'), t('settings.users.list.empty'))}></lcm-data-table>
    </lcm-card>`;
  }

  override render() {
    const users = this.#users.data;
    if (!users) {
      if (this.#users.error) {
        return html`<lcm-empty-state level="2" icon="warning" heading=${t('settings.form.loadFailed')} text=${this.#users.error}>
          <lcm-button slot="action" @click=${() => this.#users.reload()}>${t('settings.form.retry')}</lcm-button>
        </lcm-empty-state>`;
      }
      return html`<div class="skeletons"><lcm-skeleton shape="block" label=${t('settings.form.loading')}></lcm-skeleton></div>`;
    }
    return html`<div class="stack column wide">${this.#table(users)}</div>
      ${this.#modalView()} ${this.#dialogView()}`;
  }
}
customElements.define('lcm-settings-users', SettingsUsers);
