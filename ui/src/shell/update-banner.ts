import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../components/lcm-button';
import '../components/lcm-dialog';
import '../components/lcm-notice';
import { SELF_UPDATE_URL, UPDATE_INFO_URL, normaliseSelfUpdate, normaliseUpdateInfo, systemApi, type SelfUpdate, type UpdateInfo } from '../api/system';
import type { Impact } from '../components/lcm-dialog';
import { baseStyles, utilStyles } from '../components/shared';
import { auth } from '../lib/auth';
import { I18nController, SubscribeController } from '../lib/controllers';
import { errorText, t } from '../lib/i18n';
import { LiveData } from '../lib/live-data';
import { store } from '../lib/store';
import { updateBanner, updateState, type UpdateBanner } from './update-model';

/**
 * <lcm-update-banner> – Band unter dem Hauptmenü (`lcm-notice variant="bar"`): neue LCM-Version mit „Jetzt installieren“, Fortschritt des
 * Selbst-Updates (Warten auf Jobs, Sicherung, Installation).
 * Den Fortschritt meldet das Ereignis `system.build` (Thema `system`), kein Abfragen im Takt.
 */
export class LcmUpdateBanner extends LitElement {
  static override properties = { _confirm: { state: true }, _starting: { state: true }, _error: { state: true } };
  static override styles = [
    baseStyles,
    utilStyles,
    css`
      :host {
        display: block;
      }
      a {
        color: inherit;
        font-weight: var(--fw-semibold);
      }
    `,
  ];

  declare _confirm: boolean;
  declare _starting: boolean;
  /** Fehler beim Anstoßen; steht im Bestätigungsdialog, der dafür offen bleibt. */
  declare _error: string;

  readonly #info = new LiveData<UpdateInfo>(this, { url: () => (auth.isAuthenticated ? UPDATE_INFO_URL : null), transform: normaliseUpdateInfo });
  readonly #self = new LiveData<SelfUpdate>(this, {
    url: () => (this.#manages ? SELF_UPDATE_URL : null),
    topics: () => (this.#manages ? ['system'] : []),
    transform: normaliseSelfUpdate,
  });

  constructor() {
    super();
    this._confirm = false;
    this._starting = false;
    this._error = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new SubscribeController(this, (listener) => updateState.onChange(listener));
  }

  get #manages(): boolean {
    return auth.isAuthenticated && auth.can('settings:manage');
  }

  /** Ein gescheitertes Update zeigt das Band selbst (Phase `failed`); nur ein abgelehnter Start bleibt im Dialog. */
  async #start(): Promise<void> {
    this._starting = true;
    this._error = '';
    try {
      await systemApi.startSelfUpdate();
      store.invalidate(SELF_UPDATE_URL);
      this._confirm = false;
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._starting = false;
    }
  }

  #busy(status: SelfUpdate): TemplateResult {
    const version = status.target_version;
    const text =
      status.waiting_for.length > 0
        ? t('update.waiting', { version, jobs: status.waiting_for.join(', ') })
        : status.phase === 'backup'
          ? t('update.backup', { version })
          : t('update.installing', { version });
    return html`<lcm-notice variant="bar" tone="warn"
      >${text} ${status.backup_file ? html`<span class="muted">${t('update.backupFile', { file: status.backup_file })}</span>` : nothing}</lcm-notice
    >`;
  }

  #available(info: UpdateInfo, self: SelfUpdate | null): TemplateResult {
    const failed = self?.phase === 'failed' && !!self.error;
    return html`<lcm-notice
      variant="bar"
      tone=${failed ? 'crit' : 'info'}
      heading=${failed ? t('update.failed') : ''}
      dismissible
      close-label=${t('update.dismiss')}
      @lcm-dismiss=${() => updateState.dismiss(info.latest_version)}
    >
      ${failed ? html`${self!.error} ` : nothing}${t('update.available', { latest: info.latest_version, current: info.current_version })}
      ${info.latest_url ? html`<a href=${info.latest_url} target="_blank" rel="noopener noreferrer">${t('update.release')}</a>` : nothing}
      ${!self?.supported && self?.reason ? html`<span class="muted">${self.reason}</span>` : nothing}
      ${self?.supported
        ? html`<lcm-button slot="action" size="small" variant="primary" icon="download" ?busy=${this._starting} @click=${() => (this._confirm = true)}>${t('update.install')}</lcm-button>`
        : nothing}
    </lcm-notice>`;
  }

  #impacts(banner: UpdateBanner): Impact[] {
    if (banner?.kind !== 'available') return [];
    return [
      { label: t('update.impact.version'), value: `${banner.info.current_version} → ${banner.info.latest_version}` },
      { label: t('update.impact.backup'), value: t('update.impact.backupValue'), tone: 'ok' },
      { label: t('update.impact.jobs'), value: t('update.impact.jobsValue') },
      { label: t('update.impact.downtime'), value: t('update.impact.downtimeValue'), tone: 'warn' },
    ];
  }

  #render(banner: UpdateBanner): TemplateResult | typeof nothing {
    if (!banner) return nothing;
    if (banner.kind === 'busy') return this.#busy(banner.status);
    return this.#available(banner.info, banner.self);
  }

  override render() {
    const banner = auth.isAuthenticated ? updateBanner(this.#info.data, this.#self.data, updateState.dismissed) : null;
    return html`${this.#render(banner)}
      <lcm-dialog
        ?open=${this._confirm && banner?.kind === 'available'}
        heading=${banner?.kind === 'available' ? t('update.confirm.title', { version: banner.info.latest_version }) : ''}
        message=${t('update.confirm.message')}
        .impacts=${this.#impacts(banner)}
        confirm-label=${t('update.install')}
        cancel-label=${t('update.cancel')}
        ?busy=${this._starting}
        error=${this._error}
        @lcm-confirm=${() => void this.#start()}
        @lcm-cancel=${() => ((this._confirm = false), (this._error = ''))}
      ></lcm-dialog>`;
  }
}
customElements.define('lcm-update-banner', LcmUpdateBanner);
