import { LitElement, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-select';
import '../../components/lcm-skeleton';
import { formLayoutStyles } from '../../components/layout-styles';
import { baseStyles } from '../../components/shared';
import { serversApi, type ServerPackage } from '../../api/servers';
import { normaliseVersions, versionLess, versionsUrl } from '../../api/servers-packages';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import './srv-action';
import { settingsStyles } from './set-styles';


/**
 * `<lcm-pkg-version>`: eine bestimmte Version eines Pakets installieren (auch ein Downgrade). Die Versionen liefert
 * die Paketverwaltung des Servers; installiert wird als Server-Aktion mit Befehlsvorschau und Bestätigung.
 */
export class PkgVersion extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    pkg: { attribute: false },
    _chosen: { state: true },
  };
  static override styles = [baseStyles, formLayoutStyles, settingsStyles];

  declare open: boolean;
  declare serverId: number;
  declare serverName: string;
  declare pkg: ServerPackage | undefined;
  declare _chosen: string;

  readonly #versions = new LiveData<string[]>(this, {
    url: () => (this.open && this.pkg ? versionsUrl(this.serverId, this.pkg.name) : null),
    transform: normaliseVersions,
  });

  constructor() {
    super();
    this.open = false;
    this.serverId = 0;
    this.serverName = '';
    this.pkg = undefined;
    this._chosen = '';
    new I18nController(this);
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (changed.has('pkg') || (changed.has('open') && !this.open)) this._chosen = '';
  }

  #close(): void {
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  #renderChoice(pkg: ServerPackage, versions: string[]): TemplateResult {
    if (versions.length === 0) return html`<p class="note">${t('serverPackages.version.none')}</p>`;
    const options = [
      { value: '', label: t('serverPackages.version.placeholder') },
      ...versions.map((v) => ({ value: v, label: v === pkg.version ? t('serverPackages.version.installedOption', { version: v }) : v })),
    ];
    const chosen = this._chosen;
    const downgrade = chosen !== '' && chosen !== pkg.version && versionLess(chosen, pkg.version);
    return html`<div class="form-stack">
      <dl class="kv">
        <dt>${t('serverPackages.version.installed')}</dt>
        <dd class="mono">${pkg.version || '–'}</dd>
        ${pkg.candidate ? html`<dt>${t('serverPackages.version.candidate')}</dt><dd class="mono">${pkg.candidate}</dd>` : nothing}
      </dl>
      <div class="row">
        <lcm-select label=${t('serverPackages.version.choose')} width="l" .options=${options} .value=${chosen} @lcm-change=${(e: CustomEvent<{ value: string }>) => (this._chosen = e.detail.value)}></lcm-select>
        ${downgrade ? html`<lcm-badge tone="warn">${t('serverPackages.version.downgrade')}</lcm-badge>` : nothing}
      </div>
    </div>`;
  }

  #renderBody(pkg: ServerPackage): TemplateResult {
    const { data, error } = this.#versions;
    if (data === undefined) {
      return error ? html`<lcm-notice tone="crit">${error}</lcm-notice>` : html`<lcm-skeleton shape="row" lines="2" label=${t('servers.loading')}></lcm-skeleton>`;
    }
    return this.#renderChoice(pkg, data);
  }

  override render() {
    const pkg = this.pkg;
    const chosen = this._chosen;
    return html`<lcm-modal ?open=${this.open && !!pkg} size="m" heading=${t('serverPackages.version.title', { name: pkg?.name ?? '' })} close-label=${t('servers.confirm.cancel')} @lcm-close=${() => this.#close()}>
      ${pkg ? this.#renderBody(pkg) : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t('servers.confirm.cancel')}</lcm-button>
      ${pkg && (this.#versions.data?.length ?? 0) > 0
        ? html`<lcm-srv-action
            slot="footer"
            variant="primary"
            icon="download"
            server-id=${this.serverId}
            server-name=${this.serverName}
            action="update"
            .params=${{ package: [pkg.name], version: chosen }}
            ?disabled=${chosen === '' || chosen === pkg.version}
            label=${t('serverPackages.version.install.label')}
            help=${t('serverPackages.version.install.help', { name: pkg.name })}
            confirm-heading=${t('serverPackages.version.install.heading', { name: pkg.name, version: chosen })}
            .run=${() => serversApi.updatePackages(this.serverId, [pkg.name], chosen)}
            @lcm-started=${() => this.#close()}
          ></lcm-srv-action>`
        : nothing}
    </lcm-modal>`;
  }
}
customElements.define('lcm-pkg-version', PkgVersion);
