import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../components/lcm-badge';
import '../components/lcm-button';
import '../components/lcm-logo';
import '../components/lcm-modal';
import '../components/lcm-notice';
import { SYSTEM_INFO_URL, UPDATE_INFO_URL, normaliseSystemInfo, normaliseUpdateInfo, systemApi, type SystemInfo, type UpdateInfo } from '../api/system';
import { baseStyles, emit, utilStyles } from '../components/shared';
import { auth } from '../lib/auth';
import { I18nController, SubscribeController } from '../lib/controllers';
import { formatDate, formatDateTime } from '../lib/format';
import { errorText, t } from '../lib/i18n';
import { LiveData } from '../lib/live-data';
import { store } from '../lib/store';
import { updateState } from './update-model';

// Impressum: fest eingetragen wie im alten Frontend. Verantwortlich ist der Betreiber dieser Instanz.
export const IMPRINT = {
  operator: 'Techeve',
  owner: 'Tony Grätscher',
  street: 'Talweg 14',
  city: '07639 Bad Klosterlausnitz',
  email: 'info@techeve.de',
  web: 'https://techeve.de/?mtm_campaign=linking&mtm_kwd=lcm',
  webLabel: 'techeve.de',
};

/** „1.42.2 (Build 812, a1b2c3d)“ – die Kurzform für Menü und Fenster. */
export function versionLine(info: SystemInfo | undefined): string {
  if (!info?.version) return '';
  const build = [info.build ? t('about.build', { build: info.build }) : '', info.commit].filter(Boolean).join(', ');
  return build ? `${info.version} (${build})` : info.version;
}

/**
 * <lcm-about-dialog open> – Info-Fenster: Version, Build, Commit und Plattform, Update-Stand mit „Jetzt prüfen“ und das
 * Impressum. Auch ohne Anmeldung erreichbar (dann nur Version und Build). Meldet `lcm-close`.
 */
export class LcmAboutDialog extends LitElement {
  static override properties = { open: { type: Boolean, reflect: true }, _checking: { state: true }, _checkError: { state: true } };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      .columns {
        display: grid;
        grid-template-columns: repeat(2, minmax(0, 1fr));
        gap: var(--space-6);
      }
      .head {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        margin-block-end: var(--space-5);
      }
      .head div {
        display: grid;
      }
      lcm-logo {
        inline-size: 3rem;
        block-size: 3rem;
      }
      .name {
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
      }
      section + section {
        margin-block-start: var(--space-5);
        padding-block-start: var(--space-4);
        border-block-start: 1px solid var(--line);
      }
      h3 {
        margin: 0 0 var(--space-3);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
        color: var(--text-2);
        text-transform: var(--label-transform);
        letter-spacing: var(--label-spacing);
      }
      dl {
        display: grid;
        grid-template-columns: max-content minmax(0, 1fr);
        gap: var(--space-2) var(--space-4);
        margin: 0;
      }
      dt {
        color: var(--text-2);
      }
      dd {
        margin: 0;
        overflow-wrap: anywhere;
      }
      .mono {
        font-family: var(--font-mono);
        font-size: var(--fs-s);
      }
      .warn {
        color: var(--warn-text);
      }
      .check {
        margin-block-start: var(--space-3);
      }
      address {
        font-style: normal;
        line-height: 1.6;
      }
      a {
        color: var(--accent-text);
      }
      @media (max-width: 40rem) {
        .columns {
          grid-template-columns: minmax(0, 1fr);
        }
      }
    `,
  ];

  declare open: boolean;
  declare _checking: boolean;
  declare _checkError: string;

  readonly #system = new LiveData<SystemInfo>(this, { url: () => (this.open ? SYSTEM_INFO_URL : null), transform: normaliseSystemInfo });
  readonly #update = new LiveData<UpdateInfo>(this, { url: () => (this.open && auth.isAuthenticated ? UPDATE_INFO_URL : null), transform: normaliseUpdateInfo });

  constructor() {
    super();
    this.open = false;
    this._checking = false;
    this._checkError = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  async #checkNow(): Promise<void> {
    this._checking = true;
    this._checkError = '';
    try {
      await systemApi.checkNow();
      store.invalidate(UPDATE_INFO_URL);
      store.invalidate('/api/v1/system/self-update');
      updateState.markChecked();
    } catch (error) {
      // Das Fenster ist offen: der Fehler steht unter dem Knopf, kein Toast.
      this._checkError = errorText(error);
    } finally {
      this._checking = false;
    }
  }

  #row(label: string, value: TemplateResult | string): TemplateResult {
    return html`<dt>${label}</dt>
      <dd>${value}</dd>`;
  }

  #version(info: SystemInfo | undefined): TemplateResult {
    const builtAt = info?.built_at ?? '';
    return html`<section>
      <h3>${t('about.versionTitle')}</h3>
      <dl>
        ${this.#row(t('about.version'), html`<span class="mono">${info?.version || '…'}</span>`)}
        ${this.#row(t('about.buildLabel'), html`<span class="mono">${info?.build || '…'}</span>`)}
        ${info?.commit
          ? this.#row(
              t('about.commit'),
              html`<span class="mono">${info.commit}</span>
                ${info.dirty ? html`<lcm-badge tone="warn" title=${t('about.dirtyHelp')}>${t('about.dirty')}</lcm-badge>` : nothing}`,
            )
          : nothing}
        ${info?.platform ? this.#row(t('about.platform'), html`<span class="mono">${info.platform}</span>`) : nothing}
        ${builtAt ? this.#row(t('about.builtAt'), html`<span title=${formatDateTime(builtAt)}>${formatDate(builtAt)}</span>`) : nothing}
      </dl>
    </section>`;
  }

  #updates(update: UpdateInfo | undefined): TemplateResult | typeof nothing {
    if (!auth.isAuthenticated) return nothing;
    const channel = update?.channel ? t('about.channel', { channel: update.channel }) : '';
    return html`<section>
      <h3>${t('about.updatesTitle')}</h3>
      <dl>
        ${this.#row(
          t('about.latest'),
          update?.latest_version
            ? html`<span class="mono">${update.latest_version}</span> <span class="muted">${channel}</span>`
            : html`<span class="muted">${t('about.latestUnknown')}</span>`,
        )}
        ${update?.checked_at ? this.#row(t('about.checkedAt'), formatDateTime(update.checked_at)) : nothing}
        ${update?.error ? this.#row(t('about.checkError'), html`<span class="warn">${update.error}</span>`) : nothing}
      </dl>
      ${auth.can('settings:manage')
        ? html`<lcm-button class="check" size="small" variant="outline" icon="refresh" ?busy=${this._checking} @click=${() => void this.#checkNow()}>${t('about.checkNow')}</lcm-button>`
        : nothing}
      ${this._checkError ? html`<lcm-notice class="check" tone="crit" heading=${t('about.checkFailed')}>${this._checkError}</lcm-notice>` : nothing}
    </section>`;
  }

  #imprint(): TemplateResult {
    return html`<section>
      <h3>${t('about.imprintTitle')}</h3>
      <address>
        <strong>${IMPRINT.operator}</strong><br />
        ${t('about.owner', { name: IMPRINT.owner })}<br />
        ${IMPRINT.street}<br />
        ${IMPRINT.city}<br />
        ${t('about.country')}
      </address>
      <dl class="check">
        ${this.#row(t('about.email'), html`<a href=${`mailto:${IMPRINT.email}`}>${IMPRINT.email}</a>`)}
        ${this.#row(t('about.web'), html`<a href=${IMPRINT.web} target="_blank" rel="noopener noreferrer">${IMPRINT.webLabel}</a>`)}
      </dl>
    </section>`;
  }

  override render() {
    return html`<lcm-modal ?open=${this.open} size="ml" heading=${t('about.title')} close-label=${t('about.close')} @lcm-close=${() => emit(this, 'lcm-close')}>
      ${this.open
        ? html`<div class="columns">
            <div>
              <div class="head">
                <lcm-logo label=${t('nav.brand')}></lcm-logo>
                <div>
                  <span class="name">LCM</span>
                  <span class="muted">© ${new Date().getFullYear()} ${IMPRINT.operator}</span>
                </div>
              </div>
              ${this.#version(this.#system.data)}
            </div>
            <div>${this.#updates(this.#update.data)} ${this.#imprint()}</div>
          </div>`
        : nothing}
    </lcm-modal>`;
  }
}
customElements.define('lcm-about-dialog', LcmAboutDialog);
