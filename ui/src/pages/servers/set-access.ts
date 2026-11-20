import { LitElement, html, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-input';
import { formLayoutStyles } from '../../components/layout-styles';
import { baseStyles } from '../../components/shared';
import { settingsApi, type ServerState } from '../../api/servers-settings';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { isPort } from '../../lib/validate';
import './set-action';
import { settingsStyles } from './set-styles';

/** Texte je Richtung: sperren, solange root sich anmelden darf, sonst wieder erlauben. */
const ROOT_TEXT = {
  deny: { label: 'serverPackages.settings.ssh.rootLogin.deny.label', help: 'serverPackages.settings.ssh.rootLogin.deny.help', heading: 'serverPackages.settings.ssh.rootLogin.deny.heading', action: 'ssh-root-login-lock', done: 'serverPackages.settings.ssh.rootLogin.deny.done' },
  allow: { label: 'serverPackages.settings.ssh.rootLogin.allow.label', help: 'serverPackages.settings.ssh.rootLogin.allow.help', heading: 'serverPackages.settings.ssh.rootLogin.allow.heading', action: 'ssh-root-login-unlock', done: 'serverPackages.settings.ssh.rootLogin.allow.done' },
} as const;

/** Zugang im Einstellungsfenster: direktes root-Login sperren oder erlauben und den SSH-Port auf dem Server umstellen. */
export class SetAccess extends LitElement {
  static override properties = {
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    state: { attribute: false },
    _port: { state: true },
  };
  static override styles = [baseStyles, formLayoutStyles, settingsStyles];

  declare serverId: number;
  declare serverName: string;
  declare state: ServerState;
  /** Eingabe im Port-Feld; undefined, solange der gespeicherte Port gilt. */
  declare _port: string | undefined;

  constructor() {
    super();
    this.serverId = 0;
    this.serverName = '';
    this._port = undefined;
    new I18nController(this);
  }

  get #current(): number {
    return this.state.form.port;
  }

  /** Agent-Server: LCM meldet sich dort nicht per SSH an; der gespeicherte Port ist nur eine Vorgabe, den echten liest LCM beim Umstellen. */
  get #agent(): boolean {
    return this.state.transport === 'agent';
  }

  /** Hilfetext, bei Agent-Servern in der eigenen Fassung (dort ist LCM nicht über SSH verbunden). */
  #help(ssh: string, agent: string): string {
    return t(this.#agent ? agent : ssh);
  }

  #renderRootLogin() {
    const disabled = this.state.rootLoginDisabled;
    const text = ROOT_TEXT[disabled ? 'allow' : 'deny'];
    return html`<h3>${t('serverPackages.settings.ssh.rootLogin.title')}</h3>
      <div class="row center">
        <lcm-badge tone=${disabled ? 'ok' : 'warn'}>${t(disabled ? 'serverPackages.settings.ssh.rootLogin.denied' : 'serverPackages.settings.ssh.rootLogin.allowed')}</lcm-badge>
        <lcm-set-action
          icon=${disabled ? 'undo' : 'lock'}
          server-id=${this.serverId}
          server-name=${this.serverName}
          label=${t(text.label)}
          help=${disabled ? t(text.help) : this.#help(ROOT_TEXT.deny.help, 'serverPackages.settings.ssh.rootLogin.deny.helpAgent')}
          heading=${t(text.heading, { name: this.serverName })}
          ?danger=${disabled}
          action=${text.action}
          done=${t(text.done, { name: this.serverName })}
          .run=${() => settingsApi.setRootLogin(this.serverId, !disabled)}
        ></lcm-set-action>
      </div>`;
  }

  #renderPort() {
    const wanted = this._port === undefined ? this.#current : Number(this._port);
    const valid = isPort(wanted);
    // Beim Agent kann der gespeicherte Port vom echten abweichen - umstellen lässt sich jede Eingabe, LCM prüft am Server.
    const changed = valid && (this.#agent ? this._port !== undefined : wanted !== this.#current);
    return html`<h3>${t('serverPackages.settings.ssh.port.title')}</h3>
      <div class="row">
        <lcm-input
          label=${t('serverPackages.settings.ssh.port.label')}
          width="xs"
          mono
          digits
          maxlength="5"
          .value=${this._port ?? String(this.#current)}
          error=${this._port !== undefined && !valid ? t('serverPackages.settings.ssh.port.invalid') : ''}
          @lcm-input=${(e: CustomEvent<{ value: string }>) => (this._port = e.detail.value)}
        ></lcm-input>
        <lcm-set-action
          icon="arrow-right"
          server-id=${this.serverId}
          server-name=${this.serverName}
          ?disabled=${!changed}
          label=${t('serverPackages.settings.ssh.port.apply.label')}
          help=${this.#help('serverPackages.settings.ssh.port.apply.help', 'serverPackages.settings.ssh.port.apply.helpAgent')}
          heading=${t('serverPackages.settings.ssh.port.apply.heading', { name: this.serverName })}
          action="ssh-port"
          .params=${{ port: wanted }}
          .impacts=${[{ label: t('serverPackages.settings.ssh.port.change'), value: `${this.#agent ? t('serverPackages.settings.ssh.port.unknown') : this.#current} → ${wanted}` }]}
          done=${t('serverPackages.settings.ssh.port.apply.done', { name: this.serverName, port: wanted })}
          .run=${async () => {
            const result = await settingsApi.changeSshPort(this.serverId, wanted);
            this._port = undefined;
            return result;
          }}
        ></lcm-set-action>
      </div>
      ${this.#agent ? html`<p class="note">${t('serverPackages.settings.ssh.port.agentHint')}</p>` : nothing}`;
  }

  override render() {
    return html`<div class="form-stack">${this.#renderRootLogin()} ${this.#renderPort()}</div>`;
  }
}
customElements.define('lcm-set-access', SetAccess);
