import { LitElement, css, html, nothing } from 'lit';
import '../../components/lcm-badge';
import { icon } from '../../components/icons';
import { baseStyles } from '../../components/shared';
import type { SshCommand, SshSession } from '../../api/jobs';
import { I18nController } from '../../lib/controllers';
import { formatDateTime } from '../../lib/format';
import { t } from '../../lib/i18n';
import { logStyles } from './jobs-styles';

/** <lcm-jobs-ssh .sessions=${…}> – die SSH-Sitzungen eines Jobs, jede aufklappbar mit ihren Kommandos. */
export class JobsSsh extends LitElement {
  static override properties = { sessions: { attribute: false }, active: { type: Boolean }, _open: { state: true } };
  static override styles = [
    baseStyles,
    logStyles,
    css`
      :host {
        display: block;
      }
      ul {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      li {
        border-radius: var(--radius-m);
        background: var(--surface-1);
        box-shadow: var(--ring-card);
      }
      .head {
        display: grid;
        grid-template-columns: minmax(0, 1fr) auto;
        gap: var(--space-1) var(--space-3);
        align-items: center;
        inline-size: 100%;
        padding: var(--space-3);
        border: 0;
        border-radius: inherit;
        background: transparent;
        text-align: start;
        cursor: pointer;
      }
      .head:hover {
        background: var(--surface-2);
      }
      .purpose {
        font-weight: var(--fw-semibold);
        overflow-wrap: anywhere;
      }
      .sub {
        grid-column: 1;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .count {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        grid-row: 1 / span 2;
        grid-column: 2;
        color: var(--text-2);
        font-size: var(--fs-s);
        white-space: nowrap;
      }
      .head[aria-expanded='true'] .chev {
        transform: rotate(180deg);
      }
      .commands {
        display: flex;
        flex-direction: column;
        gap: var(--space-3);
        padding: 0 var(--space-3) var(--space-3);
      }
      .cmd-head {
        display: flex;
        align-items: baseline;
        justify-content: space-between;
        gap: var(--space-3);
        margin-block-end: var(--space-1);
      }
      code {
        color: var(--accent-text);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        overflow-wrap: anywhere;
      }
      .cmd-meta {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        color: var(--text-2);
        font-size: var(--fs-xs);
        white-space: nowrap;
      }
      .transport {
        margin: var(--space-1) 0 0;
        color: var(--crit-text);
        font-size: var(--fs-s);
      }
      .none {
        margin: 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare sessions: SshSession[];
  /** Der Job läuft noch. Nur dann gilt `running`: Nach einem Absturz von LCM bliebe es sonst ewig stehen. */
  declare active: boolean;
  declare _open: string;

  constructor() {
    super();
    this.sessions = [];
    this.active = false;
    this._open = '';
    new I18nController(this);
  }

  #renderCommand(command: SshCommand) {
    return html`<div>
      <div class="cmd-head">
        <code>$ ${command.command}</code>
        <span class="cmd-meta">
          ${command.running && this.active
            ? html`<lcm-badge tone="accent">${t('jobs.ssh.running')}</lcm-badge>`
            : html`<lcm-badge tone=${command.exit_code === 0 ? 'ok' : 'crit'}>${t('jobs.ssh.exit', { code: command.exit_code })}</lcm-badge>
                ${t('jobs.ssh.duration', { ms: command.duration_ms })}`}
        </span>
      </div>
      ${command.output ? html`<pre>${command.output}</pre>` : nothing}
      ${command.err ? html`<p class="transport">${t('jobs.ssh.transportError', { error: command.err })}</p>` : nothing}
    </div>`;
  }

  #renderSession(session: SshSession) {
    const open = this._open === session.id;
    const commands = session.commands ?? [];
    return html`<li>
      <button class="head" type="button" aria-expanded=${String(open)} @click=${() => (this._open = open ? '' : session.id)}>
        <span class="purpose">${session.purpose}${session.had_error ? html` <lcm-badge tone="crit">${t('jobs.ssh.error')}</lcm-badge>` : nothing}</span>
        <span class="count">${t('jobs.ssh.commands', { count: session.command_count })}<span class="chev">${icon('chevron-down')}</span></span>
        <span class="sub">${formatDateTime(session.opened_at)}${session.user && session.host ? ` · ${session.user}@${session.host}` : ''}</span>
      </button>
      ${open
        ? html`<div class="commands">${commands.length > 0 ? commands.map((c) => this.#renderCommand(c)) : html`<p class="none">${t('jobs.ssh.noCommands')}</p>`}</div>`
        : nothing}
    </li>`;
  }

  override render() {
    if (this.sessions.length === 0) return html`<p class="none">${t('jobs.ssh.none')}</p>`;
    return html`<ul>${this.sessions.map((session) => this.#renderSession(session))}</ul>`;
  }
}
customElements.define('lcm-jobs-ssh', JobsSsh);
