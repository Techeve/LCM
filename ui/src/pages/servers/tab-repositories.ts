import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-empty-state';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import { baseStyles } from '../../components/shared';
import { normaliseRepositories, repositoriesUrl, serverUrl, type Repository } from '../../api/servers';
import { APT_CACHES_URL, KNOWN_REPOS_URL, normaliseAptCaches, normaliseKnownRepos, normaliseRepoState, reposApi, reposFor, type AptCache, type KnownRepo, type RepoState } from '../../api/servers-packages';
import { packageManagers } from '../../api/server-detail';
import type { ConfigPreviewAction, ConfigPreviewParams } from '../../api/servers-previews';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import './set-action';
import { tabStyles } from './srv-styles';
import { SrvTab } from './srv-tab';


/**
 * Repositories: die Paketquellen des Servers. Unsichere (HTTP) sind markiert und lassen sich auf HTTPS umstellen
 * (und zurück); bekannte Quellen aus dem Katalog lassen sich einrichten, apt-Server an einen APT-Cache anbinden.
 * Was die Konsole dabei ausgibt, steht unter der Liste.
 */
export class SrvRepositories extends SrvTab {
  static override properties = {
    ...SrvTab.properties,
    _repoKey: { state: true },
    _cacheId: { state: true },
    _output: { state: true },
  };
  static override styles = [
    baseStyles,
    tabStyles,
    css`
      .summary {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-3);
        margin-block-end: var(--space-4);
      }
      ul {
        display: flex;
        flex-direction: column;
        margin: 0;
        padding: 0;
        list-style: none;
      }
      li {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
        padding-block: var(--space-3);
        border-block-start: 1px solid var(--line);
      }
      li:first-child {
        border-block-start: 0;
      }
      code {
        flex: 1 1 20rem;
        min-inline-size: 0;
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        overflow-wrap: anywhere;
      }
      .setup {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
      .setup h3 {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      .line {
        display: flex;
        flex-wrap: wrap;
        align-items: end;
        gap: var(--space-3);
      }
      .line.center {
        align-items: center;
      }
      p.note {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
        max-inline-size: 48rem;
      }
      pre {
        max-block-size: 20rem;
        margin: 0;
        padding: var(--space-3);
        overflow: auto;
        border-radius: var(--radius-s);
        background: var(--surface-1);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        line-height: 1.55;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
      }
    `,
  ];

  declare _repoKey: string;
  declare _cacheId: string;
  declare _output: { label: string; text: string } | null;

  readonly #repos = new LiveData<Repository[]>(this, {
    url: () => repositoriesUrl(this.server.id),
    topics: () => [`server:${this.server.id}`],
    transform: normaliseRepositories,
  });
  readonly #state = new LiveData<RepoState>(this, { url: () => serverUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseRepoState });
  readonly #known = new LiveData<KnownRepo[]>(this, { url: () => (this.canWrite ? KNOWN_REPOS_URL : null), transform: normaliseKnownRepos });
  readonly #caches = new LiveData<AptCache[]>(this, { url: () => (this.canWrite ? APT_CACHES_URL : null), transform: normaliseAptCaches });

  constructor() {
    super();
    this._repoKey = '';
    this._cacheId = '';
    this._output = null;
  }

  #after(label: string) {
    return (e: CustomEvent<{ output: string }>) => {
      this._output = { label, text: e.detail.output };
      this.#repos.reload();
    };
  }

  #action(options: { action: ConfigPreviewAction; params?: ConfigPreviewParams; label: string; help: string; meaning?: string; heading: string; icon: 'lock' | 'undo' | 'plus' | 'package' | 'x'; variant?: 'primary' | 'outline' | 'danger'; impacts?: Array<{ label: string; value: string; tone?: 'ok' | 'warn' | 'crit' }>; done: string; run: () => Promise<{ output: string }>; disabled?: boolean }): TemplateResult {
    const server = this.server;
    return html`<lcm-set-action
      variant=${options.variant ?? 'outline'}
      icon=${options.icon}
      server-id=${server.id}
      server-name=${server.name}
      ?disabled=${options.disabled ?? false}
      ?danger=${options.variant === 'danger'}
      label=${options.label}
      help=${options.help}
      meaning=${options.meaning ?? ''}
      heading=${options.heading}
      action=${options.action}
      .params=${options.params ?? {}}
      .impacts=${options.impacts ?? []}
      done=${options.done}
      .run=${options.run}
      @lcm-done=${this.#after(options.label)}
    ></lcm-set-action>`;
  }

  // ---- Quellen ----

  #renderSecure(insecure: number, apt: boolean): TemplateResult | typeof nothing {
    // Das Umstellen kennt nur APT-Quellen.
    if (!this.canWrite || insecure === 0 || !apt) return nothing;
    const name = this.server.name;
    return this.#action({
      label: t('servers.repositories.secure.label'),
      help: t('servers.repositories.secure.help'),
      meaning: t('servers.repositories.secure.meaning'),
      heading: t('servers.repositories.secure.heading', { name }),
      icon: 'lock',
      variant: 'primary',
      action: 'repos-secure',
      impacts: [{ label: t('serverPackages.repos.sources'), value: t('serverPackages.repos.secureValue', { count: insecure }) }],
      done: t('servers.repositories.secure.done', { name }),
      run: () => reposApi.secure(this.server.id),
    });
  }

  #renderRevert(state: RepoState): TemplateResult | typeof nothing {
    if (!this.canWrite || state.revertUrls.length === 0) return nothing;
    const name = this.server.name;
    return this.#action({
      label: t('serverPackages.repos.revert.label', { count: state.revertUrls.length }),
      help: t('serverPackages.repos.revert.help'),
      heading: t('serverPackages.repos.revert.heading', { name }),
      icon: 'undo',
      action: 'repos-revert-https',
      done: t('serverPackages.repos.revert.done', { name }),
      run: () => reposApi.revertHttps(this.server.id),
    });
  }

  #renderList(repos: Repository[], state: RepoState | undefined): TemplateResult {
    const insecure = repos.filter((repo) => repo.insecure).length;
    const manager = packageManagers({ package_manager: state?.packageManager ?? this.server.package_manager, has_snap: false })[0] ?? '';
    return html`<lcm-card heading=${t('servers.repositories.title')} subtitle=${t('servers.repositories.subtitle')}>
      <div class="summary">
        ${manager ? html`<lcm-badge title=${t('serverPackages.repos.managerHint')}>${manager}</lcm-badge>` : nothing}
        ${insecure > 0 ? html`<lcm-badge tone="crit">${t('servers.repositories.insecure', { count: insecure })}</lcm-badge>` : html`<lcm-badge tone="ok">${t('servers.repositories.allSecure')}</lcm-badge>`}
        ${this.#renderSecure(insecure, state?.packageManager === 'apt')} ${state ? this.#renderRevert(state) : nothing}
      </div>
      ${repos.length === 0
        ? html`<p class="note">${t('servers.repositories.empty.text')}</p>`
        : html`<ul>
            ${repos.map((repo) => html`<li><code>${repo.line}</code>${repo.insecure ? html`<lcm-badge tone="crit">${t('servers.repositories.http')}</lcm-badge>` : nothing}</li>`)}
          </ul>`}
    </lcm-card>`;
  }

  // ---- Einrichten ----

  #renderAdd(state: RepoState): TemplateResult {
    if (state.proxmox) return html`<p class="note">${t('serverPackages.repos.add.proxmox')}</p>`;
    const repos = reposFor(this.#known.data ?? [], state.packageManager);
    if (repos.length === 0) return html`<p class="note">${t('serverPackages.repos.add.noCatalog')}</p>`;
    const chosen = repos.find((repo) => repo.key === this._repoKey);
    const name = this.server.name;
    return html`<div class="line">
        <lcm-select
          label=${t('serverPackages.repos.add.choose')}
          width="l"
          .options=${[{ value: '', label: t('serverPackages.repos.add.placeholder') }, ...repos.map((repo) => ({ value: repo.key, label: repo.name }))]}
          .value=${this._repoKey}
          @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._repoKey = e.detail.value)}
        ></lcm-select>
        ${this.#action({
          label: t('serverPackages.repos.add.label'),
          help: t('serverPackages.repos.add.help'),
          heading: t('serverPackages.repos.add.heading', { repo: chosen?.name ?? '', name }),
          icon: 'plus',
          disabled: !chosen,
          action: 'repos-add',
          params: { key: this._repoKey },
          impacts: [
            { label: t('serverPackages.repos.add.source'), value: chosen?.name ?? '' },
            { label: t('serverPackages.repos.add.key'), value: chosen?.keyUrl ?? '' },
          ],
          done: t('serverPackages.repos.add.done', { repo: chosen?.name ?? '', name }),
          run: async () => {
            const result = await reposApi.add(this.server.id, this._repoKey);
            this._repoKey = '';
            return result;
          },
        })}
      </div>
      ${chosen?.description ? html`<p class="note">${chosen.description}</p>` : nothing}`;
  }

  #renderCache(state: RepoState): TemplateResult {
    const caches = this.#caches.data ?? [];
    const name = this.server.name;
    if (state.aptProxyActive) {
      const cache = caches.find((c) => c.url === state.aptProxyUrl)?.name ?? state.aptProxyUrl;
      return html`<div class="line center">
        <lcm-badge tone="info">${cache ? t('serverPackages.repos.cache.activeAt', { cache }) : t('serverPackages.repos.cache.active')}</lcm-badge>
        ${this.#action({
          label: t('serverPackages.repos.cache.disconnect.label'),
          help: t('serverPackages.repos.cache.disconnect.help'),
          heading: t('serverPackages.repos.cache.disconnect.heading', { name }),
          icon: 'x',
          action: 'apt-cache-disconnect',
          done: t('serverPackages.repos.cache.disconnect.done', { name }),
          run: () => reposApi.aptProxy(this.server.id, false),
        })}
      </div>`;
    }
    if (caches.length === 0) return html`<p class="note">${t('serverPackages.repos.cache.none')}</p>`;
    const chosen = caches.find((c) => String(c.id) === this._cacheId) ?? caches[0];
    return html`<div class="line">
      ${caches.length > 1
        ? html`<lcm-select
            label=${t('serverPackages.repos.cache.choose')}
            .options=${caches.map((c) => ({ value: String(c.id), label: c.name }))}
            .value=${String(chosen?.id ?? '')}
            @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._cacheId = e.detail.value)}
          ></lcm-select>`
        : nothing}
      ${this.#action({
        label: t('serverPackages.repos.cache.connect.label'),
        help: t('serverPackages.repos.cache.connect.help'),
        heading: t('serverPackages.repos.cache.connect.heading', { name }),
        icon: 'package',
        action: 'apt-cache-connect',
        params: { cacheId: chosen?.id ?? 0 },
        impacts: [{ label: t('serverPackages.repos.cache.cache'), value: chosen ? `${chosen.name} (${chosen.url})` : '' }],
        done: t('serverPackages.repos.cache.connect.done', { name }),
        run: () => reposApi.aptProxy(this.server.id, true, chosen?.id ?? 0),
      })}
    </div>`;
  }

  #renderSetup(): TemplateResult | typeof nothing {
    const state = this.#state.data;
    if (!this.canWrite || !state) return nothing;
    return html`<lcm-card heading=${t('serverPackages.repos.setup.title')} subtitle=${t('serverPackages.repos.setup.subtitle')}>
      <div class="setup">
        <h3>${t('serverPackages.repos.add.title')}</h3>
        ${this.#renderAdd(state)}
        ${state.packageManager === 'apt' && !state.proxmox ? html`<h3>${t('serverPackages.repos.cache.title')}</h3>${this.#renderCache(state)}` : nothing}
      </div>
    </lcm-card>`;
  }

  #renderOutput(): TemplateResult | typeof nothing {
    const output = this._output;
    if (!output) return nothing;
    return html`<lcm-card heading=${t('serverPackages.repos.output.title')} subtitle=${output.label}>
      <lcm-button slot="actions" size="small" variant="ghost" icon="x" icon-only label=${t('serverPackages.repos.output.close')} @click=${() => (this._output = null)}></lcm-button>
      <pre>${output.text || t('serverPackages.repos.output.empty')}</pre>
    </lcm-card>`;
  }

  override render() {
    const { data, error } = this.#repos;
    if (!data) {
      return error
        ? html`<lcm-empty-state level="3" icon="warning" heading=${t('servers.error')} text=${error}></lcm-empty-state>`
        : html`<lcm-skeleton shape="row" lines="4" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    return html`<div class="stack">${this.#renderList(data, this.#state.data)} ${this.#renderOutput()} ${this.#renderSetup()}</div>`;
  }
}
customElements.define('lcm-srv-repositories', SrvRepositories);
