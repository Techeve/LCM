import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, emit, utilStyles } from '../../components/shared';
import { profilesApi, type LinuxUser, type Profile } from '../../api/linux-users';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { cloneProfile } from './copy-actions';
import type { TableView } from './lu-paging';
import { pagedTable, plainLabels } from './lu-table';
import { ruleCell, ruleCount } from './profile-model';

/**
 * <lcm-lu-profiles .profiles=${…} .users=${…} active-id="3" can-write> – Tabelle der Berechtigungsprofile mit Kopieren
 * und Löschen je Zeile. Meldet `lcm-open-row` mit { id }, `lcm-copied` mit { id }.
 */
export class LcmLuProfiles extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    profiles: { attribute: false },
    users: { attribute: false },
    activeId: { type: String, attribute: 'active-id' },
    view: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _delete: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .name {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
      }
      .row-actions {
        display: flex;
        justify-content: flex-end;
        gap: var(--space-1);
      }
    `,
  ];

  declare profiles: Profile[];
  declare users: LinuxUser[];
  declare activeId: string;
  declare view: TableView;
  declare canWrite: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _delete: Profile | null;
  declare _busy: boolean;

  constructor() {
    super();
    this._dialogError = '';
    this.profiles = [];
    this.users = [];
    this.activeId = '';
    this.view = { page: 1, sort: null };
    this.canWrite = false;
    this._delete = null;
    this._busy = false;
    new I18nController(this);
  }

  #usedBy(profile: Profile): number {
    return this.users.filter((u) => u.default_profile_id === Number(profile.id)).length;
  }

  async #copy(profile: Profile): Promise<void> {
    const id = await cloneProfile(profile, this.profiles);
    if (id !== null) emit(this, 'lcm-copied', { id });
  }

  async #confirmDelete(): Promise<void> {
    const profile = this._delete;
    if (!profile) return;
    this._busy = true;
    this._dialogError = '';
    try {
      await profilesApi.remove(profile.id);
      toast({ tone: 'ok', title: t('profiles.notice.deleted', { name: profile.name }) });
      this._delete = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #actions(profile: Profile) {
    if (!this.canWrite) return nothing;
    const stop = (run: () => void) => (e: Event) => {
      e.stopPropagation();
      run();
    };
    return html`<span class="row-actions">
      <lcm-button variant="ghost" size="small" icon="copy" icon-only label=${t('profiles.clone')} @click=${stop(() => void this.#copy(profile))}></lcm-button>
      ${profile.builtin
        ? nothing
        : html`<lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('profiles.delete.label')} @click=${stop(() => (this._delete = profile))}></lcm-button>`}
    </span>`;
  }

  #rules(profile: Profile) {
    const cell = ruleCell(profile);
    if (cell === 'all') return html`<span class="muted">${t('profiles.rulesAll')}</span>`;
    if (cell === 'none') return html`<span class="muted">${t('profiles.rulesNone')}</span>`;
    return String(cell);
  }

  get #columns(): Column<Profile>[] {
    return [
      {
        key: 'name',
        label: t('profiles.column.name'),
        sortable: true,
        render: (r) => html`<span class="name"><strong>${r.name}</strong>
          ${r.builtin ? html`<lcm-badge>${t('linuxUsers.builtin')}</lcm-badge>` : nothing}
          ${r.grants_full_root ? html`<lcm-badge tone="warn">${t('profiles.fullRoot')}</lcm-badge>` : nothing}
          ${r.account_type === 'sftp' ? html`<lcm-badge tone="info">${t('profiles.account.sftp')}</lcm-badge>` : nothing}</span>`,
      },
      { key: 'description', label: t('profiles.column.description'), render: (r) => html`<span class="muted">${r.description || '–'}</span>` },
      { key: 'rules', label: t('profiles.column.rules'), align: 'end', sortable: true, value: (r) => ruleCount(r), render: (r) => this.#rules(r) },
      { key: 'blocks', label: t('profiles.column.blocks'), align: 'end', sortable: true, value: (r) => r.block_uses.length, render: (r) => String(r.block_uses.length) },
      { key: 'actions', label: t('app.actions'), labelHidden: true, align: 'end', render: (r) => this.#actions(r) },
    ];
  }

  #deleteDialog() {
    const profile = this._delete;
    const used = profile ? this.#usedBy(profile) : 0;
    return html`<lcm-dialog
      ?open=${profile !== null}
      heading=${t('profiles.delete.title', { name: profile?.name ?? '' })}
      message=${t('profiles.delete.message')}
      .impacts=${[
        { label: t('profiles.delete.users'), value: t('profiles.delete.usersValue', { count: used }), tone: used > 0 ? 'warn' : 'ok' },
        { label: t('linuxUsers.impact.reversible'), value: t('linuxUsers.impact.no'), tone: 'crit' },
      ]}
      expect=${profile?.name ?? ''}
      expect-label=${t('profiles.delete.expect', { name: profile?.name ?? '' })}
      confirm-label=${t('profiles.delete.confirm')}
      cancel-label=${t('linuxUsers.action.cancel')}
      tone="danger"
      error=${this._dialogError}
      ?busy=${this._busy}
      @lcm-confirm=${() => void this.#confirmDelete()}
      @lcm-cancel=${() => (this._delete = null)}
    ></lcm-dialog>`;
  }

  override render() {
    return html`${pagedTable({
        items: this.profiles,
        columns: this.#columns,
        labels: plainLabels(t('profiles.caption'), t('profiles.empty.title')),
        view: this.view,
        activeId: this.activeId,
        onActivate: (id) => emit(this, 'lcm-open-row', { id }),
      })}
      ${this.#deleteDialog()}`;
  }
}
customElements.define('lcm-lu-profiles', LcmLuProfiles);
