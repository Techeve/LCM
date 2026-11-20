import { css, html, LitElement, nothing } from 'lit';
import '../../components/lcm-badge';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-dialog';
import '../../components/lcm-empty-state';
import '../../components/lcm-notice';
import { baseStyles } from '../../components/shared';
import { refreshServer, type DsmCheck, type DsmFirewallRule, type DsmFirewallSection, type ServerItem } from '../../api/servers';
import {
  applyHints,
  dsmApi,
  firewallPolicy,
  firewallPorts,
  firewallProtocol,
  firewallSource,
  isSettable,
  settableDeviations,
  targetKey,
  type FirewallPolicy,
} from '../../api/servers-dsm';
import { canPortScan } from '../../api/servers-portscan';
import { errorText, t } from '../../lib/i18n';
import { announceJob } from '../../lib/job-toast';
import { OwnJobs } from '../../lib/job-watch';
import './sec-portscan';
import { tabStyles } from './srv-styles';

// Reihenfolge wie in der DSM-Systemsteuerung: Firewall, Sicherheit, Schutz, Erweitert.
const CHECK_ORDER = ['firewall', 'skipIpCheck', 'csrf', 'csp', 'iframe', 'sessionClear', 'autoBlock', 'dos', 'tlsProfile', 'spectre'];

const TONE: Record<DsmCheck['state'], string> = { ok: 'ok', deviation: 'warn', unknown: 'neutral' };

const STATE_LABEL: Record<DsmCheck['state'], string> = {
  ok: 'serverSecurity.device.state.ok',
  deviation: 'serverSecurity.device.state.deviation',
  unknown: 'serverSecurity.device.state.unknown',
};

const CHECK_LABEL: Record<string, string> = {
  firewall: 'serverSecurity.device.check.firewall',
  skipIpCheck: 'serverSecurity.device.check.skipIpCheck',
  csrf: 'serverSecurity.device.check.csrf',
  csp: 'serverSecurity.device.check.csp',
  iframe: 'serverSecurity.device.check.iframe',
  sessionClear: 'serverSecurity.device.check.sessionClear',
  autoBlock: 'serverSecurity.device.check.autoBlock',
  dos: 'serverSecurity.device.check.dos',
  tlsProfile: 'serverSecurity.device.check.tlsProfile',
  spectre: 'serverSecurity.device.check.spectre',
};

const RULE_ACTION: Record<FirewallPolicy, string> = {
  allow: 'serverSecurity.device.firewall.action.allow',
  deny: 'serverSecurity.device.firewall.action.deny',
  none: 'serverSecurity.device.firewall.default.none',
};

const SECTION_DEFAULT: Record<FirewallPolicy, string> = {
  allow: 'serverSecurity.device.firewall.default.allow',
  deny: 'serverSecurity.device.firewall.default.deny',
  none: 'serverSecurity.device.firewall.default.none',
};

/** Prüfungen in der Reihenfolge der DSM-Systemsteuerung, unbekannte Schlüssel am Ende. */
export function orderedChecks(checks: DsmCheck[]): DsmCheck[] {
  const rank = (key: string) => {
    const i = CHECK_ORDER.indexOf(key);
    return i < 0 ? CHECK_ORDER.length : i;
  };
  return [...checks].sort((a, b) => rank(a.key) - rank(b.key));
}

function checkLabel(key: string): string {
  return CHECK_LABEL[key] ? t(CHECK_LABEL[key]) : key;
}

/**
 * <lcm-srv-device-security .server=${…} ?can-write> – Sicherheit für Geräte, die LCM über ihre API anspricht (DSM,
 * RouterOS). Der Reiter für Linux liest Werkzeuge, CVE-Scan und Härtung über SSH; hier gibt es die Prüfungen von DSM
 * und die Portprüfung von LCM aus. Abweichungen setzt LCM auf Klick und nach Bestätigung auf den Sollwert, als Job.
 */
export class SrvDeviceSecurity extends LitElement {
  static override properties = {
    server: { attribute: false },
    canWrite: { type: Boolean, attribute: 'can-write' },
    _keys: { state: true },
    _busy: { state: true },
    _error: { state: true },
  };
  static override styles = [
    baseStyles,
    tabStyles,
    css`
      ul {
        list-style: none;
        margin: 0;
        padding: 0;
        display: grid;
        gap: var(--space-2);
      }
      li {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-1) var(--space-3);
        align-items: baseline;
      }
      .value {
        color: var(--text-muted);
      }
      .note {
        flex-basis: 100%;
        color: var(--text-muted);
        font-size: var(--fs-s);
      }
      .changes {
        margin-block-start: var(--space-4);
      }
      lcm-notice {
        display: block;
        margin-block-start: var(--space-3);
      }
      .section + .section {
        margin-block-start: var(--space-5);
      }
      h3 {
        margin: 0 0 var(--space-2);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
      }
      .scroll {
        overflow-x: auto;
      }
      table {
        border-collapse: collapse;
        font-size: var(--fs-s);
      }
      th {
        padding: 0 var(--space-5) var(--space-2) 0;
        color: var(--text-2);
        font-weight: var(--fw-medium);
        text-align: start;
      }
      td {
        padding: var(--space-2) var(--space-5) var(--space-2) 0;
        border-block-start: 1px solid var(--line);
        vertical-align: baseline;
      }
      tr.off td {
        color: var(--text-2);
      }
      tr.off lcm-badge {
        margin-inline-start: var(--space-2);
      }
      .fallback {
        margin: var(--space-2) 0 0;
        font-size: var(--fs-s);
      }
    `,
  ];

  declare server: ServerItem | undefined;
  declare canWrite: boolean;
  /** Die Prüfungen im Bestätigungsdialog; leer = Dialog zu. */
  declare _keys: string[];
  declare _busy: boolean;
  declare _error: string;

  readonly #jobs = new OwnJobs(this, () => {
    if (this.server) refreshServer(this.server.id);
  });

  constructor() {
    super();
    this.server = undefined;
    this.canWrite = false;
    this._keys = [];
    this._busy = false;
    this._error = '';
  }

  #open(keys: string[]): void {
    this._error = '';
    this._keys = keys;
  }

  async #apply(server: ServerItem): Promise<void> {
    this._busy = true;
    this._error = '';
    try {
      const job = await dsmApi.apply(server.id, this._keys);
      this._keys = [];
      announceJob(t('serverSecurity.device.apply.jobLabel'), server.name, job.job_id);
      refreshServer(server.id);
      this.#jobs.watch(job.job_id);
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  #rowAction(check: DsmCheck) {
    if (check.state !== 'deviation') return nothing;
    if (check.key === 'firewall') return html`<span class="note">${t('serverSecurity.device.firewallNote')}</span>`;
    if (!this.canWrite || !isSettable(check.key)) return nothing;
    return html`<lcm-button size="small" variant="ghost" ?disabled=${this.#jobs.running} @click=${() => this.#open([check.key])}>${t('serverSecurity.device.apply.one')}</lcm-button>`;
  }

  #renderFixAll(checks: DsmCheck[]) {
    const keys = settableDeviations(checks).map((check) => check.key);
    if (!this.canWrite || keys.length === 0) return nothing;
    return html`<lcm-button slot="actions" size="small" ?disabled=${this.#jobs.running} @click=${() => this.#open(keys)}>${t('serverSecurity.device.apply.all')}</lcm-button>`;
  }

  #renderChecks(server: ServerItem) {
    if (server.os_id !== 'dsm') return nothing;
    const checks = orderedChecks(server.dsm_checks);
    return html`<lcm-card heading=${t('serverSecurity.device.title')}>
      ${this.#renderFixAll(checks)}
      ${checks.length === 0
        ? html`<p class="muted">${t('serverSecurity.device.notRead')}</p>`
        : html`<ul>
            ${checks.map(
              (check) => html`<li>
                <lcm-badge tone=${TONE[check.state]}>${t(STATE_LABEL[check.state])}</lcm-badge>
                <span>${checkLabel(check.key)}</span>
                ${check.value ? html`<span class="value">${check.value}</span>` : nothing} ${this.#rowAction(check)}
              </li>`,
            )}
          </ul>`}
    </lcm-card>`;
  }

  #renderRule(rule: DsmFirewallRule) {
    const all = t('serverSecurity.device.firewall.all');
    return html`<tr class=${rule.enabled ? '' : 'off'}>
      <td>
        ${t(RULE_ACTION[firewallPolicy(rule.policy)])}
        ${rule.enabled ? nothing : html`<lcm-badge tone="neutral">${t('serverSecurity.device.firewall.off')}</lcm-badge>`}
      </td>
      <td>${firewallProtocol(rule) || all}</td>
      <td class="mono">${firewallPorts(rule) || all}</td>
      <td class="mono">${firewallSource(rule) || all}</td>
    </tr>`;
  }

  #renderSection(section: DsmFirewallSection) {
    const name = section.name === 'global' ? t('serverSecurity.device.firewall.allInterfaces') : section.name;
    const fallback = t(SECTION_DEFAULT[firewallPolicy(section.policy)]);
    return html`<div class="section">
      <h3>${name}</h3>
      ${section.rules.length === 0
        ? html`<p class="muted">${t('serverSecurity.device.firewall.noRules')}</p>`
        : html`<div class="scroll"><table>
            <thead><tr>
              <th scope="col">${t('serverSecurity.device.firewall.column.action')}</th>
              <th scope="col">${t('serverSecurity.device.firewall.column.protocol')}</th>
              <th scope="col">${t('serverSecurity.device.firewall.column.ports')}</th>
              <th scope="col">${t('serverSecurity.device.firewall.column.source')}</th>
            </tr></thead>
            <tbody>${section.rules.map((rule) => this.#renderRule(rule))}</tbody>
          </table></div>`}
      <p class="fallback">${t('serverSecurity.device.firewall.fallback', { policy: fallback })}</p>
    </div>`;
  }

  /** Die Regeln des aktiven Profils, nur zur Ansicht. Gepflegt werden sie in DSM. */
  #renderFirewall(server: ServerItem) {
    const firewall = server.dsm_firewall;
    if (server.os_id !== 'dsm' || !firewall) return nothing;
    return html`<lcm-card
      heading=${t('serverSecurity.device.firewall.heading', { profile: firewall.profile })}
      subtitle=${t('serverSecurity.device.firewall.readOnly')}
    >
      ${firewall.sections.length === 0
        ? html`<p class="muted">${t('serverSecurity.device.firewall.noRules')}</p>`
        : firewall.sections.map((section) => this.#renderSection(section))}
    </lcm-card>`;
  }

  /** Bestätigung: nennt genau, was sich ändert, und die Folgen, die man vorher kennen sollte. */
  #renderDialog(server: ServerItem) {
    const chosen = orderedChecks(server.dsm_checks.filter((check) => this._keys.includes(check.key)));
    return html`<lcm-dialog
      ?open=${this._keys.length > 0}
      heading=${t('serverSecurity.device.apply.heading', { name: server.name })}
      message=${t('serverSecurity.device.apply.message')}
      confirm-label=${t('serverSecurity.device.apply.confirm')}
      cancel-label=${t('servers.confirm.cancel')}
      error=${this._error}
      ?busy=${this._busy}
      @lcm-cancel=${() => (this._keys = [])}
      @lcm-confirm=${() => void this.#apply(server)}
    >
      <ul class="changes">
        ${chosen.map(
          (check) => html`<li>
            <span>${checkLabel(check.key)}</span>
            <span class="value">${t('serverSecurity.device.apply.change', { current: check.value, target: t(targetKey(check.key)) })}</span>
          </li>`,
        )}
      </ul>
      ${applyHints(this._keys).map((key) => html`<lcm-notice tone="warn">${t(key)}</lcm-notice>`)}
    </lcm-dialog>`;
  }

  override render() {
    const server = this.server;
    if (!server) return null;
    return html`<div class="stack">
      ${this.#renderChecks(server)} ${this.#renderFirewall(server)}
      ${canPortScan(server) ? html`<lcm-srv-portscan server-id=${server.id} server-name=${server.name} ?can-write=${this.canWrite}></lcm-srv-portscan>` : nothing}
    </div>
    ${server.os_id === 'dsm' ? this.#renderDialog(server) : nothing}`;
  }
}
customElements.define('lcm-srv-device-security', SrvDeviceSecurity);
