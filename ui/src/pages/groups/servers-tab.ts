import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-combobox';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-os';
import '../../components/lcm-status';
import { baseStyles } from '../../components/shared';
import { toast } from '../../components/lcm-toaster';
import { SERVERS_URL, SERVER_STATUSES_URL, groupsApi, itemsOf, normalizeServerOptions, type Group, type GroupServer, type ServerOption } from '../../api/groups';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { store } from '../../lib/store';
import { STATUS_LABEL_KEYS, statusLevel } from './group-model';

type Confirm = { kind: 'add' | 'remove'; server: { id: number; name: string } };

/**
 * <lcm-groups-servers .group=${…} ?can-write> – Server der Gruppe zuordnen und entfernen. Der Status kommt
 * aus `/servers/statuses` und wird live aufgefrischt.
 */
export class LcmGroupsServers extends LitElement {
  static override properties = {
    _dialogError: { state: true },
    group: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _statuses: { state: true },
    _options: { state: true },
    _pick: { state: true },
    _confirm: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .add {
        display: flex;
        align-items: end;
        gap: var(--space-3);
        margin-block-end: var(--space-4);
      }
      .scroll {
        overflow-x: auto;
      }
      table {
        inline-size: 100%;
        border-collapse: collapse;
      }
      th {
        padding: var(--space-2) var(--space-3);
        color: var(--text-2);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        text-align: start;
      }
      td {
        padding: var(--space-2) var(--space-3);
        border-block-start: 1px solid var(--line);
      }
      td.end {
        text-align: end;
      }
      .mono {
        color: var(--text-2);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
      }
      a {
        color: var(--text-1);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      a:hover {
        text-decoration: underline;
      }
    `,
  ];

  declare group: Group;
  declare canWrite: boolean;
  /** Fehler der bestätigten Aktion; steht im Dialog über dem Fuß. */
  declare _dialogError: string;
  declare _statuses: Map<number, string>;
  declare _options: ServerOption[];
  declare _pick: string;
  declare _confirm: Confirm | null;
  declare _busy: boolean;
  #stop: Array<() => void> = [];

  constructor() {
    super();
    this._dialogError = '';
    this.group = undefined as unknown as Group;
    this.canWrite = false;
    this._statuses = new Map();
    this._options = [];
    this._pick = '';
    this._confirm = null;
    this._busy = false;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    if (!auth.can('servers:read')) return;
    const statuses = (data: unknown) =>
      (this._statuses = new Map(itemsOf(data).map((s) => [(s as { id: number }).id, (s as { status: string }).status])));
    const options = (data: unknown) => (this._options = normalizeServerOptions(data));
    this.#stop.push(store.subscribe(SERVER_STATUSES_URL, statuses), store.subscribe(SERVERS_URL, options));
    void store.get(SERVER_STATUSES_URL).then(statuses, () => {});
    void store.get(SERVERS_URL).then(options, () => {});
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stop.forEach((stop) => stop());
    this.#stop = [];
  }

  #available(): ServerOption[] {
    return this._options.filter((option) => !this.group.servers.some((server) => server.id === option.id));
  }

  async #confirmed(): Promise<void> {
    const confirm = this._confirm;
    if (!confirm) return;
    this._busy = true;
    this._dialogError = '';
    try {
      if (confirm.kind === 'add') await groupsApi.assignServer(this.group.id, confirm.server.id);
      else await groupsApi.removeServer(this.group.id, confirm.server.id);
      toast({ tone: 'ok', title: t(confirm.kind === 'add' ? 'groups.servers.added' : 'groups.servers.removed', { name: confirm.server.name }) });
      this._confirm = null;
      this._pick = '';
    } catch (error) {
      this._dialogError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #dialog() {
    const confirm = this._confirm;
    const enforce = this.group.rules.filter((rule) => rule.enforce && rule.enabled).length;
    const scheduled = this.group.rules.filter((rule) => !rule.enforce && rule.enabled).length;
    const adding = confirm?.kind === 'add';
    return html`<lcm-dialog
      ?open=${confirm !== null}
      heading=${t(adding ? 'groups.servers.addTitle' : 'groups.servers.removeTitle', { name: confirm?.server.name ?? '' })}
      message=${t(adding ? 'groups.servers.addMessage' : 'groups.servers.removeMessage', { group: this.group.name })}
      .impacts=${adding
        ? [
            { label: t('groups.servers.impactEnforce'), value: t('groups.servers.impactEnforceValue', { count: enforce }), tone: enforce > 0 ? 'warn' : 'ok' },
            { label: t('groups.servers.impactScheduled'), value: t('groups.servers.impactScheduledValue', { count: scheduled }) },
            { label: t('groups.run.reversible'), value: t('groups.run.yes'), tone: 'ok' },
          ]
        : [
            { label: t('groups.servers.impactKept'), value: t('groups.servers.impactKeptValue') },
            { label: t('groups.run.reversible'), value: t('groups.run.yes'), tone: 'ok' },
          ]}
      confirm-label=${adding ? t('groups.action.add') : t('groups.action.remove')}
      cancel-label=${t('groups.action.cancel')}
      tone=${adding ? 'default' : 'danger'}
      ?busy=${this._busy}
      error=${this._dialogError}
      @lcm-confirm=${this.#confirmed}
      @lcm-cancel=${() => (this._confirm = null)}
    ></lcm-dialog>`;
  }

  #row(server: GroupServer) {
    const level = statusLevel(this._statuses.get(server.id));
    return html`<tr>
      <td><lcm-os os=${server.os_id} label=${server.os_name}></lcm-os></td>
      <td><a href=${`/servers/${server.id}`}>${server.name}</a></td>
      <td class="mono">${server.host}</td>
      <td>${auth.can('servers:read') ? html`<lcm-status level=${level} label=${t(STATUS_LABEL_KEYS[level])}></lcm-status>` : nothing}</td>
      ${this.canWrite
        ? html`<td class="end"><lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('groups.servers.remove', { name: server.name })} @click=${() => (this._confirm = { kind: 'remove', server })}></lcm-button></td>`
        : nothing}
    </tr>`;
  }

  override render() {
    if (!this.group) return nothing;
    const servers = this.group.servers;
    const available = this.#available();
    const picked = available.find((option) => String(option.id) === this._pick);
    return html`<lcm-card heading=${t('groups.servers.title')} subtitle=${t('groups.servers.subtitle', { count: servers.length })}>
      ${this.canWrite && auth.can('servers:read')
        ? html`<div class="add">
            <lcm-combobox
              label=${t('groups.servers.add')}
              width="l"
              placeholder=${t('groups.servers.addPlaceholder')}
              empty-text=${t('groups.servers.noneLeft')}
              .options=${available.map((option) => ({ value: String(option.id), label: option.name, hint: option.host }))}
              .value=${this._pick}
              @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._pick = e.detail.value)}
            ></lcm-combobox>
            <lcm-button variant="primary" icon="plus" ?disabled=${!picked} @click=${() => picked && (this._confirm = { kind: 'add', server: picked })}>${t('groups.action.add')}</lcm-button>
          </div>`
        : nothing}
      ${servers.length === 0
        ? html`<lcm-empty-state level="3" icon="server" heading=${t('groups.servers.emptyTitle')} text=${t('groups.servers.emptyText')}></lcm-empty-state>`
        : html`<div class="scroll"><table>
            <thead><tr><th scope="col"><span class="sr-only">${t('servers.column.os')}</span></th><th scope="col">${t('groups.servers.name')}</th><th scope="col">${t('groups.servers.host')}</th><th scope="col">${t('groups.servers.status')}</th>${this.canWrite ? html`<th scope="col"><span class="sr-only">${t('app.actions')}</span></th>` : nothing}</tr></thead>
            <tbody>${servers.map((server) => this.#row(server))}</tbody>
          </table></div>`}
      ${this.#dialog()}
    </lcm-card>`;
  }
}
customElements.define('lcm-groups-servers', LcmGroupsServers);
