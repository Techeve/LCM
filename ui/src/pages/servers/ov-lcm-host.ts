import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import '../../components/lcm-switch';
import { baseStyles, type StatusLevel } from '../../components/shared';
import type { ServerItem, StartedJob } from '../../api/servers';
import { detailApi, lcmHostMode, lcmHostUrl, normaliseLcmHost, type LcmHostStatus, type ScannerInfo } from '../../api/server-detail';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { formatDateTime, formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import type { ConfigPreviewAction } from '../../api/servers-previews';
import './set-action';

/** Die Befehlsvorschau je Einrichtung des LCM-Hosts. */
const PREVIEW: Record<'trivy' | 'sandbox' | 'aptCacher' | 'lapi', ConfigPreviewAction> = {
  trivy: 'lcm-host-trivy',
  sandbox: 'lcm-host-sandbox',
  aptCacher: 'lcm-host-apt-cacher',
  lapi: 'lcm-host-crowdsec-lapi',
};


const INSTALL = {
  trivy: { label: 'serverDetail.lcmHost.trivy.label', help: 'serverDetail.lcmHost.trivy.help', meaning: 'serverDetail.lcmHost.trivy.meaning' },
  sandbox: { label: 'serverDetail.lcmHost.sandbox.label', help: 'serverDetail.lcmHost.sandbox.help', meaning: 'serverDetail.lcmHost.sandbox.meaning' },
  aptCacher: { label: 'serverDetail.lcmHost.aptCacher.label', help: 'serverDetail.lcmHost.aptCacher.help', meaning: 'serverDetail.lcmHost.aptCacher.meaning' },
  lapi: { label: 'serverDetail.lcmHost.lapi.label', help: 'serverDetail.lcmHost.lapi.help', meaning: 'serverDetail.lcmHost.lapi.meaning' },
};

function pill(level: StatusLevel, label: string): TemplateResult {
  return html`<lcm-status variant="pill" level=${level} label=${label}></lcm-status>`;
}

/**
 * `<lcm-srv-lcm-host .server can-write>`: Karte für den Server, auf dem LCM selbst läuft. Richtet Trivy (CVE-Scanner,
 * samt Sandbox), apt-cacher-ng und die zentrale CrowdSec-LAPI ein. Im Container und ohne apt steht nur die Erklärung da,
 * weil die Einrichtung dort scheitern müsste.
 */
export class SrvLcmHost extends LitElement {
  static override properties = {
    server: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _bouncer: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      .services {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(min(100%, 17rem), 1fr));
        gap: var(--space-4) var(--gap);
      }
      .service {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        padding: var(--space-4);
        border-radius: var(--radius-m);
        background: var(--surface-1);
      }
      .name {
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        gap: var(--space-2);
        font-weight: var(--fw-semibold);
      }
      .what {
        color: var(--text-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-regular);
      }
      .state {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2);
      }
      .meta {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .mono {
        font-family: var(--font-mono);
      }
      .act {
        display: flex;
        flex-wrap: wrap;
        align-items: center;
        gap: var(--space-2) var(--space-3);
        margin-block-start: auto;
        padding-block-start: var(--space-2);
      }
      p {
        margin: 0;
      }
    `,
  ];

  declare server: ServerItem;
  declare canWrite: boolean;
  declare _bouncer: boolean;

  readonly #status = new LiveData<LcmHostStatus>(this, {
    url: () => lcmHostUrl(this.server.id),
    topics: () => [`server:${this.server.id}`],
    transform: normaliseLcmHost,
  });

  constructor() {
    super();
    this.canWrite = false;
    this._bouncer = false;
    new I18nController(this);
  }

  #install(key: 'trivy' | 'sandbox' | 'aptCacher' | 'lapi', run: () => Promise<StartedJob>, variant = 'primary'): TemplateResult | typeof nothing {
    if (!this.canWrite) return nothing;
    const s = this.server;
    return html`<lcm-set-action
      variant=${variant}
      icon="download"
      server-id=${s.id}
      server-name=${s.name}
      action=${PREVIEW[key]}
      .params=${key === 'lapi' ? { bouncer: this._bouncer } : {}}
      label=${t(INSTALL[key].label)}
      help=${t(INSTALL[key].help)}
      meaning=${t(INSTALL[key].meaning)}
      .run=${async () => {
        const job = await run();
        this.#status.reload();
        return job;
      }}
    ></lcm-set-action>`;
  }

  #scanner(scanner: ScannerInfo | null): TemplateResult | typeof nothing {
    if (!scanner?.available) return nothing;
    const db = scanner.updated_at
      ? html`<span title=${formatDateTime(scanner.updated_at)}>${t('serverDetail.lcmHost.trivy.dbFrom', { when: formatRelative(scanner.updated_at) })}</span>
          ${scanner.freshness === 'stale' || scanner.freshness === 'critical' ? pill(scanner.freshness === 'critical' ? 'crit' : 'warn', t('serverDetail.lcmHost.trivy.dbStale')) : nothing}`
      : pill('crit', t('serverDetail.lcmHost.trivy.dbNever'));
    return html`<div class="meta"><span class="mono">${t('serverDetail.lcmHost.trivy.version', { version: scanner.version })}</span> · ${db}</div>
      <div class="state">
        ${scanner.sandboxed ? pill('ok', t('serverDetail.lcmHost.trivy.sandboxOn', { backend: scanner.sandbox_backend })) : pill('warn', t('serverDetail.lcmHost.trivy.sandboxOff'))}
      </div>
      ${scanner.sandboxed ? nothing : html`<p class="meta">${t('serverDetail.lcmHost.trivy.sandboxOffHint')}${scanner.sandbox_note ? ` ${scanner.sandbox_note}` : ''}</p>`}`;
  }

  #trivy(status: LcmHostStatus): TemplateResult {
    const id = this.server.id;
    return html`<div class="service">
      <div class="name">${t('serverDetail.lcmHost.trivy.name')} <span class="what">${t('serverDetail.lcmHost.trivy.what')}</span></div>
      <div class="state">${status.trivy_installed ? pill('ok', t('serverDetail.lcmHost.installed')) : pill('unknown', t('serverDetail.lcmHost.notInstalled'))}</div>
      ${status.trivy_installed ? this.#scanner(status.scanner) : nothing}
      <div class="act">
        ${status.trivy_installed ? nothing : this.#install('trivy', () => detailApi.installTrivy(id))}
        ${status.trivy_installed && status.sandbox_retrofit ? this.#install('sandbox', () => detailApi.installSandbox(id), 'outline') : nothing}
      </div>
    </div>`;
  }

  #aptCacher(status: LcmHostStatus): TemplateResult {
    const id = this.server.id;
    const settings = status.apt_cacher_installed && auth.can('settings:manage');
    return html`<div class="service">
      <div class="name">${t('serverDetail.lcmHost.aptCacher.name')} <span class="what">${t('serverDetail.lcmHost.aptCacher.what')}</span></div>
      <div class="state">${status.apt_cacher_installed ? pill('ok', t('serverDetail.lcmHost.installed')) : pill('unknown', t('serverDetail.lcmHost.notInstalled'))}</div>
      <div class="act">
        ${status.apt_cacher_installed ? nothing : this.#install('aptCacher', () => detailApi.installAptCacher(id))}
        ${settings ? html`<lcm-button size="small" variant="ghost" icon="arrow-right" href="/settings/apt-cache">${t('serverDetail.lcmHost.aptCacher.settings')}</lcm-button>` : nothing}
      </div>
    </div>`;
  }

  #lapi(status: LcmHostStatus): TemplateResult {
    const id = this.server.id;
    const installable = !status.crowdsec_lapi_installed && this.canWrite;
    return html`<div class="service">
      <div class="name">${t('serverDetail.lcmHost.lapi.name')} <span class="what">${t('serverDetail.lcmHost.lapi.what')}</span></div>
      <div class="state">${status.crowdsec_lapi_installed ? pill('ok', t('serverDetail.lcmHost.installed')) : pill('unknown', t('serverDetail.lcmHost.notInstalled'))}</div>
      ${installable
        ? html`<div class="act">
            <lcm-switch label=${t('serverDetail.lcmHost.lapi.bouncer')} .checked=${this._bouncer} @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => (this._bouncer = e.detail.checked)}></lcm-switch>
            ${this.#install('lapi', () => detailApi.installCrowdSecLapi(id, this._bouncer))}
          </div>`
        : nothing}
    </div>`;
  }

  #body(status: LcmHostStatus): TemplateResult {
    const mode = lcmHostMode(status);
    if (mode === 'container') return html`<lcm-notice>${t('serverDetail.lcmHost.inContainer')}</lcm-notice>`;
    if (mode === 'not-apt') return html`<lcm-notice tone="warn">${t('serverDetail.lcmHost.notApt')}</lcm-notice>`;
    return html`<div class="services">${this.#trivy(status)} ${this.#aptCacher(status)} ${this.#lapi(status)}</div>`;
  }

  override render() {
    const { data, error } = this.#status;
    const body = data
      ? this.#body(data)
      : error
        ? html`<lcm-notice tone="warn">${error}</lcm-notice>`
        : html`<lcm-skeleton shape="block" lines="3" label=${t('servers.loading')}></lcm-skeleton>`;
    return html`<lcm-card heading=${t('serverDetail.lcmHost.title')} subtitle=${t('serverDetail.lcmHost.intro')}>${body}</lcm-card>`;
  }
}
customElements.define('lcm-srv-lcm-host', SrvLcmHost);
