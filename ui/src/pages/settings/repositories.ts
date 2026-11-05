import { LitElement, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import '../../components/lcm-textarea';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { KNOWN_REPOS_URL, normaliseKnownRepos, settingsMoreApi, type KnownRepo } from '../../api/settings-more';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { ModalForm, generalError, sectionStyles, tableLabels } from './packages/kit';
import { MANAGER_KEYS, PACKAGE_MANAGERS, URL_HINT_KEYS, emptyRepoForm, repoBody, repoFormOf, validateRepoForm, type RepoForm } from './packages/repo-model';
import './packages/cells';
import './packages/settings-modal';

// Das Backend meldet Eingabefehler des Katalogs nur allgemein (`known_repo.invalid_input`); Längen
// und Steuerzeichen kommen mit Feldnamen.
// Technische Beispiele in Platzhaltern, nicht übersetzt.
const EXAMPLES = {
  key: 'nodejs',
  keyUrl: 'https://…/gpg.key',
  url: 'https://…',
  line: 'deb [arch=$ARCH signed-by=/etc/apt/keyrings/<key>.asc] https://… $CODENAME main',
};

const FIELDS: Record<string, string> = { 'field:name': 'name', 'field:key': 'key', 'field:description': 'description' };

type RepoErrors = ReturnType<typeof validateRepoForm>;

/** Einstellungen → Repositories: Katalog der Paketquellen, die im Server-Detail angeboten werden. */
export class SettingsRepositories extends LitElement {
  static override properties = { _dialogError: { state: true }, _delete: { state: true }, _deleting: { state: true } };
  static override styles = [baseStyles, sectionStyles];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _delete: KnownRepo | null;
  declare _deleting: boolean;
  readonly #repos = new LiveData<KnownRepo[]>(this, { url: () => KNOWN_REPOS_URL, transform: normaliseKnownRepos });
  readonly #modal = new ModalForm<RepoForm>(this, (modal) => {
    if (modal.kind === 'new') return emptyRepoForm();
    if (modal.kind !== 'edit' || !this.#repos.data) return this.#repos.loading ? undefined : null;
    const repo = this.#repos.data.find((r) => r.id === modal.id);
    return repo ? repoFormOf(repo) : null;
  });

  constructor() {
    super();
    this._dialogError = '';
    this._delete = null;
    this._deleting = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    palette.register(this, [{ id: 'repo-new', key: 'n', label: t('settings.repositories.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) }]);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  override willUpdate(): void {
    this.#modal.sync();
  }

  async #save(): Promise<void> {
    const form = this.#modal.form;
    if (!form) return;
    const errors = validateRepoForm(form);
    const id = this.#modal.editId;
    const saved = await this.#modal.submit(Object.keys(errors).length === 0, () => settingsMoreApi.saveKnownRepo(id, repoBody(form)), FIELDS);
    if (saved) toast({ tone: 'ok', title: t(id ? 'settings.repositories.notice.saved' : 'settings.repositories.notice.created', { name: form.name.trim() }) });
  }

  async #confirmDelete(): Promise<void> {
    const repo = this._delete;
    if (!repo) return;
    this._deleting = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.deleteKnownRepo(repo.id);
      toast({ tone: 'ok', title: t('settings.repositories.notice.deleted', { name: repo.name }) });
      this._delete = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._deleting = false;
    }
  }

  get #columns(): Column<KnownRepo>[] {
    return [
      {
        key: 'name',
        label: t('settings.repositories.column.name'),
        sortable: true,
        render: (r) => html`<lcm-settings-cell sub=${r.description}><strong>${r.name}</strong></lcm-settings-cell>`,
      },
      {
        key: 'package_manager',
        label: t('settings.repositories.column.manager'),
        sortable: true,
        render: (r) => html`<lcm-badge>${r.package_manager}</lcm-badge>`,
      },
      { key: 'key', label: t('settings.repositories.column.key'), kind: 'mono', sortable: true },
      { key: 'line', label: t('settings.repositories.column.source'), kind: 'mono' },
      {
        key: 'actions',
        label: t('app.actions'),
        labelHidden: true,
        align: 'end',
        render: (r) => html`<lcm-settings-actions>
          <lcm-button
            variant="ghost"
            size="small"
            icon="trash"
            icon-only
            label=${t('settings.repositories.delete.label')}
            @click=${(e: Event) => {
              e.stopPropagation();
              this._delete = r;
            }}
          ></lcm-button>
        </lcm-settings-actions>`,
      },
    ];
  }

  #table() {
    const repos = this.#repos.data;
    if (!repos) {
      return this.#repos.error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('settings.more.loadFailed')} text=${this.#repos.error}></lcm-empty-state>`
        : html`<lcm-skeleton lines="4"></lcm-skeleton>`;
    }
    if (repos.length === 0) {
      return html`<lcm-empty-state level="3" icon="package" heading=${t('settings.repositories.empty.title')} text=${t('settings.repositories.empty.text')}></lcm-empty-state>`;
    }
    return html`<lcm-data-table
      no-toolbar
      density="compact"
      active-id=${String(this.#modal.editId ?? '')}
      .columns=${this.#columns}
      .rows=${repos.map((r) => ({ ...r, id: String(r.id) }))}
      .labels=${tableLabels(t('settings.repositories.caption'), t('settings.repositories.empty.title'))}
      @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#modal.show({ kind: 'edit', id: Number(e.detail.id) })}
    ></lcm-data-table>`;
  }

  #lineField(form: RepoForm, errors: RepoErrors) {
    const set = (e: CustomEvent<{ value: string }>) => this.#modal.set({ line: e.detail.value });
    if (form.package_manager === 'apt') {
      return html`<lcm-textarea
        label=${t('settings.repositories.form.line')}
        mono
        rows="3"
        maxlength=${LIMITS.path}
        placeholder=${EXAMPLES.line}
        hint=${t('settings.repositories.form.lineHint')}
        error=${this.#modal.fieldText(errors, 'line', LIMITS.path)}
        .value=${form.line}
        @lcm-input=${set}
      ></lcm-textarea>`;
    }
    return html`<lcm-input
      label=${t('settings.repositories.form.url')}
      width="full"
      mono
      maxlength=${LIMITS.path}
      placeholder=${EXAMPLES.url}
      hint=${t(URL_HINT_KEYS[form.package_manager] ?? 'settings.repositories.form.urlHint.dnf')}
      error=${this.#modal.fieldText(errors, 'line', LIMITS.path)}
      .value=${form.line}
      @lcm-input=${set}
    ></lcm-input>`;
  }

  #identityFields(form: RepoForm, errors: RepoErrors) {
    return html`<lcm-input
        label=${t('settings.repositories.form.name')}
        placeholder=${t('settings.repositories.form.namePlaceholder')}
        maxlength=${LIMITS.name}
        error=${this.#modal.fieldText(errors, 'name', LIMITS.name)}
        .value=${form.name}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ name: e.detail.value })}
      ></lcm-input>
      <lcm-input
        label=${t('settings.repositories.form.key')}
        mono
        maxlength=${LIMITS.name}
        placeholder=${EXAMPLES.key}
        hint=${t('settings.repositories.form.keyHint')}
        error=${this.#modal.fieldText(errors, 'key')}
        .value=${form.key}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ key: e.detail.value })}
      ></lcm-input>
      <lcm-select
        label=${t('settings.repositories.form.manager')}
        hint=${t('settings.repositories.form.managerHint')}
        .options=${PACKAGE_MANAGERS.map((m) => ({ value: m, label: t(MANAGER_KEYS[m]) }))}
        .value=${form.package_manager}
        @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ package_manager: e.detail.value })}
      ></lcm-select>`;
  }

  #sourceFields(form: RepoForm, errors: RepoErrors) {
    return html`<lcm-input
        label=${t('settings.repositories.form.description')}
        width="full"
        maxlength=${LIMITS.description}
        error=${this.#modal.fieldText(errors, 'description', LIMITS.description)}
        .value=${form.description}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ description: e.detail.value })}
      ></lcm-input>
      <lcm-input
        label=${t('settings.repositories.form.keyUrl')}
        width="full"
        mono
        maxlength=${LIMITS.path}
        placeholder=${EXAMPLES.keyUrl}
        hint=${t('settings.repositories.form.keyUrlHint')}
        error=${this.#modal.fieldText(errors, 'key_url', LIMITS.path)}
        .value=${form.key_url}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ key_url: e.detail.value })}
      ></lcm-input>
      ${this.#lineField(form, errors)}`;
  }

  #modalView() {
    const form = this.#modal.form;
    const errors = form ? validateRepoForm(form) : {};
    const editing = this.#modal.editId !== null;
    return html`<lcm-settings-modal
      ?open=${form !== null}
      heading=${t(editing ? 'settings.repositories.form.editTitle' : 'settings.repositories.form.newTitle')}
      save-label=${t(editing ? 'settings.more.modal.save' : 'settings.more.modal.create')}
      ?dirty=${this.#modal.dirty}
      ?busy=${this.#modal.busy}
      error=${generalError(this.#modal.error)}
      @lcm-submit=${() => void this.#save()}
      @lcm-close=${() => this.#modal.close()}
    >
      ${form ? html`${this.#identityFields(form, errors)} ${this.#sourceFields(form, errors)}` : nothing}
    </lcm-settings-modal>`;
  }

  #deleteDialog() {
    const repo = this._delete;
    return html`<lcm-dialog
      ?open=${repo !== null}
      heading=${t('settings.repositories.delete.title', { name: repo?.name ?? '' })}
      message=${t('settings.repositories.delete.message')}
      .impacts=${[
        { label: t('settings.repositories.delete.servers'), value: t('settings.repositories.delete.serversValue'), tone: 'ok' },
        { label: t('settings.more.impact.reversible'), value: t('settings.repositories.delete.reversible'), tone: 'warn' },
      ]}
      confirm-label=${t('settings.repositories.delete.confirm')}
      cancel-label=${t('settings.more.modal.cancel')}
      tone="danger"
      error=${this._dialogError}
      ?busy=${this._deleting}
      @lcm-confirm=${() => void this.#confirmDelete()}
      @lcm-cancel=${() => (this._delete = null)}
    ></lcm-dialog>`;
  }

  override render() {
    return html`<div class="main">
        <p class="lead">${t('settings.repositories.lead')}</p>
        <lcm-card level="2" heading=${t('settings.repositories.catalog')}>
          <lcm-button slot="actions" variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>
            ${t('settings.repositories.add')}
          </lcm-button>
          ${this.#table()}
        </lcm-card>
        <p class="callout">
          ${t('settings.repositories.cachePointer')}
          <a href="/settings/apt-cache">${t('settings.repositories.cacheLink')}</a>
        </p>
      </div>
      ${this.#modalView()} ${this.#deleteDialog()}`;
  }
}
customElements.define('lcm-settings-repositories', SettingsRepositories);
