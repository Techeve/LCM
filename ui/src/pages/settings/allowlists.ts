import { LitElement, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-skeleton';
import '../../components/lcm-textarea';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { ALLOWLISTS_URL, normaliseAllowlists, settingsMoreApi, type Allowlist } from '../../api/settings-more';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import './packages/cells';
import { ModalForm, generalError, sectionStyles, tableLabels } from './packages/kit';
import './packages/settings-modal';
import { allowlistBody, allowlistEntries, entryPreview, invalidEntries, validateAllowlistForm, type AllowlistForm } from './protection/models';

const FIELDS: Record<string, string> = { 'field:name': 'name', 'field:description': 'description' };

/** Einstellungen → Allowlists: benannte Listen von Adressen und Netzen für Firewall, fail2ban und CrowdSec. */
export class SettingsAllowlists extends LitElement {
  static override properties = { _dialogError: { state: true }, _delete: { state: true }, _deleting: { state: true } };
  static override styles = [baseStyles, sectionStyles];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _delete: Allowlist | null;
  declare _deleting: boolean;
  readonly #lists = new LiveData<Allowlist[]>(this, { url: () => ALLOWLISTS_URL, transform: normaliseAllowlists });
  readonly #modal = new ModalForm<AllowlistForm>(this, (modal) => {
    if (modal.kind === 'new') return { name: '', description: '', entries: '' };
    if (modal.kind !== 'edit' || !this.#lists.data) return this.#lists.loading ? undefined : null;
    const list = this.#lists.data.find((l) => l.id === modal.id);
    return list ? { name: list.name, description: list.description, entries: list.entries } : null;
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
    palette.register(this, [{ id: 'allowlist-new', key: 'n', label: t('settings.allowlists.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) }]);
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
    const id = this.#modal.editId;
    const valid = Object.keys(validateAllowlistForm(form)).length === 0;
    if (await this.#modal.submit(valid, () => settingsMoreApi.saveAllowlist(id, allowlistBody(form)), FIELDS)) {
      toast({ tone: 'ok', title: t(id ? 'settings.allowlists.notice.saved' : 'settings.allowlists.notice.created', { name: form.name.trim() }) });
    }
  }

  async #confirmDelete(): Promise<void> {
    const list = this._delete;
    if (!list) return;
    this._deleting = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.deleteAllowlist(list.id);
      toast({ tone: 'ok', title: t('settings.allowlists.notice.deleted', { name: list.name }) });
      this._delete = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._deleting = false;
    }
  }

  get #columns(): Column<Allowlist>[] {
    return [
      {
        key: 'name',
        label: t('settings.allowlists.column.name'),
        sortable: true,
        render: (r) => html`<lcm-settings-cell sub=${r.description}><strong>${r.name}</strong></lcm-settings-cell>`,
      },
      {
        key: 'count',
        label: t('settings.allowlists.column.count'),
        align: 'end',
        sortable: true,
        value: (r) => allowlistEntries(r.entries).length,
        render: (r) => html`<lcm-badge>${allowlistEntries(r.entries).length}</lcm-badge>`,
      },
      {
        key: 'preview',
        label: t('settings.allowlists.column.preview'),
        render: (r) => {
          const preview = entryPreview(r.entries);
          return html`<lcm-settings-cell mono sub=${preview.more ? t('settings.allowlists.more', { count: preview.more }) : ''}>${preview.text || '–'}</lcm-settings-cell>`;
        },
      },
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
            label=${t('settings.allowlists.delete.label')}
            @click=${(e: Event) => {
              e.stopPropagation();
              this._delete = this.#lists.data?.find((l) => String(l.id) === String(r.id)) ?? null;
            }}
          ></lcm-button>
        </lcm-settings-actions>`,
      },
    ];
  }

  #table() {
    const lists = this.#lists.data;
    if (!lists) return this.#lists.error ? html`<p class="callout warn">${this.#lists.error}</p>` : html`<lcm-skeleton lines="3"></lcm-skeleton>`;
    if (lists.length === 0) {
      return html`<lcm-empty-state level="3" icon="shield" heading=${t('settings.allowlists.empty.title')} text=${t('settings.allowlists.empty.text')}></lcm-empty-state>`;
    }
    return html`<lcm-data-table
      no-toolbar
      density="compact"
      active-id=${String(this.#modal.editId ?? '')}
      .columns=${this.#columns}
      .rows=${lists.map((l) => ({ ...l, id: String(l.id) }))}
      .labels=${tableLabels(t('settings.allowlists.caption'), t('settings.allowlists.empty.title'))}
      @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#modal.show({ kind: 'edit', id: Number(e.detail.id) })}
    ></lcm-data-table>`;
  }

  #entriesField(form: AllowlistForm, errors: ReturnType<typeof validateAllowlistForm>) {
    const bad = invalidEntries(form.entries);
    const count = allowlistEntries(form.entries).length;
    const error = this.#modal.tried && bad.length > 0 ? t('settings.allowlists.form.entriesInvalid', { list: bad.slice(0, 3).join(', ') }) : this.#modal.fieldText(errors, 'entries', LIMITS.script);
    return html`<lcm-textarea
      label=${t('settings.allowlists.form.entries')}
      width="m"
      mono
      rows="8"
      maxlength=${LIMITS.script}
      placeholder=${'10.0.0.5\n192.168.1.0/24\n2001:db8::/32'}
      hint=${t('settings.allowlists.form.entriesHint', { count })}
      error=${error}
      .value=${form.entries}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ entries: e.detail.value })}
    ></lcm-textarea>`;
  }

  #modalView() {
    const form = this.#modal.form;
    const errors = form ? validateAllowlistForm(form) : {};
    const editing = this.#modal.editId !== null;
    return html`<lcm-settings-modal
      ?open=${form !== null}
      heading=${t(editing ? 'settings.allowlists.form.editTitle' : 'settings.allowlists.form.newTitle')}
      save-label=${t(editing ? 'settings.more.modal.save' : 'settings.more.modal.create')}
      ?dirty=${this.#modal.dirty}
      ?busy=${this.#modal.busy}
      error=${generalError(this.#modal.error)}
      @lcm-submit=${() => void this.#save()}
      @lcm-close=${() => this.#modal.close()}
    >
      ${form
        ? html`<lcm-input
              label=${t('settings.allowlists.form.name')}
              placeholder=${t('settings.allowlists.form.namePlaceholder')}
              maxlength=${LIMITS.name}
              error=${this.#modal.fieldText(errors, 'name', LIMITS.name)}
              .value=${form.name}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ name: e.detail.value })}
            ></lcm-input>
            <lcm-input
              label=${t('settings.allowlists.form.description')}
              width="full"
              maxlength=${LIMITS.description}
              error=${this.#modal.fieldText(errors, 'description', LIMITS.description)}
              .value=${form.description}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#modal.set({ description: e.detail.value })}
            ></lcm-input>
            ${this.#entriesField(form, errors)}
            ${editing ? html`<p class="callout">${t('settings.allowlists.form.effect')}</p>` : nothing}`
        : nothing}
    </lcm-settings-modal>`;
  }

  #deleteDialog() {
    const list = this._delete;
    return html`<lcm-dialog
      ?open=${list !== null}
      heading=${t('settings.allowlists.delete.title', { name: list?.name ?? '' })}
      message=${t('settings.allowlists.delete.message')}
      .impacts=${[
        { label: t('settings.allowlists.delete.inUse'), value: t('settings.allowlists.delete.inUseValue'), tone: 'ok' },
        { label: t('settings.more.impact.reversible'), value: t('settings.allowlists.delete.reversible'), tone: 'warn' },
      ]}
      confirm-label=${t('settings.allowlists.delete.confirm')}
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
        <p class="lead">${t('settings.allowlists.lead')}</p>
        <lcm-card level="2" heading=${t('settings.allowlists.title')}>
          <lcm-button slot="actions" variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>${t('settings.allowlists.add')}</lcm-button>
          ${this.#table()}
        </lcm-card>
      </div>
      ${this.#modalView()} ${this.#deleteDialog()}`;
  }
}
customElements.define('lcm-settings-allowlists', SettingsAllowlists);
