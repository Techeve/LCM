import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-data-table';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-skeleton';
import type { Column } from '../../components/lcm-data-table';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import {
  ALL_SERVERS_URL,
  GLOBAL_SETTINGS_URL,
  LAPIS_URL,
  consoleConfigured,
  normaliseLapis,
  normaliseServerLinks,
  settingsMoreApi,
  type Lapi,
  type ProbeStatus,
  type ServerLink,
} from '../../api/settings-more';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { SAVE_EVENT } from '../../lib/hotkeys';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { unsaved } from '../../lib/unsaved';
import './packages/cells';
import { ModalForm, generalError, sectionStyles, tableLabels } from './packages/kit';
import './packages/monitor-card';
import './packages/settings-modal';
import { LAPI_LOGIN_MAX, emptyLapiForm, lapiBody, lapiFormOf, lapiNameFor, lapiServers, validateLapiForm, type LapiForm } from './protection/models';

const FIELDS: Record<string, string> = { 'field:name': 'name', 'field:url': 'url', 'field:login': 'login', 'field:password': 'password' };

type Probe = ProbeStatus | 'checking';

/** Einstellungen → CrowdSec: zentrale LAPIs, angebundene Server, Überwachung und Console-Zugang. */
export class SettingsCrowdsec extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    _probes: { state: true },
    _delete: { state: true },
    _deleting: { state: true },
    _consoleKey: { state: true },
    _consoleBusy: { state: true },
    _consoleError: { state: true },
  };
  static override styles = [
    baseStyles,
    sectionStyles,
    css`
      .card-text {
        margin: 0 0 var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .console-foot {
        display: flex;
        justify-content: flex-end;
      }
    `,
  ];

  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _probes: Record<number, Probe>;
  declare _delete: Lapi | null;
  declare _deleting: boolean;
  declare _consoleKey: string;
  declare _consoleBusy: boolean;
  declare _consoleError: string;

  readonly #lapis = new LiveData<Lapi[]>(this, { url: () => LAPIS_URL, transform: normaliseLapis });
  readonly #console = new LiveData<boolean>(this, { url: () => GLOBAL_SETTINGS_URL, transform: consoleConfigured });
  readonly #servers = new LiveData<ServerLink[]>(this, {
    url: () => (auth.can('servers:read') ? ALL_SERVERS_URL : null),
    topics: () => (auth.can('servers:read') ? ['servers'] : []),
    transform: normaliseServerLinks,
  });
  readonly #modal = new ModalForm<LapiForm>(this, (modal) => {
    if (modal.kind === 'new') return emptyLapiForm();
    if (modal.kind !== 'edit' || !this.#lapis.data) return this.#lapis.loading ? undefined : null;
    const lapi = this.#lapis.data.find((l) => l.id === modal.id);
    return lapi ? lapiFormOf(lapi) : null;
  });

  constructor() {
    super();
    this._dialogError = '';
    this._probes = {};
    this._delete = null;
    this._deleting = false;
    this._consoleKey = '';
    this._consoleBusy = false;
    this._consoleError = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
  }

  override connectedCallback(): void {
    super.connectedCallback();
    document.addEventListener(SAVE_EVENT, this.#onSave);
    palette.register(this, [{ id: 'lapi-new', key: 'n', label: t('settings.crowdsec.add'), icon: 'plus', run: () => this.#modal.show({ kind: 'new' }) }]);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    document.removeEventListener(SAVE_EVENT, this.#onSave);
    palette.unregister(this);
    unsaved.set(this, false);
  }

  override willUpdate(): void {
    this.#modal.sync();
    unsaved.set(this, this._consoleKey !== '');
  }

  /** Strg/Cmd+S speichert den Console-Key, solange keine Fenster offen ist (die speichert selbst). */
  #onSave = (event: Event): void => {
    if (this.#modal.open || !this._consoleKey) return;
    event.preventDefault();
    void this.#saveConsole();
  };

  async #saveConsole(): Promise<void> {
    this._consoleBusy = true;
    this._consoleError = '';
    try {
      await settingsMoreApi.saveConsoleKey(this._consoleKey.trim());
      this._consoleKey = '';
      toast({ tone: 'ok', title: t('settings.crowdsec.console.saved') });
    } catch (error) {
      this._consoleError = errorText(error);
    } finally {
      this._consoleBusy = false;
    }
  }

  async #probe(lapi: Lapi): Promise<void> {
    this._probes = { ...this._probes, [lapi.id]: 'checking' };
    try {
      this._probes = { ...this._probes, [lapi.id]: await settingsMoreApi.checkLapi(lapi.id) };
    } catch (error) {
      this._probes = { ...this._probes, [lapi.id]: { reachable: false, running: false, message: errorText(error) } };
    }
  }

  async #save(): Promise<void> {
    const form = this.#modal.form;
    if (!form) return;
    const id = this.#modal.editId;
    const valid = Object.keys(validateLapiForm(form, id === null)).length === 0;
    if (await this.#modal.submit(valid, () => settingsMoreApi.saveLapi(id, lapiBody(form)), FIELDS)) {
      toast({ tone: 'ok', title: t('settings.crowdsec.notice.saved', { name: form.name.trim() }) });
    }
  }

  async #confirmDelete(): Promise<void> {
    const lapi = this._delete;
    if (!lapi) return;
    this._deleting = true;
    this._dialogError = '';
    try {
      await settingsMoreApi.deleteLapi(lapi.id);
      toast({ tone: 'ok', title: t('settings.crowdsec.notice.deleted', { name: lapi.name }) });
      this._delete = null;
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._deleting = false;
    }
  }

  #status(lapi: Lapi) {
    const probe = this._probes[lapi.id];
    if (!probe) return html`<span class="muted">–</span>`;
    if (probe === 'checking') return html`<span class="muted">${t('settings.crowdsec.status.checking')}</span>`;
    const [tone, key] = probe.running
      ? ['ok', 'settings.crowdsec.status.ok']
      : probe.reachable
        ? ['warn', 'settings.crowdsec.status.loginRejected']
        : ['crit', 'settings.crowdsec.status.down'];
    return html`<lcm-badge tone=${tone} title=${probe.message}>${t(key)}</lcm-badge>`;
  }

  get #columns(): Column<Lapi>[] {
    const servers = this.#servers.data;
    const lapis = this.#lapis.data ?? [];
    return [
      { key: 'name', label: t('settings.crowdsec.column.name'), sortable: true, render: (r) => html`<strong>${r.name}</strong>` },
      {
        key: 'url',
        label: t('settings.crowdsec.column.url'),
        render: (r) => html`<lcm-settings-cell mono sub=${t('settings.crowdsec.column.loginValue', { login: r.login })}>${r.url}</lcm-settings-cell>`,
      },
      ...(servers
        ? [{ key: 'servers', label: t('settings.crowdsec.column.servers'), align: 'end' as const, render: (r: Lapi) => String(lapiServers(servers, [r]).length) }]
        : []),
      { key: 'status', label: t('settings.crowdsec.column.status'), render: (r) => this.#status(r) },
      {
        key: 'actions',
        label: t('app.actions'),
        labelHidden: true,
        align: 'end',
        render: (r) => html`<lcm-settings-actions>
          <lcm-button
            variant="ghost"
            size="small"
            icon="refresh"
            ?busy=${this._probes[r.id] === 'checking'}
            @click=${(e: Event) => {
              e.stopPropagation();
              void this.#probe(r);
            }}
            >${t('settings.crowdsec.check')}</lcm-button
          >
          <lcm-button
            variant="ghost"
            size="small"
            icon="trash"
            icon-only
            label=${t('settings.crowdsec.delete.label')}
            @click=${(e: Event) => {
              e.stopPropagation();
              this._delete = lapis.find((l) => String(l.id) === String(r.id)) ?? null;
            }}
          ></lcm-button>
        </lcm-settings-actions>`,
      },
    ];
  }

  #lapisCard() {
    const lapis = this.#lapis.data;
    let body;
    if (!lapis) body = this.#lapis.error ? html`<p class="callout warn">${this.#lapis.error}</p>` : html`<lcm-skeleton lines="3"></lcm-skeleton>`;
    else if (lapis.length === 0) {
      body = html`<lcm-empty-state level="3" icon="shield" heading=${t('settings.crowdsec.empty.title')} text=${t('settings.crowdsec.empty.text')}></lcm-empty-state>`;
    } else {
      body = html`<lcm-data-table
        no-toolbar
        density="compact"
        active-id=${String(this.#modal.editId ?? '')}
        .columns=${this.#columns}
        .rows=${lapis.map((l) => ({ ...l, id: String(l.id) }))}
        .labels=${tableLabels(t('settings.crowdsec.caption'), t('settings.crowdsec.empty.title'))}
        @lcm-row-activate=${(e: CustomEvent<{ id: string }>) => this.#modal.show({ kind: 'edit', id: Number(e.detail.id) })}
      ></lcm-data-table>`;
    }
    return html`<lcm-card level="2" heading=${t('settings.crowdsec.lapis')}>
      <lcm-button slot="actions" variant="primary" size="small" icon="plus" @click=${() => this.#modal.show({ kind: 'new' })}>${t('settings.crowdsec.add')}</lcm-button>
      <p class="card-text">${t('settings.crowdsec.lapisIntro')}</p>
      ${body}
    </lcm-card>`;
  }

  #connectedCard() {
    const lapis = this.#lapis.data ?? [];
    if (!auth.can('servers:read') || lapis.length === 0) return nothing;
    const servers = this.#servers.data;
    const connected = servers ? lapiServers(servers, lapis) : [];
    const columns: Column<ServerLink>[] = [
      { key: 'name', label: t('settings.crowdsec.connected.server'), sortable: true, render: (r) => html`<lcm-settings-cell><a href=${`/servers/${r.id}`}>${r.name}</a></lcm-settings-cell>` },
      { key: 'host', label: t('settings.crowdsec.connected.host'), kind: 'mono' },
      { key: 'lapi', label: t('settings.crowdsec.connected.lapi'), render: (r) => lapiNameFor(lapis, r.crowdsec_lapi_url) },
      { key: 'mode', label: t('settings.crowdsec.connected.mode'), render: (r) => (r.crowdsec_lapi_mode ? html`<lcm-badge>${r.crowdsec_lapi_mode}</lcm-badge>` : '–') },
      {
        key: 'active',
        label: t('settings.crowdsec.connected.service'),
        render: (r) =>
          r.crowdsec_active
            ? html`<lcm-badge tone="ok">${t('settings.crowdsec.connected.active')}</lcm-badge>`
            : html`<lcm-badge tone="warn">${t('settings.crowdsec.connected.inactive')}</lcm-badge>`,
      },
    ];
    return html`<lcm-card level="2" heading=${t('settings.crowdsec.connected.title')}>
      <p class="card-text">${t('settings.crowdsec.connected.intro')}</p>
      ${!servers
        ? html`<lcm-skeleton lines="2"></lcm-skeleton>`
        : connected.length === 0
          ? html`<p class="card-text">${t('settings.crowdsec.connected.none')}</p>`
          : html`<lcm-data-table
              no-toolbar
              density="compact"
              .columns=${columns}
              .rows=${connected.map((s) => ({ ...s, id: String(s.id) }))}
              .labels=${tableLabels(t('settings.crowdsec.connected.title'), t('settings.crowdsec.connected.none'))}
            ></lcm-data-table>`}
    </lcm-card>`;
  }

  #consoleCard() {
    const configured = this.#console.data ?? false;
    return html`<lcm-card level="2" heading=${t('settings.crowdsec.console.title')}>
      ${configured ? html`<lcm-badge slot="actions" tone="ok">${t('settings.crowdsec.console.configured')}</lcm-badge>` : nothing}
      <p class="card-text">${t('settings.crowdsec.console.intro')}</p>
      <lcm-input
          label=${t('settings.crowdsec.console.key')}
          width="full"
          type="password"
          maxlength=${LIMITS.password}
          autocomplete="off"
          mono
          placeholder=${configured ? t('settings.crowdsec.console.unchanged') : ''}
          hint=${t('settings.crowdsec.console.hint')}
          error=${this._consoleError}
          .value=${this._consoleKey}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => {
            this._consoleKey = e.detail.value;
            this._consoleError = '';
          }}
        ></lcm-input>
      <div slot="footer" class="console-foot">
        <lcm-button variant="primary" ?disabled=${!this._consoleKey.trim()} ?busy=${this._consoleBusy} @click=${() => void this.#saveConsole()}>
          ${t('settings.crowdsec.console.save')}
        </lcm-button>
      </div>
    </lcm-card>`;
  }

  #modalView() {
    const form = this.#modal.form;
    const isNew = this.#modal.editId === null;
    const errors = form ? validateLapiForm(form, isNew) : {};
    const set = (patch: Partial<LapiForm>) => this.#modal.set(patch);
    return html`<lcm-settings-modal
      ?open=${form !== null}
      heading=${t(isNew ? 'settings.crowdsec.form.newTitle' : 'settings.crowdsec.form.editTitle')}
      save-label=${t(isNew ? 'settings.more.modal.create' : 'settings.more.modal.save')}
      ?dirty=${this.#modal.dirty}
      ?busy=${this.#modal.busy}
      error=${generalError(this.#modal.error)}
      @lcm-submit=${() => void this.#save()}
      @lcm-close=${() => this.#modal.close()}
    >
      ${form
        ? html`<lcm-input
              label=${t('settings.crowdsec.form.name')}
              placeholder=${t('settings.crowdsec.form.namePlaceholder')}
              maxlength=${LIMITS.name}
              error=${this.#modal.fieldText(errors, 'name')}
              .value=${form.name}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => set({ name: e.detail.value })}
            ></lcm-input>
            <lcm-input
              label=${t('settings.crowdsec.form.url')}
              width="l"
              mono
              placeholder=${'http://lapi.example:8080'}
              maxlength=${LIMITS.path}
              error=${this.#modal.fieldText(errors, 'url')}
              .value=${form.url}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => set({ url: e.detail.value })}
            ></lcm-input>
            <lcm-input
                label=${t('settings.crowdsec.form.login')}
                mono
                maxlength=${LAPI_LOGIN_MAX}
                error=${this.#modal.fieldText(errors, 'login')}
                .value=${form.login}
                @lcm-input=${(e: CustomEvent<{ value: string }>) => set({ login: e.detail.value })}
              ></lcm-input>
              <lcm-input
                label=${t('settings.crowdsec.form.password')}
                type="password"
                autocomplete="new-password"
                maxlength=${LIMITS.password}
                placeholder=${isNew ? '' : t('settings.crowdsec.console.unchanged')}
                error=${this.#modal.fieldText(errors, 'password', LIMITS.password)}
                .value=${form.password}
                @lcm-input=${(e: CustomEvent<{ value: string }>) => set({ password: e.detail.value })}
              ></lcm-input>
            <p class="callout">${t('settings.crowdsec.form.hint')}</p>`
        : nothing}
    </lcm-settings-modal>`;
  }

  #deleteDialog() {
    const lapi = this._delete;
    const servers = lapi && this.#servers.data ? lapiServers(this.#servers.data, [lapi]).length : 0;
    return html`<lcm-dialog
      ?open=${lapi !== null}
      heading=${t('settings.crowdsec.delete.title', { name: lapi?.name ?? '' })}
      message=${t('settings.crowdsec.delete.message')}
      .impacts=${[
        { label: t('settings.crowdsec.delete.servers'), value: t('settings.crowdsec.delete.serversValue', { count: servers }), tone: 'ok' },
        { label: t('settings.crowdsec.delete.secret'), value: t('settings.crowdsec.delete.secretValue'), tone: 'warn' },
      ]}
      confirm-label=${t('settings.crowdsec.delete.confirm')}
      cancel-label=${t('settings.more.modal.cancel')}
      tone="danger"
      error=${this._dialogError}
      ?busy=${this._deleting}
      @lcm-confirm=${() => void this.#confirmDelete()}
      @lcm-cancel=${() => (this._delete = null)}
    ></lcm-dialog>`;
  }

  override render() {
    const hasLapis = (this.#lapis.data?.length ?? 0) > 0;
    return html`<div class="main">
        <p class="lead">${t('settings.crowdsec.lead')}</p>
        ${this.#lapisCard()} ${this.#connectedCard()}
        ${hasLapis
          ? html`<lcm-settings-monitor type="crowdsec_lapi_down" intro=${t('settings.crowdsec.monitorIntro')} rule-name=${t('settings.crowdsec.monitorRule')}></lcm-settings-monitor>`
          : nothing}
        ${this.#consoleCard()}
      </div>
      ${this.#modalView()} ${this.#deleteDialog()}`;
  }
}
customElements.define('lcm-settings-crowdsec', SettingsCrowdsec);
