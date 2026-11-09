import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-empty-state';
import '../../components/lcm-input';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import { toast } from '../../components/lcm-toaster';
import { baseStyles, utilStyles } from '../../components/shared';
import { normalizeGroups, type Group } from '../../api/groups';
import { GROUPS_URL, refreshServer, type ServerItem } from '../../api/servers';
import { filterByName, groupChanges, groupsApi } from '../../api/server-detail';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { router } from '../../lib/router';
import { store } from '../../lib/store';


/**
 * `<lcm-srv-groups .server>`: die Gruppen des Servers als Chips und „Gruppen zuordnen“ (Recht `groups:write`, wie im
 * Backend). Das Fenster ist offen, solange `?groups=1` in der Adresse steht; gespeichert wird als Unterschied zur
 * bisherigen Zuordnung (hinzufügen, entfernen).
 */
export class SrvGroups extends LitElement {
  static override properties = {
    _modalError: { state: true },
    server: { attribute: false },
    _chosen: { state: true },
    _query: { state: true },
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
      .chips {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .chips a {
        display: inline-flex;
        padding: var(--space-1) var(--space-3);
        border: 1px solid var(--line);
        border-radius: var(--radius-pill);
        background: var(--surface-1);
        color: var(--text-1);
        font-size: var(--fs-s);
        text-decoration: none;
      }
      .chips a:hover {
        background: var(--accent-soft);
        color: var(--accent-text);
      }
      .muted {
        margin: 0;
      }
      .list {
        display: flex;
        flex-direction: column;
        max-block-size: 22rem;
        margin-block-start: var(--space-3);
        overflow: auto;
      }
      label {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        padding: var(--space-2) var(--space-2);
        border-radius: var(--radius-s);
        cursor: pointer;
      }
      label:hover {
        background: var(--surface-1);
      }
      input[type='checkbox'] {
        inline-size: 1rem;
        block-size: 1rem;
        margin: 0;
        accent-color: var(--accent);
      }
      .desc {
        margin-inline-start: auto;
        color: var(--text-2);
        font-size: var(--fs-s);
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        max-inline-size: 50%;
      }
      .summary {
        margin: var(--space-3) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare server: ServerItem;
  /** Fehler der letzten Aktion im Fenster; steht über dem Fuß. */
  declare _modalError: string;
  declare _chosen: number[] | null;
  declare _query: string;
  declare _busy: boolean;

  readonly #groups = new LiveData<Group[]>(this, {
    url: () => (this.#open ? GROUPS_URL : null),
    topics: () => ['servers'],
    transform: normalizeGroups,
  });

  constructor() {
    super();
    this._modalError = '';
    this._chosen = null;
    this._query = '';
    this._busy = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  get #open(): boolean {
    return router.state.query.get('groups') === '1' && auth.can('groups:write');
  }

  get #current(): number[] {
    return this.server.groups.map((g) => g.id);
  }

  /** Öffnet das Fenster (auch aus der Befehlspalette). */
  open(): void {
    this._chosen = null;
    this._query = '';
    router.setQuery({ groups: 1 });
  }

  #close(): void {
    this._modalError = '';
    router.setQuery({ groups: null }, { replace: true });
  }

  #toggle(id: number, checked: boolean): void {
    const chosen = this._chosen ?? this.#current;
    this._chosen = checked ? [...chosen, id] : chosen.filter((g) => g !== id);
  }

  async #save(): Promise<void> {
    const changes = groupChanges(this.#current, this._chosen ?? this.#current);
    const id = this.server.id;
    this._busy = true;
    this._modalError = '';
    try {
      for (const groupId of changes.add) await groupsApi.assign(groupId, id);
      for (const groupId of changes.remove) await groupsApi.remove(groupId, id);
      toast({ tone: 'ok', title: t('serverDetail.groups.saved', { name: this.server.name }) });
      store.invalidate(GROUPS_URL);
      refreshServer(id);
      this.#close();
    } catch (error) {
      this._modalError = errorText(error);
      store.invalidate(GROUPS_URL);
      refreshServer(id);
    } finally {
      this._busy = false;
    }
  }

  #renderList() {
    const groups = this.#groups.data;
    if (!groups) {
      return this.#groups.error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${this.#groups.error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="5" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    if (groups.length === 0) return html`<p class="muted">${t('serverDetail.groups.noneExist')}</p>`;
    const chosen = this._chosen ?? this.#current;
    const shown = filterByName(groups, this._query);
    return html`<div class="list" role="group" aria-label=${t('serverDetail.groups.listLabel')}>
      ${shown.length === 0 ? html`<p class="muted">${t('serverDetail.groups.noMatch')}</p>` : nothing}
      ${shown.map(
        (group) => html`<label>
          <input type="checkbox" .checked=${chosen.includes(group.id)} @change=${(e: Event) => this.#toggle(group.id, (e.target as HTMLInputElement).checked)} />
          <span>${group.name}</span>
          ${group.description ? html`<span class="desc" title=${group.description}>${group.description}</span>` : nothing}
        </label>`,
      )}
    </div>`;
  }

  #renderModal() {
    const changes = groupChanges(this.#current, this._chosen ?? this.#current);
    const dirty = changes.add.length + changes.remove.length > 0;
    return html`<lcm-modal ?open=${this.#open} size="m" heading=${t('serverDetail.groups.modalTitle', { name: this.server.name })} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => this.#close()}>
      <lcm-input maxlength=${LIMITS.query} width="full" label=${t('serverDetail.groups.search')} type="search" icon="search" .value=${this._query} @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._query = e.detail.value)}></lcm-input>
      ${this.#renderList()}
      ${dirty ? html`<p class="summary" role="status">${t('serverDetail.groups.changes', { add: changes.add.length, remove: changes.remove.length })}</p>` : nothing}
      ${this._modalError ? html`<lcm-notice slot="note" tone="crit">${this._modalError}</lcm-notice>` : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t('servers.confirm.cancel')}</lcm-button>
      <lcm-button slot="footer" variant="primary" ?busy=${this._busy} ?disabled=${!dirty} @click=${() => void this.#save()}>${t('serverDetail.groups.save')}</lcm-button>
    </lcm-modal>`;
  }

  override render() {
    const groups = this.server.groups;
    const write = auth.can('groups:write');
    return html`<lcm-card heading=${t('serverDetail.groups.title')}>
      ${write ? html`<lcm-button slot="actions" size="small" variant="ghost" icon="edit" @click=${() => this.open()}>${t('serverDetail.groups.assign')}</lcm-button>` : nothing}
      ${groups.length > 0
        ? html`<ul class="chips">${groups.map((g) => html`<li><a href=${`/groups/${g.id}`}>${g.name}</a></li>`)}</ul>`
        : html`<p class="muted">${t('serverDetail.groups.none')}</p>`}
    </lcm-card>
    ${write ? this.#renderModal() : nothing}`;
  }
}
customElements.define('lcm-srv-groups', SrvGroups);
