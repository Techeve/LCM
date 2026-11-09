import { LitElement, html, type TemplateResult } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-input';
import { formLayoutStyles } from '../../components/layout-styles';
import { baseStyles } from '../../components/shared';
import { clockState, platformOf } from '../../api/server-detail';
import { MAX_NTP_SERVERS, addPreset, cleanList, fillSlots, settingsApi, type ServerState } from '../../api/servers-settings';
import { I18nController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LIMITS } from '../../lib/validate';
import './set-action';
import { presetsData, renderPresets } from './set-dns';
import { validTimeServer, validTimezone } from './set-model';
import { settingsStyles } from './set-styles';

const ZONE_EXAMPLE = 'Europe/Berlin';
const ZONE_MAX = 64;

/** Zeit im Einstellungsfenster: Stand der Uhr prüfen, Zeitzone setzen, Zeitserver einrichten (nicht in Containern). */
export class SetTime extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    state: { attribute: false },
    _zone: { state: true },
    _slots: { state: true },
    _left: { state: true },
  };
  static override styles = [baseStyles, formLayoutStyles, settingsStyles];

  declare serverId: number;
  declare serverName: string;
  declare state: ServerState;
  declare _zone: string | undefined;
  declare _slots: string[] | undefined;
  /** Felder, die man schon verlassen hat (Zeitzone = -1, Zeitserver ab 0); erst dann zeigen sie Fehler. */
  declare _left: ReadonlySet<number>;

  readonly #presets = presetsData(this);

  constructor() {
    super();
    this.serverId = 0;
    this.serverName = '';
    this._zone = undefined;
    this._slots = undefined;
    this._left = new Set();
    new I18nController(this);
  }

  get #zone(): string {
    return this._zone ?? (this.state.timezone || this.#presets.data?.timezone || '');
  }

  /** Vorbelegung wie früher: die eingetragenen Zeitserver, sonst die ersten zwei Vorgaben. */
  get #slots(): string[] {
    if (this._slots) return this._slots;
    const current = this.state.ntpServers.length > 0 ? this.state.ntpServers : (this.#presets.data?.ntp ?? []).slice(0, 2).map((p) => p.value);
    return fillSlots(current, MAX_NTP_SERVERS);
  }

  #leave(field: number): void {
    this._left = new Set(this._left).add(field);
  }

  #clock(): TemplateResult {
    const s = this.state;
    const state = clockState({ time_checked_at: s.timeCheckedAt, clock_offset_seconds: s.clockOffset });
    if (state === 'unknown') return html`<span class="note">${t('serverDetail.facts.clock.never')}</span>`;
    const label = t(`serverDetail.facts.clock.${state}`, { seconds: Math.abs(s.clockOffset) });
    return html`<lcm-badge tone=${state === 'ok' ? 'ok' : 'warn'}>${label}</lcm-badge> <span class="note">${t('serverPackages.settings.time.checkedAt', { date: formatDateTime(s.timeCheckedAt) })}</span>`;
  }

  #renderState(): TemplateResult {
    const s = this.state;
    const service = s.ntpService ? `${s.ntpService} · ${t(s.ntpSynchronized ? 'serverDetail.facts.clock.synced' : 'serverDetail.facts.clock.notSynced')}` : t('serverDetail.facts.clock.noNtp');
    return html`<dl class="kv">
        <dt>${t('serverDetail.facts.clock.label')}</dt>
        <dd>${this.#clock()}</dd>
        <dt>${t('serverPackages.settings.time.zone')}</dt>
        <dd class="mono">${s.timezone || '–'}</dd>
        <dt>${t('serverPackages.settings.time.service')}</dt>
        <dd>${service}</dd>
        <dt>${t('serverPackages.settings.time.servers')}</dt>
        <dd class="mono">${s.ntpServers.join(', ') || '–'}</dd>
      </dl>
      <div class="row">
        <lcm-set-action
          no-confirm
          icon="clock"
          server-id=${this.serverId}
          server-name=${this.serverName}
          label=${t('serverPackages.settings.time.check.label')}
          help=${t('serverPackages.settings.time.check.help')}
          done=${t('serverPackages.settings.time.check.done', { name: this.serverName })}
          .run=${async () => void (await settingsApi.timeCheck(this.serverId))}
        ></lcm-set-action>
      </div>`;
  }

  #renderZone(): TemplateResult {
    const zone = this.#zone.trim();
    const valid = validTimezone(zone);
    return html`<h3>${t('serverPackages.settings.time.zone')}</h3>
      <div class="row">
        <lcm-input
          label=${t('serverPackages.settings.time.zoneLabel')}
          mono
          maxlength=${ZONE_MAX}
          placeholder=${ZONE_EXAMPLE}
          .value=${this.#zone}
          error=${this._left.has(-1) && zone !== '' && !valid ? t('serverPackages.settings.time.invalidZone') : ''}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._zone = e.detail.value)}
          @lcm-change=${() => this.#leave(-1)}
        ></lcm-input>
        <lcm-set-action
          icon="globe"
          server-id=${this.serverId}
          server-name=${this.serverName}
          ?disabled=${!valid || zone === this.state.timezone}
          label=${t('serverPackages.settings.time.setZone.label')}
          help=${t('serverPackages.settings.time.setZone.help')}
          heading=${t('serverPackages.settings.time.setZone.heading', { name: this.serverName })}
          action="timezone"
          .params=${{ timezone: zone }}
          .impacts=${[{ label: t('serverPackages.settings.time.zone'), value: `${this.state.timezone || '–'} → ${zone}` }]}
          done=${t('serverPackages.settings.time.setZone.done', { name: this.serverName, zone })}
          .run=${async () => {
            const result = await settingsApi.setTimezone(this.serverId, zone);
            this._zone = undefined;
            this._left = new Set();
            return result;
          }}
        ></lcm-set-action>
      </div>`;
  }

  #renderNtp(): TemplateResult {
    const s = this.state;
    const platform = platformOf(s.virtualization);
    if (platform.kind === 'container') return html`<h3>${t('serverPackages.settings.time.servers')}</h3><p class="note">${t('serverPackages.settings.time.container', { type: s.virtualization })}</p>`;
    const slots = this.#slots;
    const wanted = cleanList(slots);
    const valid = slots.every(validTimeServer);
    return html`<h3>${t('serverPackages.settings.time.servers')}</h3>
      <div class="slots">
        ${slots.map(
          (value, i) =>
            html`<lcm-input
              label=${t('serverPackages.settings.time.server', { n: i + 1 })}
              mono
              maxlength=${LIMITS.host}
              .value=${value}
              error=${this._left.has(i) && !validTimeServer(value) ? t('serverPackages.settings.time.invalidServer') : ''}
              @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._slots = slots.map((slot, j) => (j === i ? e.detail.value : slot)))}
              @lcm-change=${() => this.#leave(i)}
            ></lcm-input>`,
        )}
      </div>
      ${renderPresets(this.#presets.data?.ntp ?? [], t('serverPackages.settings.time.presets'), (value) => (this._slots = addPreset(slots, value)))}
      <div class="row">
        <lcm-set-action
          icon="clock"
          variant="primary"
          server-id=${this.serverId}
          server-name=${this.serverName}
          ?disabled=${wanted.length === 0 || !valid}
          label=${t('serverPackages.settings.time.ntp.label')}
          help=${t('serverPackages.settings.time.ntp.help')}
          heading=${t('serverPackages.settings.time.ntp.heading', { name: this.serverName })}
          action="ntp"
          .params=${{ servers: wanted }}
          done=${t('serverPackages.settings.time.ntp.done', { name: this.serverName })}
          .run=${async () => {
            const result = await settingsApi.configureNtp(this.serverId, wanted);
            this._slots = undefined;
            this._left = new Set();
            return result;
          }}
        ></lcm-set-action>
      </div>`;
  }

  override render() {
    return html`<div class="form-stack">${this.#renderState()} ${this.#renderZone()} ${this.#renderNtp()}</div>`;
  }
}
customElements.define('lcm-set-time', SetTime);
