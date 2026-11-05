import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-input';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-switch';
import { toast } from '../../components/lcm-toaster';
import { baseStyles } from '../../components/shared';
import { refreshServer } from '../../api/servers';
import { DEFAULT_WARN_PERCENT, monitorBody, opsApi, parseThreshold, type OpsVolume } from '../../api/servers-ops';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';

/**
 * `<lcm-ops-volume-monitor server-id .volume>`: Überwachung eines Volumes ein- oder ausschalten und die Warnschwelle
 * setzen. Offen, solange `volume` gesetzt ist; meldet `lcm-close`.
 */
export class OpsVolumeMonitor extends LitElement {
  static override properties = {
    _modalError: { state: true },
    serverId: { type: Number, attribute: 'server-id' },
    volume: { attribute: false },
    _enabled: { state: true },
    _warn: { state: true },
    _busy: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      .form {
        display: flex;
        flex-direction: column;
        gap: var(--space-4);
      }
      p {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare serverId: number;
  declare volume: OpsVolume | null;
  /** Fehler der letzten Aktion im Fenster; steht über dem Fuß. */
  declare _modalError: string;
  declare _enabled: boolean;
  declare _warn: string;
  declare _busy: boolean;

  constructor() {
    super();
    this._modalError = '';
    this.serverId = 0;
    this.volume = null;
    this._enabled = false;
    this._warn = '';
    this._busy = false;
    new I18nController(this);
  }

  override willUpdate(changed: Map<string, unknown>): void {
    if (!changed.has('volume') || !this.volume) return;
    const previous = changed.get('volume') as OpsVolume | null | undefined;
    if (previous?.mountpoint === this.volume.mountpoint) return;
    this._enabled = this.volume.monitor !== null;
    this._warn = String(this.volume.monitor?.warn || DEFAULT_WARN_PERCENT);
  }

  #close(): void {
    this._modalError = '';
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  get #threshold(): number | null {
    return parseThreshold(this._warn);
  }

  /** Etwas geändert: ein- oder ausgeschaltet, oder bei eingeschalteter Überwachung eine andere Schwelle. */
  #dirty(volume: OpsVolume): boolean {
    const wasOn = volume.monitor !== null;
    if (this._enabled !== wasOn) return true;
    return this._enabled && this.#threshold !== (volume.monitor?.warn || DEFAULT_WARN_PERCENT);
  }

  async #save(volume: OpsVolume): Promise<void> {
    const warn = this.#threshold;
    if (this._enabled && warn === null) return;
    this._busy = true;
    this._modalError = '';
    try {
      await opsApi.setVolumeMonitor(this.serverId, monitorBody(volume, this._enabled, warn ?? 0));
      toast({ tone: 'ok', title: this._enabled ? t('serverOps.storage.monitor.savedOn', { mount: volume.mountpoint, percent: warn ?? 0 }) : t('serverOps.storage.monitor.savedOff', { mount: volume.mountpoint }) });
      refreshServer(this.serverId);
      this.#close();
    } catch (error) {
      this._modalError = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #body(volume: OpsVolume) {
    const invalid = this._enabled && this.#threshold === null;
    return html`<div class="form">
      <lcm-switch
        label=${t('serverOps.storage.monitor.switch')}
        .checked=${this._enabled}
        on-label=${t('serverOps.storage.monitor.onLabel')}
        off-label=${t('serverOps.storage.monitor.offLabel')}
        on-text=${t('serverOps.storage.monitor.on')}
        off-text=${t('serverOps.storage.monitor.off')}
        @lcm-change=${(e: CustomEvent<{ checked: boolean }>) => (this._enabled = e.detail.checked)}
      ></lcm-switch>
      ${this._enabled
        ? html`<lcm-input
            width="xs"
            digits
            maxlength="3"
            label=${t('serverOps.storage.monitor.threshold')}
            .value=${this._warn}
            error=${invalid ? t('serverOps.storage.monitor.invalid') : ''}
            @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._warn = e.detail.value)}
          ></lcm-input>`
        : nothing}
      <p>${t('serverOps.storage.monitor.current', { percent: volume.percent })}</p>
    </div>`;
  }

  override render() {
    const volume = this.volume;
    const invalid = this._enabled && this.#threshold === null;
    return html`<lcm-modal
      ?open=${volume !== null}
      size="s"
      heading=${t('serverOps.storage.monitor.title', { mount: volume?.mountpoint ?? '' })}
      close-label=${t('servers.confirm.cancel')}
      @lcm-close=${(e: Event) => {
        e.stopPropagation();
        this.#close();
      }}
    >
      ${volume ? this.#body(volume) : nothing}
      ${this._modalError ? html`<lcm-notice slot="note" tone="crit">${this._modalError}</lcm-notice>` : nothing}
      <lcm-button slot="footer" variant="ghost" @click=${() => this.#close()}>${t('servers.confirm.cancel')}</lcm-button>
      <lcm-button slot="footer" variant="primary" ?busy=${this._busy} ?disabled=${invalid || !volume || !this.#dirty(volume)} @click=${() => volume && void this.#save(volume)}>${t('serverOps.storage.monitor.save')}</lcm-button>
    </lcm-modal>`;
  }
}
customElements.define('lcm-ops-volume-monitor', OpsVolumeMonitor);
