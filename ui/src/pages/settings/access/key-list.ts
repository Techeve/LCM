import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../../components/lcm-badge';
import '../../../components/lcm-button';
import '../../../components/lcm-card';
import '../../../components/lcm-data-table';
import '../../../components/lcm-dialog';
import '../../../components/lcm-input';
import '../../../components/lcm-notice';
import '../../../components/lcm-select';
import '../../../components/lcm-server-action';
import type { Column, Row } from '../../../components/lcm-data-table';
import type { Impact } from '../../../components/lcm-dialog';
import { toast } from '../../../components/lcm-toaster';
import { baseStyles } from '../../../components/shared';
import { API_KEYS_URL, USERS_URL, readApiKeys, readUsers, settingsApi, type ApiKey, type CreatedKey, type KeyScope, type LoginUser } from '../../../api/settings';
import { ApiError } from '../../../lib/api';
import { auth } from '../../../lib/auth';
import { I18nController } from '../../../lib/controllers';
import { formatDate, formatDateTime, formatRelative } from '../../../lib/format';
import { errorText, t } from '../../../lib/i18n';
import { LIMITS } from '../../../lib/validate';
import { LiveData } from '../../../lib/live-data';
import { emptyKeyForm, expiryDays, keyState, keysFor, validateKeyForm, type KeyForm, type KeyFormErrors, type KeyState } from './keys-model';
import '../system/copy-field';
import { helpLabels } from '../system/help';
import { settingsStyles } from '../system/settings-styles';
import { tableLabels } from '../system/table-labels';

const STATE_TONE: Record<KeyState, 'ok' | 'crit' | 'neutral'> = { active: 'ok', revoked: 'crit', expired: 'neutral' };
const STATE_LABEL: Record<KeyState, string> = {
  active: 'settings.apikeys.state.active',
  revoked: 'settings.apikeys.state.revoked',
  expired: 'settings.apikeys.state.expired',
};
const SCOPE_LABEL: Record<KeyScope, string> = {
  read: 'settings.apikeys.scope.read',
  readwrite: 'settings.apikeys.scope.readwrite',
  mcp: 'settings.apikeys.scope.mcp',
};

/**
 * <lcm-settings-key-list mode="rest|mcp"> – API-Schlüssel anlegen, einmal anzeigen und widerrufen. Die REST-Seite
 * zeigt Lese- und Schreibschlüssel, die MCP-Seite nur MCP-Schlüssel. Der Klartext steht nur in der Antwort auf das
 * Anlegen und bleibt hier, bis du ihn wegklickst oder die Seite verlässt. Mit `aside` steht der Inhalt des Slots
 * `aside` (etwa eine Erklärspalte) rechts neben Formular und Tabelle.
 */
export class LcmSettingsKeyList extends LitElement {
  static override properties = {
    mode: { type: String },
    aside: { type: Boolean },
    _form: { state: true },
    _errors: { state: true },
    _formError: { state: true },
    _busy: { state: true },
    _created: { state: true },
    _revoking: { state: true },
    _dialogError: { state: true },
  };
  static override styles = [
    baseStyles,
    settingsStyles,
    css`
      .create .submit {
        padding-block-start: calc(var(--fs-s) * 1.5 + var(--space-1));
      }
      @media (max-width: 75rem) {
        ::slotted(lcm-explainer) {
          grid-row: 1;
          inline-size: auto;
          position: static;
        }
      }
    `,
  ];

  declare mode: 'rest' | 'mcp';
  declare aside: boolean;
  declare _form: KeyForm;
  declare _errors: KeyFormErrors;
  /** Fehler beim Anlegen, der zu keinem Feld gehört; steht unter dem Formular am Knopf. */
  declare _formError: string;
  declare _busy: boolean;
  declare _created: CreatedKey | undefined;
  declare _revoking: ApiKey | undefined;
  declare _dialogError: string;

  readonly #keys = new LiveData<ApiKey[]>(this, { url: () => API_KEYS_URL, transform: readApiKeys });
  readonly #users = new LiveData<LoginUser[]>(this, { url: () => (auth.can('users:read') ? USERS_URL : null), transform: readUsers });

  constructor() {
    super();
    this.mode = 'rest';
    this.aside = false;
    this._form = emptyKeyForm('readwrite');
    this._errors = {};
    this._formError = '';
    this._busy = false;
    this._created = undefined;
    this._revoking = undefined;
    this._dialogError = '';
    new I18nController(this);
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('mode')) this._form = emptyKeyForm(this.#defaultScope);
  }

  get #defaultScope(): KeyScope {
    return this.mode === 'mcp' ? 'mcp' : 'readwrite';
  }

  #set(patch: Partial<KeyForm>): void {
    this._form = { ...this._form, ...patch };
    this._errors = {};
  }

  async #create(): Promise<void> {
    if (this._busy) return;
    const errors = validateKeyForm(this._form);
    this._errors = errors;
    this._formError = '';
    if (Object.keys(errors).length > 0) return;
    this._busy = true;
    try {
      this._created = await settingsApi.createApiKey(this._form.name.trim(), this._form.scope, expiryDays(this._form));
      this._form = emptyKeyForm(this.#defaultScope);
    } catch (error) {
      this.#showError(error);
    } finally {
      this._busy = false;
    }
  }

  #showError(error: unknown): void {
    const message = errorText(error);
    if (error instanceof ApiError && error.code === 'apikey.invalid_expiry') this._errors = { days: message };
    else if (error instanceof ApiError && (error.params.field === 'name' || error.code === 'request.missing_fields')) this._errors = { name: message };
    else this._formError = message;
  }

  #onKeyDown(event: KeyboardEvent): void {
    const target = event.composedPath()[0];
    if (event.key !== 'Enter' || !(target instanceof HTMLInputElement)) return;
    event.preventDefault();
    event.stopPropagation(); // sonst speichert auch das Formular, in dem die Liste steht (MCP)
    void this.#create();
  }

  async #revoke(key: ApiKey): Promise<void> {
    this._busy = true;
    this._dialogError = '';
    try {
      await settingsApi.revokeApiKey(key.id);
      if (this._created?.key.id === key.id) this._created = undefined;
      this._revoking = undefined;
      toast({ tone: 'ok', title: t('settings.apikeys.revoke.done', { name: key.name }) });
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #scopeOptions() {
    return [
      { value: 'readwrite', label: t('settings.apikeys.scope.readwrite') },
      { value: 'read', label: t('settings.apikeys.scope.read') },
    ];
  }

  #message(key: string | undefined): string {
    if (!key) return '';
    return key.startsWith('settings.') || key.startsWith('check.') ? t(key, { max: LIMITS.name }) : key;
  }

  #createForm(): TemplateResult {
    const form = this._form;
    const mcp = this.mode === 'mcp';
    return html`<div class="create field-row" @keydown=${this.#onKeyDown}>
      <lcm-input
        label=${t('settings.apikeys.form.name')}
        placeholder=${mcp ? t('settings.apikeys.form.namePlaceholderMcp') : t('settings.apikeys.form.namePlaceholder')}
        error=${this.#message(this._errors.name)}
        maxlength=${LIMITS.name}
        autocomplete="off"
        .value=${form.name}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ name: e.detail.value })}
      ></lcm-input>
      ${mcp
        ? nothing
        : html`<lcm-select
            label=${t('settings.apikeys.form.scope')}
            .options=${this.#scopeOptions()}
            .value=${form.scope}
            @lcm-change=${(e: CustomEvent<{ value: string }>) => this.#set({ scope: e.detail.value === 'read' ? 'read' : 'readwrite' })}
          ></lcm-select>`}
      <lcm-input
        label=${t('settings.apikeys.form.days')}
        width="s"
        digits
        maxlength=${String(LIMITS.apiKeyDays).length}
        mono
        placeholder=${t('settings.apikeys.form.daysPlaceholder')}
        error=${this.#message(this._errors.days)}
        .value=${form.days}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set({ days: e.detail.value })}
      ></lcm-input>
      <span class="submit">
        <lcm-server-action
          variant="primary"
          icon="key"
          label=${t('settings.apikeys.form.create')}
          help=${mcp ? t('settings.apikeys.form.createHelpMcp') : t('settings.apikeys.form.createHelp', { scope: t(SCOPE_LABEL[form.scope]) })}
          meaning=${t('settings.apikeys.form.createMeaning')}
          .labels=${helpLabels()}
          ?busy=${this._busy && !this._revoking}
          @click=${() => void this.#create()}
        ></lcm-server-action>
      </span>
    </div>
    ${this._formError ? html`<lcm-notice class="spaced-top" tone="crit">${this._formError}</lcm-notice>` : nothing}`;
  }

  #createdNote(): TemplateResult | typeof nothing {
    const created = this._created;
    if (!created) return nothing;
    return html`<div class="fields spaced-top" role="status">
      <lcm-notice tone="warn" heading=${t('settings.apikeys.created.title', { name: created.key.name })}>${t('settings.apikeys.created.text')}</lcm-notice>
      <lcm-copy-field block value=${created.secret}></lcm-copy-field>
      <div class="row end">
        <lcm-button variant="ghost" size="small" icon="check" @click=${() => (this._created = undefined)}>${t('settings.apikeys.created.done')}</lcm-button>
      </div>
    </div>`;
  }

  /** Die Spalte „Erstellt von“ lohnt nur, wenn die Schlüssel von mehr als einem Konto stammen. */
  #severalOwners(): boolean {
    if (!auth.can('users:read')) return false;
    return new Set(keysFor(this.#keys.data ?? [], this.mode).map((key) => key.userId)).size > 1;
  }

  #ownerName(id: number): string {
    const user = this.#users.data?.find((candidate) => candidate.id === id);
    return user?.username ?? (id ? `#${id}` : '');
  }

  get #columns(): Column[] {
    const columns: Column[] = [
      { key: 'name', label: t('settings.apikeys.col.name'), sortable: true, locked: true, render: (row) => html`<strong>${String(row.name)}</strong> <code>${String(row.prefix)}…</code>` },
    ];
    if (this.mode === 'rest') {
      columns.push({
        key: 'scope',
        label: t('settings.apikeys.col.scope'),
        sortable: true,
        render: (row) => html`<lcm-badge tone=${row.scope === 'read' ? 'info' : 'accent'}>${t(SCOPE_LABEL[row.scope as KeyScope])}</lcm-badge>`,
      });
    }
    if (this.#severalOwners()) columns.push({ key: 'owner', label: t('settings.apikeys.col.owner'), sortable: true });
    columns.push(
      { key: 'lastUsed', label: t('settings.apikeys.col.lastUsed'), sortable: true, sortValue: (row) => String(row.lastUsed ?? ''), render: (row) => this.#when(row.lastUsed as string | null, t('settings.apikeys.never')) },
      { key: 'expires', label: t('settings.apikeys.col.expires'), sortable: true, sortValue: (row) => String(row.expires ?? '9'), render: (row) => (row.expires ? html`<span title=${formatDateTime(row.expires as string)}>${formatDate(row.expires as string)}</span>` : html`<span class="muted">${t('settings.apikeys.noExpiry')}</span>`) },
      { key: 'state', label: t('settings.apikeys.col.state'), sortable: true, render: (row) => html`<lcm-badge tone=${STATE_TONE[row.state as KeyState]}>${t(STATE_LABEL[row.state as KeyState])}</lcm-badge>` },
      { key: 'actions', label: t('settings.apikeys.col.actions'), align: 'end', locked: true, render: (row) => this.#rowAction(row.key as ApiKey, row.state as KeyState) },
    );
    return columns;
  }

  #when(value: string | null, fallback: string): TemplateResult {
    return value ? html`<span title=${formatDateTime(value)}>${formatRelative(value)}</span>` : html`<span class="muted">${fallback}</span>`;
  }

  #rowAction(key: ApiKey, state: KeyState): TemplateResult | string {
    if (state === 'revoked') return '';
    return html`<lcm-button
      variant="ghost"
      size="small"
      icon="trash"
      icon-only
      label=${t('settings.apikeys.revoke.labelFor', { name: key.name })}
      @click=${() => {
        this._dialogError = '';
        this._revoking = key;
      }}
    ></lcm-button>`;
  }

  #revokeDialog(): TemplateResult | typeof nothing {
    const key = this._revoking;
    if (!key) return nothing;
    const impacts: Impact[] = [
      { label: t('settings.apikeys.revoke.impactKey'), value: `${key.name} (${key.prefix}…)` },
      { label: t('settings.apikeys.revoke.impactClients'), value: t(this.mode === 'mcp' ? 'settings.apikeys.revoke.impactClientsMcp' : 'settings.apikeys.revoke.impactClientsValue'), tone: 'crit' },
      { label: t('settings.apikeys.revoke.impactLastUsed'), value: key.lastUsedAt ? formatDateTime(key.lastUsedAt) : t('settings.apikeys.never') },
      { label: t('settings.apikeys.revoke.impactReversible'), value: t('settings.apikeys.revoke.impactReversibleValue'), tone: 'crit' },
    ];
    return html`<lcm-dialog
      open
      tone="danger"
      heading=${t('settings.apikeys.revoke.title', { name: key.name })}
      message=${t('settings.apikeys.revoke.message')}
      .impacts=${impacts}
      expect=${key.name}
      expect-label=${t('settings.apikeys.revoke.expect', { name: key.name })}
      confirm-label=${t('settings.apikeys.revoke.confirm')}
      cancel-label=${t('settings.form.cancel')}
      ?busy=${this._busy}
      error=${this._dialogError}
      @lcm-confirm=${() => void this.#revoke(key)}
      @lcm-cancel=${() => (this._revoking = undefined)}
    ></lcm-dialog>`;
  }

  #rows(): Row[] {
    const order: Record<KeyState, number> = { active: 0, expired: 1, revoked: 2 };
    return keysFor(this.#keys.data ?? [], this.mode)
      .map((key) => ({ key, state: keyState(key) }))
      .sort((a, b) => order[a.state] - order[b.state])
      .map(({ key, state }) => ({
        id: String(key.id),
        name: key.name,
        prefix: key.prefix,
        scope: key.scope,
        owner: this.#ownerName(key.userId),
        lastUsed: key.lastUsedAt,
        expires: key.expiresAt,
        state,
        key,
      }));
  }

  override render() {
    const mcp = this.mode === 'mcp';
    const title = mcp ? t('settings.apikeys.list.titleMcp') : t('settings.apikeys.list.title');
    return html`<div class=${this.aside ? 'layout explain' : 'layout'}>
        <div class="stack">
          <lcm-card level="2" heading=${mcp ? t('settings.apikeys.create.titleMcp') : t('settings.apikeys.create.title')} subtitle=${mcp ? t('settings.apikeys.create.subtitleMcp') : t('settings.apikeys.create.subtitle')}>
            ${this.#createForm()} ${this.#createdNote()}
          </lcm-card>
          <lcm-card level="2" heading=${title}>
            ${this.#keys.error && !this.#keys.data
              ? html`<lcm-notice tone="crit">${this.#keys.error}</lcm-notice>`
              : html`<lcm-data-table density="compact" no-toolbar no-chooser .columns=${this.#columns} .rows=${this.#rows()} .labels=${tableLabels(title, mcp ? t('settings.apikeys.list.emptyMcp') : t('settings.apikeys.list.empty'))}></lcm-data-table>`}
          </lcm-card>
        </div>
        ${this.aside ? html`<slot name="aside"></slot>` : nothing}
      </div>
      ${this.#revokeDialog()}`;
  }
}
customElements.define('lcm-settings-key-list', LcmSettingsKeyList);
