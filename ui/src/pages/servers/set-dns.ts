import { LitElement, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-input';
import { formLayoutStyles } from '../../components/layout-styles';
import { baseStyles } from '../../components/shared';
import { GLOBAL_SETTINGS_URL, MAX_DNS_SERVERS, addPreset, cleanList, fillSlots, normalisePresets, sameList, settingsApi, type GlobalPresets, type Preset, type ServerState } from '../../api/servers-settings';
import { auth } from '../../lib/auth';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { validNameserver } from './set-model';
import './set-action';
import { settingsStyles } from './set-styles';

/** Längste IP-Schreibweise: IPv6 mit eingebetteter IPv4 (45 Zeichen). */
const IP_MAX = 45;

/** Vorgaben als kleine Knöpfe; ein Klick setzt den Wert in das erste freie Feld. */
export function renderPresets(presets: readonly Preset[], label: string, onPick: (value: string) => void): TemplateResult | typeof nothing {
  if (presets.length === 0) return nothing;
  return html`<div class="presets">
    <span class="label">${label}</span>
    ${presets.map((p) => html`<lcm-button size="small" variant="ghost" icon="plus" title=${p.value} @click=${() => onPick(p.value)}>${p.label === p.value ? p.value : `${p.label} · ${p.value}`}</lcm-button>`)}
  </div>`;
}

/** Vorgaben aus den globalen Einstellungen; nur wer sie verwalten darf, kann sie lesen. */
export function presetsData(host: LitElement): LiveData<GlobalPresets> {
  return new LiveData<GlobalPresets>(host, { url: () => (auth.can('settings:manage') ? GLOBAL_SETTINGS_URL : null), transform: normalisePresets });
}

/**
 * DNS im Einstellungsfenster: bis zu vier Nameserver, die LCM auf dem Server einträgt (leer = Verwaltung freigeben).
 * Fehler zeigt ein Feld erst, wenn man es verlassen hat; solange eins ungültig ist, bleibt „DNS anwenden“ gesperrt.
 */
export class SetDns extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    state: { attribute: false },
    _slots: { state: true },
    _left: { state: true },
  };
  static override styles = [baseStyles, formLayoutStyles, settingsStyles];

  declare serverId: number;
  declare serverName: string;
  declare state: ServerState;
  declare _slots: string[] | undefined;
  /** Felder, die man schon verlassen hat. */
  declare _left: ReadonlySet<number>;

  readonly #presets = presetsData(this);

  constructor() {
    super();
    this.serverId = 0;
    this.serverName = '';
    this._slots = undefined;
    this._left = new Set();
    new I18nController(this);
  }

  get #slots(): string[] {
    return this._slots ?? fillSlots(this.state.dnsServers, MAX_DNS_SERVERS);
  }

  #set(index: number, value: string): void {
    this._slots = this.#slots.map((slot, i) => (i === index ? value : slot));
  }

  #slot(value: string, i: number): TemplateResult {
    const error = this._left.has(i) && !validNameserver(value) ? t('serverPackages.settings.dns.invalid') : '';
    const last = i === MAX_DNS_SERVERS - 1;
    return html`<lcm-input
      label=${t('serverPackages.settings.dns.server', { n: i + 1 })}
      width="full"
      mono
      maxlength=${IP_MAX}
      .value=${value}
      placeholder=${i === 0 ? '1.1.1.1' : ''}
      hint=${last ? t('serverPackages.settings.dns.fourthHint') : ''}
      error=${error}
      @lcm-input=${(e: CustomEvent<{ value: string }>) => this.#set(i, e.detail.value)}
      @lcm-change=${() => (this._left = new Set(this._left).add(i))}
    ></lcm-input>`;
  }

  override render() {
    const slots = this.#slots;
    const wanted = cleanList(slots);
    const valid = slots.every(validNameserver);
    const changed = !sameList(wanted, this.state.dnsServers);
    return html`<div class="form-stack">
      <h3>${t('serverPackages.settings.dns.title')}</h3>
      <div class="grid-2">${slots.map((value, i) => this.#slot(value, i))}</div>
      <p class="note">${t('serverPackages.settings.dns.note')}</p>
      ${renderPresets(this.#presets.data?.dns ?? [], t('serverPackages.settings.dns.presets'), (value) => (this._slots = addPreset(slots, value)))}
      <div class="row">
        <lcm-set-action
          icon="globe"
          variant="primary"
          server-id=${this.serverId}
          server-name=${this.serverName}
          ?disabled=${!changed || !valid}
          label=${t('serverPackages.settings.dns.apply.label')}
          help=${t('serverPackages.settings.dns.apply.help')}
          heading=${t('serverPackages.settings.dns.apply.heading', { name: this.serverName })}
          action="dns"
          .params=${{ servers: wanted }}
          done=${t('serverPackages.settings.dns.apply.done', { name: this.serverName })}
          .run=${async () => {
            const result = await settingsApi.configureDns(this.serverId, wanted);
            this._slots = undefined;
            this._left = new Set();
            return result;
          }}
        ></lcm-set-action>
      </div>
    </div>`;
  }
}
customElements.define('lcm-set-dns', SetDns);
