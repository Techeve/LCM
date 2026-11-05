import { LitElement, css, html, nothing, unsafeCSS } from 'lit';
import { FitAddon } from '@xterm/addon-fit';
import { Terminal } from '@xterm/xterm';
import xtermStyles from '@xterm/xterm/css/xterm.css?inline';
import '../../components/lcm-button';
import '../../components/lcm-notice';
import { icon } from '../../components/icons';
import { modalClosed, modalOpened } from '../../components/modal-watch';
import { baseStyles, dialogStyles } from '../../components/shared';
import { serversApi } from '../../api/servers';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';
import { openStyled } from './xterm-styles';

type State = 'idle' | 'connecting' | 'open' | 'closed' | 'error';

const SCREEN_BACKGROUND = '#0f1320';

/**
 * `<lcm-srv-terminal open server-id server-name>`: Web-Konsole in einem großen Dialog. Der Aufbau ist zweistufig:
 * Erst holt der angemeldete Browser eine Einmal-Fahrkarte (`terminal/ticket`), dann öffnet die WebSocket-Verbindung
 * mit ihr, denn ein Zugangstoken gehört nicht in eine URL. Meldet `lcm-close`.
 */
export class SrvTerminal extends LitElement {
  static override properties = {
    open: { type: Boolean, reflect: true },
    serverId: { type: Number, attribute: 'server-id' },
    serverName: { type: String, attribute: 'server-name' },
    _state: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    dialogStyles,
    unsafeCSS(xtermStyles),
    css`
      :host {
        display: contents;
      }
      dialog {
        inline-size: min(70rem, 96vw);
        max-inline-size: 96vw;
        overflow: auto;
      }
      header,
      footer {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-3);
        padding: var(--space-4) var(--space-5);
      }
      h2 {
        margin: 0;
        font-size: var(--fs-xl);
      }
      lcm-notice {
        margin: 0 var(--space-5) var(--space-3);
      }
      .screen {
        block-size: min(32rem, 60dvh);
        margin: 0 var(--space-5) var(--space-4);
        overflow: hidden;
        border-radius: var(--radius-m);
        background: ${unsafeCSS(SCREEN_BACKGROUND)};
      }
      /* Der Abstand gehört an xterm selbst: Nur dessen Innenabstand rechnet das Fit-Addon heraus. */
      .xterm {
        padding: var(--space-2);
      }
      .xterm .xterm-viewport {
        background-color: transparent;
      }
      .close {
        display: grid;
        place-items: center;
        inline-size: var(--control-h);
        block-size: var(--control-h);
        border: 0;
        border-radius: var(--radius-m);
        background: transparent;
        color: var(--text-2);
        cursor: pointer;
      }
      .close:hover {
        background: var(--surface-2);
      }
    `,
  ];

  declare open: boolean;
  declare serverId: number;
  declare serverName: string;
  declare _state: State;
  declare _error: string;

  #term: Terminal | undefined;
  #socket: WebSocket | undefined;
  #observer: ResizeObserver | undefined;
  #removeStyles: (() => void) | undefined;

  constructor() {
    super();
    this.open = false;
    this.serverId = 0;
    this.serverName = '';
    this._state = 'idle';
    this._error = '';
    new I18nController(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#stop();
    modalClosed(this);
  }

  get #dialog(): HTMLDialogElement | null {
    return this.renderRoot.querySelector('dialog');
  }

  override updated(): void {
    const dialog = this.#dialog;
    if (!dialog) return;
    if (this.open && !dialog.open) {
      dialog.showModal();
      modalOpened(this);
    } else if (!this.open && dialog.open) {
      this.#stop();
      dialog.close();
      modalClosed(this);
    }
  }

  async #connect(): Promise<void> {
    if (this._state === 'connecting' || this._state === 'open') return;
    this._state = 'connecting';
    this._error = '';
    try {
      const { ticket } = await serversApi.terminalTicket(this.serverId);
      // Der Dialog wurde während des Wartens geschlossen: keine Sitzung öffnen, die niemand sieht.
      if (this._state !== 'connecting') return;
      this.#start(ticket);
    } catch (error) {
      this._error = errorText(error);
      this._state = 'error';
    }
  }

  #start(ticket: string): void {
    const host = this.renderRoot.querySelector<HTMLElement>('.screen');
    if (!host) return;
    this.#dispose();
    const term = new Terminal({ convertEol: true, fontSize: 13, fontFamily: 'ui-monospace, Menlo, monospace', scrollback: 5000, theme: { background: SCREEN_BACKGROUND } });
    const fit = new FitAddon();
    term.loadAddon(fit);
    this.#removeStyles = openStyled(term, host);
    fit.fit();
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const url = `${protocol}//${location.host}/api/v1/servers/${this.serverId}/terminal?ticket=${encodeURIComponent(ticket)}&cols=${term.cols}&rows=${term.rows}&term=xterm-256color`;
    const socket = new WebSocket(url);
    socket.binaryType = 'arraybuffer';
    socket.onopen = () => {
      this._state = 'open';
      term.focus();
    };
    socket.onmessage = (event) => term.write(new Uint8Array(event.data as ArrayBuffer));
    socket.onerror = () => {
      if (this.#socket !== socket) return;
      this._error = t('servers.terminal.connectionFailed');
      this._state = 'error';
    };
    socket.onclose = () => {
      if (this.#socket !== socket) return;
      if (this._state !== 'error') this._state = 'closed';
      term.writeln(`\r\n\x1b[2m${t('servers.terminal.sessionEnded')}\x1b[0m`);
    };
    term.onData((data) => socket.readyState === WebSocket.OPEN && socket.send(data));
    // Größenänderungen gehen als Text, damit sie nicht als Eingabe im Terminal landen.
    this.#observer = new ResizeObserver(() => {
      fit.fit();
      if (socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
    });
    this.#observer.observe(host);
    this.#term = term;
    this.#socket = socket;
  }

  /** Räumt die Sitzung weg, auch eine, die der Server schon beendet hat. */
  #dispose(): void {
    this.#observer?.disconnect();
    const socket = this.#socket;
    this.#socket = undefined;
    socket?.close();
    this.#term?.dispose();
    this.#removeStyles?.();
    this.#observer = this.#term = this.#removeStyles = undefined;
  }

  #stop(): void {
    this.#dispose();
    if (this._state !== 'error') this._state = 'idle';
  }

  #close(): void {
    this.#stop();
    this.dispatchEvent(new CustomEvent('lcm-close', { bubbles: true, composed: true }));
  }

  override render() {
    const state = this._state;
    return html`<dialog aria-labelledby="title" @cancel=${(e: Event) => (e.preventDefault(), this.#close())}>
      <header>
        <h2 id="title">${t('servers.terminal.title', { name: this.serverName })}</h2>
        <button class="close" type="button" aria-label=${t('servers.confirm.cancel')} @click=${() => this.#close()}>${icon('x')}</button>
      </header>
      <lcm-notice tone="warn">${t('servers.terminal.notice')}</lcm-notice>
      ${this._error ? html`<lcm-notice tone="crit">${this._error}</lcm-notice>` : nothing}
      <div class="screen" aria-label=${t('servers.terminal.screen')}></div>
      <footer>
        <span></span>
        ${state === 'open'
          ? html`<lcm-button variant="danger" @click=${() => this.#stop()}>${t('servers.terminal.end')}</lcm-button>`
          : html`<lcm-button variant="primary" icon="terminal" ?busy=${state === 'connecting'} @click=${() => void this.#connect()}>${t(state === 'connecting' ? 'servers.terminal.connecting' : 'servers.terminal.connect')}</lcm-button>`}
      </footer>
    </dialog>`;
  }
}
customElements.define('lcm-srv-terminal', SrvTerminal);
