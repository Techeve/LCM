import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-empty-state';
import '../../components/lcm-explainer';
import '../../components/lcm-input';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import '../../components/lcm-tabs';
import '../../components/lcm-term';
import type { TabItem } from '../../components/lcm-tabs';
import { baseStyles } from '../../components/shared';
import { GROUPS_URL, normalizeGroups, type Group } from '../../api/groups';
import {
  BLOCKS_URL,
  LINUX_USERS_URL,
  PROFILES_URL,
  normalizeBlocks,
  normalizeProfiles,
  normalizeUsers,
  type Block,
  type LinuxUser,
  type Profile,
} from '../../api/linux-users';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import { LiveData } from '../../lib/live-data';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { filterBlocks, parseSource, type SourceFilter } from './block-model';
import { cloneBlock, cloneProfile } from './copy-actions';
import { parseView } from './lu-paging';
import { filterProfiles } from './profile-model';
import { filterUsers } from './user-model';
import { modalQuery, parseModal, parseSection, sectionPath, type Modal, type Section } from './url-state';
import './block-modal';
import './blocks-section';
import './profile-modal';
import './profiles-section';
import './user-modal';
import './users-section';

interface SectionInfo {
  id: Section;
  labelKey: string;
  read: string;
  write: string;
}

const SECTION_INFO: SectionInfo[] = [
  { id: 'users', labelKey: 'linuxUsers.tabs.users', read: 'linuxusers:read', write: 'linuxusers:write' },
  { id: 'profiles', labelKey: 'linuxUsers.tabs.profiles', read: 'profiles:read', write: 'profiles:write' },
  { id: 'profile-blocks', labelKey: 'linuxUsers.tabs.blocks', read: 'profiles:read', write: 'profiles:write' },
];

/** Die Namen der Schaltflächen zum Anlegen je Abschnitt. */
const CREATE_LABEL: Record<Section, string> = {
  users: 'linuxUsers.users.create',
  profiles: 'profiles.create',
  'profile-blocks': 'profileBlocks.create',
};

const SEARCH_PLACEHOLDER: Record<Section, string> = {
  users: 'linuxUsers.search.users',
  profiles: 'linuxUsers.search.profiles',
  'profile-blocks': 'linuxUsers.search.blocks',
};

/**
 * Linux-Benutzer: Reiter Benutzer, Berechtigungsprofile und Regelbausteine, im Pfad (`/linux-users/profiles`).
 * Suche, Herkunftsfilter und offene Fenster stehen in der Query. Auf Profilen und Bausteinen erklärt eine
 * Spalte, wie beides zusammenspielt und was auf den Servern entsteht.
 */
export class LinuxUsersPage extends LitElement {
  static override properties = { _copying: { state: true } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .head {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3) var(--space-5);
        margin-block-end: var(--space-5);
      }
      h1 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .tools {
        display: flex;
        flex: 1;
        flex-wrap: wrap;
        align-items: end;
        justify-content: flex-end;
        gap: var(--space-3);
      }
      .tools lcm-input {
        flex: 1;
        min-inline-size: 14rem;
        max-inline-size: 24rem;
      }
      .tools lcm-select {
        inline-size: 12rem;
      }
      .layout {
        display: grid;
        grid-template-columns: minmax(0, 1fr);
        gap: var(--gap);
        align-items: start;
      }
      .layout.explain {
        grid-template-columns: minmax(0, 1fr) auto;
      }
      .content {
        min-inline-size: 0;
      }
      lcm-explainer ul {
        margin: 0 0 var(--space-3);
        padding-inline-start: 1.1rem;
      }
      lcm-explainer li {
        margin-block-end: var(--space-2);
      }
      @media (max-width: 75rem) {
        .layout.explain {
          grid-template-columns: minmax(0, 1fr);
        }
        .layout.explain lcm-explainer {
          inline-size: auto;
        }
      }
    `,
  ];

  declare _copying: boolean;

  readonly #users = new LiveData<LinuxUser[]>(this, { url: () => (auth.can('linuxusers:read') ? LINUX_USERS_URL : null), transform: normalizeUsers });
  readonly #profiles = new LiveData<Profile[]>(this, { url: () => (auth.can('profiles:read') ? PROFILES_URL : null), transform: normalizeProfiles });
  readonly #blocks = new LiveData<Block[]>(this, { url: () => (auth.can('profiles:read') ? BLOCKS_URL : null), transform: normalizeBlocks });
  readonly #groups = new LiveData<Group[]>(this, { url: () => (auth.can('linuxusers:read') && auth.can('groups:read') ? GROUPS_URL : null), transform: normalizeGroups });

  constructor() {
    super();
    this._copying = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    palette.unregister(this);
  }

  get #sections(): SectionInfo[] {
    return SECTION_INFO.filter((s) => auth.can(s.read));
  }

  /** Der gewählte Abschnitt; darf der Benutzer ihn nicht sehen, der erste erlaubte. */
  get #section(): Section | null {
    const wanted = parseSection(router.state.match?.params.section);
    return this.#sections.find((s) => s.id === wanted)?.id ?? this.#sections[0]?.id ?? null;
  }

  get #query(): string {
    return router.state.query.get('q') ?? '';
  }

  get #source(): SourceFilter {
    return parseSource(router.state.query.get('source'));
  }

  get #modal(): Modal {
    return parseModal(router.state.query);
  }

  #canWrite(section: Section): boolean {
    return auth.can(SECTION_INFO.find((s) => s.id === section)?.write ?? '');
  }

  #openModal(modal: Modal): void {
    router.setQuery(modalQuery(modal));
  }

  #closeModal(): void {
    router.setQuery(modalQuery({ kind: 'none' }), { replace: true });
  }

  #registerPalette(section: Section | null): void {
    palette.register(
      this,
      section && this.#canWrite(section)
        ? [{ id: 'linux-users:create', key: 'n', label: t(CREATE_LABEL[section]), icon: 'plus' as const, run: () => this.#openModal({ kind: 'new' }) }]
        : [],
    );
  }

  override updated(): void {
    this.#registerPalette(this.#section);
  }

  #gotoSection(id: string): void {
    void router.navigate(sectionPath(parseSection(id)));
  }

  // ---- Inhalte ----

  #loading(data: unknown, error: string) {
    if (data !== undefined) return null;
    return error
      ? html`<lcm-empty-state level="2" icon="warning" heading=${t('linuxUsers.loadFailed.title')} text=${error}></lcm-empty-state>`
      : html`<lcm-skeleton shape="row" lines="5" label=${t('linuxUsers.loading')}></lcm-skeleton>`;
  }

  #open(e: CustomEvent<{ id: number }>): void {
    this.#openModal({ kind: 'edit', id: e.detail.id });
  }

  #renderUsers() {
    const users = this.#users.data;
    const pending = this.#loading(users, this.#users.error);
    if (pending || !users) return pending;
    const profiles = this.#profiles.data ?? [];
    const name = (id: number | null) => profiles.find((p) => p.id === id)?.name ?? '';
    const visible = filterUsers(users, this.#query, name);
    if (users.length === 0) {
      return html`<lcm-empty-state level="2" icon="terminal" heading=${t('linuxUsers.users.empty.title')} text=${t('linuxUsers.users.empty.text')}>
        ${this.#canWrite('users') ? html`<lcm-button slot="action" variant="primary" icon="plus" @click=${() => this.#openModal({ kind: 'new' })}>${t('linuxUsers.users.create')}</lcm-button>` : nothing}
      </lcm-empty-state>`;
    }
    if (visible.length === 0) return this.#noMatch();
    const modal = this.#modal;
    return html`<lcm-lu-users .users=${visible} .profiles=${profiles} .view=${parseView(router.state.query)} active-id=${modal.kind === 'edit' ? String(modal.id) : ''} @lcm-open-row=${this.#open}></lcm-lu-users>`;
  }

  #noMatch() {
    return html`<lcm-empty-state level="2" icon="search" heading=${t('linuxUsers.noMatch.title')} text=${t('linuxUsers.noMatch.text', { query: this.#query })}></lcm-empty-state>`;
  }

  async #copied(clone: () => Promise<number | null>): Promise<void> {
    if (this._copying) return;
    this._copying = true;
    const id = await clone();
    this._copying = false;
    if (id !== null) this.#openModal({ kind: 'edit', id });
  }

  #renderProfiles() {
    const profiles = this.#profiles.data;
    const pending = this.#loading(profiles, this.#profiles.error);
    if (pending || !profiles) return pending;
    const visible = filterProfiles(profiles, this.#query);
    if (visible.length === 0) return this.#noMatch();
    const modal = this.#modal;
    return html`<lcm-lu-profiles
      .profiles=${visible}
      .users=${this.#users.data ?? []}
      .view=${parseView(router.state.query)}
      active-id=${modal.kind === 'edit' ? String(modal.id) : ''}
      ?can-write=${this.#canWrite('profiles')}
      @lcm-open-row=${this.#open}
      @lcm-copied=${this.#open}
    ></lcm-lu-profiles>`;
  }

  #renderBlocks() {
    const blocks = this.#blocks.data;
    const pending = this.#loading(blocks, this.#blocks.error);
    if (pending || !blocks) return pending;
    const visible = filterBlocks(blocks, this.#query, this.#source);
    if (visible.length === 0) return this.#noMatch();
    const modal = this.#modal;
    return html`<lcm-lu-blocks
      .blocks=${visible}
      .view=${parseView(router.state.query)}
      active-id=${modal.kind === 'edit' ? String(modal.id) : ''}
      ?can-write=${this.#canWrite('profile-blocks')}
      @lcm-open-row=${this.#open}
      @lcm-copied=${this.#open}
    ></lcm-lu-blocks>`;
  }

  #renderSection(section: Section) {
    if (section === 'profiles') return this.#renderProfiles();
    if (section === 'profile-blocks') return this.#renderBlocks();
    return this.#renderUsers();
  }

  // ---- Erklärspalten ----

  #term(name: 'profile' | 'block') {
    return name === 'profile'
      ? html`<lcm-term heading=${t('linuxUsers.term.profile.title')} definition=${t('linuxUsers.term.profile.definition')}>${t('linuxUsers.term.profile.title')}</lcm-term>`
      : html`<lcm-term heading=${t('linuxUsers.term.block.title')} definition=${t('linuxUsers.term.block.definition')}>${t('linuxUsers.term.block.title')}</lcm-term>`;
  }

  #explainer(section: Section) {
    if (section === 'profiles') {
      return html`<lcm-explainer topic="linux-profiles" heading=${t('profiles.explainer.title')} collapse-label=${t('linuxUsers.explainer.collapse')} expand-label=${t('linuxUsers.explainer.expand')}>
        <p>${t('profiles.explainer.introBefore')} ${this.#term('profile')} ${t('profiles.explainer.introAfter')}</p>
        <ul>
          <li>${t('profiles.explainer.assign')}</li>
          <li>${t('profiles.explainer.blocks')}</li>
          <li>${t('profiles.explainer.builtin')}</li>
        </ul>
        <p><strong>${t('profiles.explainer.writtenTitle')}</strong> ${t('profiles.explainer.written')}</p>
        <p>${t('profiles.explainer.apply')}</p>
      </lcm-explainer>`;
    }
    return html`<lcm-explainer topic="linux-profile-blocks" heading=${t('profileBlocks.explainer.title')} collapse-label=${t('linuxUsers.explainer.collapse')} expand-label=${t('linuxUsers.explainer.expand')}>
      <p>${t('profileBlocks.explainer.introBefore')} ${this.#term('block')} ${t('profileBlocks.explainer.introAfter')}</p>
      <ul>
        <li>${t('profileBlocks.explainer.variants')}</li>
        <li>${t('profileBlocks.explainer.params')}</li>
        <li>${t('profileBlocks.explainer.combine')}</li>
      </ul>
      <p><strong>${t('profileBlocks.explainer.writtenTitle')}</strong> ${t('profileBlocks.explainer.written')}</p>
      <p>${t('profileBlocks.explainer.change')}</p>
    </lcm-explainer>`;
  }

  // ---- Fenster ----

  #renderModal(section: Section) {
    const modal = this.#modal;
    const close = () => this.#closeModal();
    const editId = modal.kind === 'edit' ? modal.id : null;
    if (section === 'profiles') {
      const profile = this.#profiles.data?.find((p) => p.id === editId) ?? null;
      return html`<lcm-lu-profile-modal
        ?open=${modal.kind === 'new' || profile !== null}
        .profile=${profile}
        .blocks=${this.#blocks.data ?? []}
        ?can-write=${this.#canWrite('profiles')}
        @lcm-close=${close}
        @lcm-saved=${close}
        @lcm-clone=${(e: CustomEvent<Profile>) => void this.#copied(() => cloneProfile(e.detail, this.#profiles.data ?? []))}
      ></lcm-lu-profile-modal>`;
    }
    if (section === 'profile-blocks') {
      const block = this.#blocks.data?.find((b) => b.id === editId) ?? null;
      return html`<lcm-lu-block-modal
        ?open=${modal.kind === 'new' || block !== null}
        .block=${block}
        ?can-write=${this.#canWrite('profile-blocks')}
        @lcm-close=${close}
        @lcm-saved=${close}
        @lcm-clone=${(e: CustomEvent<Block>) => void this.#copied(() => cloneBlock(e.detail, this.#blocks.data ?? []))}
      ></lcm-lu-block-modal>`;
    }
    const user = this.#users.data?.find((u) => u.id === editId) ?? null;
    return html`<lcm-lu-user-modal
      ?open=${modal.kind === 'new' || user !== null}
      .user=${user}
      .profiles=${this.#profiles.data ?? []}
      .groups=${this.#groups.data ?? []}
      ?can-write=${this.#canWrite('users')}
      ?can-sync=${auth.can('servers:write')}
      @lcm-close=${close}
      @lcm-saved=${close}
      @lcm-created=${(e: CustomEvent<{ id: number }>) => this.#openModal({ kind: 'edit', id: e.detail.id })}
    ></lcm-lu-user-modal>`;
  }

  // ---- Seite ----

  #tabs(): TabItem[] {
    return this.#sections.map((s) => ({ id: s.id, label: t(s.labelKey) }));
  }

  #tools(section: Section) {
    const searchLabel = t(SEARCH_PLACEHOLDER[section]);
    return html`<div class="tools">
      <lcm-input maxlength=${LIMITS.query}
        icon="search"
        label=${t('linuxUsers.search.label')}
        width="full"
        placeholder=${searchLabel}
        .value=${this.#query}
        @lcm-input=${(e: CustomEvent<{ value: string }>) => router.setQuery({ q: e.detail.value, page: null }, { replace: true })}
      ></lcm-input>
      ${section === 'profile-blocks'
        ? html`<lcm-select
            label=${t('profileBlocks.source.label')}
            width="full"
            .options=${[
              { value: 'all', label: t('profileBlocks.source.all') },
              { value: 'builtin', label: t('profileBlocks.source.builtin') },
              { value: 'own', label: t('profileBlocks.source.own') },
            ]}
            .value=${this.#source}
            @lcm-change=${(e: CustomEvent<{ value: string }>) => router.setQuery({ source: e.detail.value === 'all' ? null : e.detail.value, page: null }, { replace: true })}
          ></lcm-select>`
        : nothing}
      ${this.#canWrite(section)
        ? html`<lcm-button variant="primary" icon="plus" @click=${() => this.#openModal({ kind: 'new' })}>${t(CREATE_LABEL[section])}</lcm-button>`
        : nothing}
    </div>`;
  }

  override render() {
    const section = this.#section;
    if (section === null) {
      return html`<lcm-empty-state level="1" icon="lock" heading=${t('linuxUsers.forbidden.title')} text=${t('linuxUsers.forbidden.text')}></lcm-empty-state>`;
    }
    const explain = section !== 'users';
    return html`
      <div class="head">
        <h1>${t('linuxUsers.title')}</h1>
        ${this.#tools(section)}
      </div>
      <lcm-tabs label=${t('linuxUsers.tabs.label')} .tabs=${this.#tabs()} value=${section} @lcm-tab-change=${(e: CustomEvent<{ id: string }>) => this.#gotoSection(e.detail.id)}>
        <div class=${`layout${explain ? ' explain' : ''}`}>
          <div class="content">${this.#renderSection(section)}</div>
          ${explain ? this.#explainer(section) : nothing}
        </div>
      </lcm-tabs>
      ${this.#renderModal(section)}
    `;
  }
}
customElements.define('lcm-page-linux-users', LinuxUsersPage);
