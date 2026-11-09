import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../components/lcm-button';
import '../components/lcm-dialog';
import '../components/lcm-modal';
import '../components/lcm-notice';
import {
  MAINTENANCE_URL,
  SELF_UPDATE_URL,
  UPDATE_INFO_URL,
  normaliseMaintenance,
  normaliseSelfUpdate,
  normaliseUpdateInfo,
  systemApi,
  type Maintenance,
  type SelfUpdate,
  type UpdateInfo,
} from '../api/system';
import type { Impact } from '../components/lcm-dialog';
import { icon } from '../components/icons';
import { baseStyles, utilStyles } from '../components/shared';
import { auth } from '../lib/auth';
import { I18nController, SubscribeController } from '../lib/controllers';
import { errorText, t } from '../lib/i18n';
import { LiveData } from '../lib/live-data';
import { store } from '../lib/store';
import { versionWatcher } from '../lib/version';
import { updateBanner, updateState, updateSteps, type UpdateBanner, type UpdateStep } from './update-model';

const STEP_LABEL: Record<UpdateStep['key'], string> = {
  jobs: 'update.step.jobs',
  backup: 'update.step.backup',
  install: 'update.step.install',
};
const STATE_LABEL: Record<UpdateStep['state'], string> = {
  done: 'update.stepState.done',
  current: 'update.stepState.current',
  pending: 'update.stepState.pending',
};

/**
 * <lcm-update-banner> – Band unter dem Hauptmenü (`lcm-notice variant="bar"`): neue LCM-Version mit „Jetzt installieren“, Fortschritt des
 * Selbst-Updates (Warten auf Jobs, Sicherung, Installation).
 * Nach dem Bestätigen bleibt ein Fenster mit dem Fortschritt offen, bis LCM neu gestartet ist. Von der Anforderung bis zum Neustart
 * ist LCM für alle gesperrt: Das Band sagt das jedem Benutzer, auch ohne Recht auf die Einstellungen.
 * Den Fortschritt meldet das Ereignis `system.build`, die Sperre `system.maintenance` (beide Thema `system`), kein Abfragen im Takt.
 */
export class LcmUpdateBanner extends LitElement {
  static override properties = {
    _confirm: { state: true },
    _starting: { state: true },
    _error: { state: true },
    _progress: { state: true },
    _cancelling: { state: true },
    _cancelError: { state: true },
  };
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
      .lead {
        margin: 0 0 var(--space-4);
        color: var(--text-2);
      }
      ol {
        display: grid;
        gap: var(--space-3);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      li {
        display: grid;
        grid-template-columns: 1.25rem 1fr;
        gap: var(--space-1) var(--space-3);
        align-items: start;
      }
      .mark {
        display: grid;
        place-items: center;
        inline-size: 1.25rem;
        block-size: 1.25rem;
        margin-block-start: 0.1rem;
        color: var(--text-3);
      }
      .mark .icon {
        inline-size: 1rem;
        block-size: 1rem;
      }
      .done .mark {
        color: var(--ok-text);
      }
      .pending .mark::before {
        content: '';
        inline-size: 0.6rem;
        block-size: 0.6rem;
        border: 1.5px solid currentColor;
        border-radius: 50%;
      }
      .current .mark::before {
        content: '';
        inline-size: 0.9rem;
        block-size: 0.9rem;
        border: 2px solid var(--accent);
        border-inline-end-color: transparent;
        border-radius: 50%;
        animation: spin var(--dur-spin) linear infinite;
      }
      .label {
        font-weight: var(--fw-medium);
      }
      .pending .label {
        color: var(--text-2);
      }
      .detail {
        grid-column: 2;
        color: var(--text-2);
        font-size: var(--fs-s);
        overflow-wrap: anywhere;
      }
      @keyframes spin {
        to {
          transform: rotate(360deg);
        }
      }
      @media (prefers-reduced-motion: reduce) {
        .current .mark::before {
          animation: none;
          border-inline-end-color: var(--accent);
        }
      }
    `,
  ];

  declare _confirm: boolean;
  declare _starting: boolean;
  /** Fehler beim Anstoßen; steht im Bestätigungsdialog, der dafür offen bleibt. */
  declare _error: string;
  /** Das Fortschrittsfenster ist offen. Es öffnet sich nach dem Bestätigen und lässt sich über das Band wieder öffnen. */
  declare _progress: boolean;
  declare _cancelling: boolean;
  declare _cancelError: string;
  /** Die zuletzt gesehene Phase: Am Übergang zurück nach „idle“ erkennt das Fenster Neustart oder Abbruch. */
  #lastPhase: SelfUpdate['phase'] | undefined;
  /** LCM ist nach dem Update neu gestartet; das Fenster bietet das Neuladen an. */
  #restarted = false;

  readonly #info = new LiveData<UpdateInfo>(this, { url: () => (auth.isAuthenticated ? UPDATE_INFO_URL : null), transform: normaliseUpdateInfo });
  readonly #self = new LiveData<SelfUpdate>(this, {
    url: () => (this.#manages ? SELF_UPDATE_URL : null),
    topics: () => (this.#manages ? ['system'] : []),
    transform: normaliseSelfUpdate,
  });
  /** Die Sperre für alle, die den Fortschritt nicht sehen dürfen. */
  readonly #lock = new LiveData<Maintenance>(this, {
    url: () => (auth.isAuthenticated && !this.#manages ? MAINTENANCE_URL : null),
    topics: () => (auth.isAuthenticated && !this.#manages ? ['system'] : []),
    transform: normaliseMaintenance,
  });

  constructor() {
    super();
    this._confirm = false;
    this._starting = false;
    this._error = '';
    this._progress = false;
    this._cancelling = false;
    this._cancelError = '';
    new I18nController(this);
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new SubscribeController(this, (listener) => updateState.onChange(listener));
  }

  get #manages(): boolean {
    return auth.isAuthenticated && auth.can('settings:manage');
  }

  /** Ein gescheitertes Update zeigt das Fortschrittsfenster selbst; nur ein abgelehnter Start bleibt im Dialog. */
  async #start(): Promise<void> {
    this._starting = true;
    this._error = '';
    try {
      await systemApi.startSelfUpdate();
      store.invalidate(SELF_UPDATE_URL);
      this._confirm = false;
      this._progress = true;
      this._cancelError = '';
      this.#restarted = false;
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._starting = false;
    }
  }

  async #cancel(): Promise<void> {
    this._cancelling = true;
    this._cancelError = '';
    try {
      await systemApi.cancelSelfUpdate();
      store.invalidate(SELF_UPDATE_URL);
      this._progress = false;
    } catch (error) {
      this._cancelError = errorText(error);
    } finally {
      this._cancelling = false;
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
      >${text} <span class="muted">${t('update.lockedAll')}</span>
      <lcm-button slot="action" size="small" variant="outline" @click=${() => (this._progress = true)}>${t('update.showProgress')}</lcm-button>
    </lcm-notice>`;
  }

  #locked(lock: Maintenance): TemplateResult {
    return html`<lcm-notice variant="bar" tone="warn">${t('update.lockedOther', { version: lock.target_version })}</lcm-notice>`;
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
      { label: t('update.impact.lock'), value: t('update.impact.lockValue'), tone: 'warn' },
      { label: t('update.impact.downtime'), value: t('update.impact.downtimeValue'), tone: 'warn' },
    ];
  }

  #render(banner: UpdateBanner): TemplateResult | typeof nothing {
    if (banner) return banner.kind === 'busy' ? this.#busy(banner.status) : this.#available(banner.info, banner.self);
    const lock = this.#lock.data;
    return lock?.active ? this.#locked(lock) : nothing;
  }

  #stepDetail(step: UpdateStep, status: SelfUpdate): string {
    switch (step.key) {
      case 'jobs':
        if (step.state === 'current') return status.waiting_for.length ? t('update.step.jobsWaiting', { jobs: status.waiting_for.join(', ') }) : t('update.step.jobsChecking');
        return step.state === 'done' ? t('update.step.jobsDone') : '';
      case 'backup':
        return status.backup_file ? t('update.backupFile', { file: status.backup_file }) : '';
      case 'install':
        return step.state === 'current' ? t('update.step.installHint') : '';
    }
  }

  #steps(status: SelfUpdate): TemplateResult {
    return html`<ol aria-live="polite">
      ${updateSteps(status).map((step) => {
        const detail = this.#stepDetail(step, status);
        return html`<li class=${step.state} aria-current=${step.state === 'current' ? 'step' : 'false'}>
          <span class="mark" aria-hidden="true">${step.state === 'done' ? icon('check') : nothing}</span>
          <span class="label">${t(STEP_LABEL[step.key])}<span class="sr-only"> – ${t(STATE_LABEL[step.state])}</span></span>
          ${detail ? html`<span class="detail">${detail}</span>` : nothing}
        </li>`;
      })}
    </ol>`;
  }

  /** Inhalt und Fuß des Fortschrittsfensters, je nach Stand: läuft, gescheitert oder neu gestartet. */
  #progressContent(status: SelfUpdate | undefined): { body: TemplateResult; footer: TemplateResult | typeof nothing } {
    if (status?.phase === 'failed') {
      return {
        body: html`<lcm-notice tone="crit" heading=${t('update.progress.failed')}>${status.error}</lcm-notice>`,
        footer: html`<lcm-button slot="footer" variant="primary" @click=${() => (this._progress = false)}>${t('update.close')}</lcm-button>`,
      };
    }
    if (this.#restarted) {
      return {
        body: html`<lcm-notice tone="ok">${t('update.restarted')}</lcm-notice>`,
        footer: html`<lcm-button slot="footer" variant="primary" icon="refresh" @click=${() => versionWatcher.reloadNow()}>${t('update.reloadNow')}</lcm-button>`,
      };
    }
    const body = html`<p class="lead">${t('update.progress.lock')}</p>
      ${status ? this.#steps(status) : nothing}`;
    const footer =
      status?.phase === 'waiting'
        ? html`<lcm-button slot="footer" variant="ghost" ?busy=${this._cancelling} @click=${() => void this.#cancel()}>${t('update.abort')}</lcm-button>`
        : nothing;
    return { body, footer };
  }

  #renderProgress(): TemplateResult | typeof nothing {
    if (!this.#manages) return nothing;
    const status = this.#self.data;
    const { body, footer } = this.#progressContent(status);
    const version = status?.target_version || this.#info.data?.latest_version || '';
    return html`<lcm-modal
      ?open=${this._progress}
      size="s"
      heading=${t('update.progress.title', { version })}
      close-label=${t('update.close')}
      @lcm-close=${() => (this._progress = false)}
    >
      ${body}
      ${this._cancelError ? html`<lcm-notice slot="note" variant="line" tone="crit">${this._cancelError}</lcm-notice>` : nothing} ${footer}
    </lcm-modal>`;
  }

  /**
   * Fällt die Phase auf „idle“ zurück, ist entweder LCM neu gestartet (nach Sicherung oder Einspielen kommt nur noch der
   * Neustart) oder das Warten wurde abgebrochen, womöglich von einem anderen Administrator. Dann schließt das Fenster.
   */
  override willUpdate(): void {
    const phase = this.#self.data?.phase;
    if (phase === this.#lastPhase) return;
    const was = this.#lastPhase;
    this.#lastPhase = phase;
    if (phase !== 'idle' || !was) return;
    if (was === 'backup' || was === 'running') this.#restarted = true;
    else if (was === 'waiting') this._progress = false;
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
      ></lcm-dialog>
      ${this.#renderProgress()}`;
  }
}
customElements.define('lcm-update-banner', LcmUpdateBanner);
