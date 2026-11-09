import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import { placeFloating } from '../../components/floating';
import { icon } from '../../components/icons';
import { baseStyles, emit } from '../../components/shared';
import type { IpAllowlist } from '../../api/groups';
import { I18nController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { LIMITS, digitsOnly } from '../../lib/validate';
import { FIREWALL_PROTOCOLS, IP_VERSIONS, emptyFirewallRow, firewallErrors, splitSources, type FirewallProto, type FirewallRow, type FirewallRowErrors, type IpVersion } from './rule-model';

/** Die feste SSH-Freigabe eines Servers: Port steht fest, nur die Quellen lassen sich einschränken. */
export interface SshSources {
  port: number;
  allowlist_ids: number[];
  source_ips: string;
}

type Open = number | 'ssh' | null;

const protoOf = (value: string): FirewallProto => FIREWALL_PROTOCOLS.find((entry) => entry.value === value)?.value ?? 'tcp';
const versionOf = (value: string): IpVersion => IP_VERSIONS.find((entry) => entry.value === value)?.value ?? 'any';

/**
 * <lcm-groups-firewall-editor .rows=${…} .allowlists=${…} .ssh=${…} ?show-errors> – Firewall-Freigaben als Tabelle:
 * Port, Protokoll, IP-Version, erlaubte Quellen und Bemerkung je Zeile. Die Quellen (Allowlists zum Anhaken, darunter
 * eigene IPs/Netze) öffnen sich wie eine Auswahl direkt unter dem Feld. Mit `ssh` steht die SSH-Freigabe als feste erste Zeile darüber.
 * Meldet `lcm-change` mit { rows } und `lcm-ssh-change` mit { ssh }.
 */
export class LcmGroupsFirewallEditor extends LitElement {
  static override properties = {
    rows: { attribute: false },
    allowlists: { attribute: false },
    ssh: { attribute: false },
    sshLabel: { type: String, attribute: 'ssh-label' },
    showErrors: { type: Boolean, attribute: 'show-errors' },
    _open: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .frame {
        overflow-x: auto;
        border: 1px solid var(--line-muted);
        border-radius: var(--radius-m);
      }
      table {
        inline-size: 100%;
        border-collapse: collapse;
      }
      th,
      td {
        padding: 0.4rem 0.5rem;
        border-block-end: 1px solid var(--line-muted);
        text-align: start;
        vertical-align: middle;
      }
      th {
        background: var(--surface-2);
        color: var(--text-2);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
        white-space: nowrap;
      }
      tbody tr:last-child td {
        border-block-end: 0;
      }
      tr.fixed td {
        background: var(--surface-2);
        color: var(--text-2);
      }
      .port {
        inline-size: 6rem;
      }
      .proto {
        inline-size: 7.5rem;
      }
      .ver {
        inline-size: 7rem;
      }
      .src {
        inline-size: 14rem;
      }
      .act {
        inline-size: 2.5rem;
        text-align: end;
      }
      input,
      select {
        inline-size: 100%;
        min-block-size: 2rem;
        padding: 0 0.5rem;
        border: 1px solid var(--line);
        border-radius: var(--radius-s);
        background: var(--surface-1);
        color: var(--text-1);
        font: inherit;
        font-size: var(--fs-s);
      }
      input:hover,
      select:hover {
        border-color: var(--line-strong);
      }
      input:focus-visible,
      select:focus-visible {
        outline: 2px solid var(--focus-ring);
        outline-offset: -1px;
      }
      input[aria-invalid='true'] {
        border-color: var(--crit);
      }
      .mono {
        font-family: var(--font-mono);
      }
      .static {
        display: block;
        padding-inline: 0.5rem;
        font-size: var(--fs-s);
      }
      .sources {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-2);
        inline-size: 100%;
        min-block-size: 2rem;
        padding: 0 0.5rem;
        border: 1px solid var(--line);
        border-radius: var(--radius-s);
        background: var(--surface-1);
        color: var(--text-1);
        font: inherit;
        font-size: var(--fs-s);
        text-align: start;
        cursor: pointer;
      }
      .sources:hover,
      .sources[aria-expanded='true'] {
        border-color: var(--accent);
      }
      .sources.invalid {
        border-color: var(--crit);
      }
      .sources span {
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      .sources svg {
        flex: none;
        inline-size: 0.9rem;
        block-size: 0.9rem;
        opacity: 0.7;
      }
      .pop {
        inline-size: 20rem;
        margin: 0;
        padding: var(--space-1);
        border: 1px solid var(--line);
        border-radius: var(--radius-m);
        background: var(--surface-overlay);
        color: var(--text-1);
        box-shadow: var(--elev-2);
      }
      .pop .label {
        display: block;
        padding: var(--space-2) var(--space-2) var(--space-1);
        color: var(--text-3);
        font-size: var(--fs-xs);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      .option {
        display: flex;
        align-items: center;
        gap: var(--space-2);
        padding: 0.35rem var(--space-2);
        border-radius: var(--radius-s);
        font-size: var(--fs-s);
        cursor: pointer;
      }
      .option:hover {
        background: var(--surface-2);
      }
      .option input {
        inline-size: auto;
        min-block-size: 0;
        margin: 0;
        accent-color: var(--accent);
      }
      .ips {
        display: grid;
        gap: var(--space-1);
        padding: var(--space-2);
        border-block-start: 1px solid var(--line-muted);
        margin-block-start: var(--space-1);
      }
      .empty {
        padding: var(--space-2);
        color: var(--text-3);
        font-size: var(--fs-s);
      }
      .hint {
        margin: 0;
        color: var(--text-3);
        font-size: var(--fs-xs);
      }
      .error {
        margin: 0;
        color: var(--crit-text);
        font-size: var(--fs-xs);
      }
      td .error {
        margin-block-start: 2px;
      }
      .add {
        margin-block-start: var(--space-2);
      }
    `,
  ];

  declare rows: FirewallRow[];
  declare allowlists: IpAllowlist[];
  declare ssh: SshSources | null;
  declare sshLabel: string;
  declare showErrors: boolean;
  declare _open: Open;

  constructor() {
    super();
    this.rows = [];
    this.allowlists = [];
    this.ssh = null;
    this.sshLabel = '';
    this.showErrors = false;
    this._open = null;
    new I18nController(this);
  }

  #change(rows: FirewallRow[]): void {
    this.rows = rows;
    emit(this, 'lcm-change', { rows });
  }

  #patch(index: number, patch: Partial<FirewallRow>): void {
    this.#change(this.rows.map((row, i) => (i === index ? { ...row, ...patch } : row)));
  }

  #patchSsh(patch: Partial<SshSources>): void {
    if (!this.ssh) return;
    this.ssh = { ...this.ssh, ...patch };
    emit(this, 'lcm-ssh-change', { ssh: this.ssh });
  }

  #remove(index: number): void {
    if (this._open === index) this._open = null;
    else if (typeof this._open === 'number' && this._open > index) this._open -= 1;
    this.#change(this.rows.filter((_, i) => i !== index));
  }

  #toggle(target: Open): void {
    this._open = this._open === target ? null : target;
  }

  /** „alle Quellen“ oder die gewählten Allowlists und Adressen in einer Zeile. */
  #summary(allowlistIds: number[], sourceIps: string): string {
    const names = this.allowlists.filter((list) => allowlistIds.includes(list.id)).map((list) => list.name);
    const parts = [...names, ...splitSources(sourceIps)];
    return parts.length === 0 ? t('groups.firewall.allSources') : parts.join(', ');
  }

  #sourcesButton(target: Open, allowlistIds: number[], sourceIps: string, invalid: boolean): TemplateResult {
    const open = this._open === target;
    return html`<button
      class=${invalid ? 'sources invalid' : 'sources'}
      type="button"
      data-src=${String(target)}
      aria-haspopup="dialog"
      aria-expanded=${String(open)}
      title=${t('groups.firewall.sources')}
      @click=${() => this.#toggle(target)}
    >
      <span>${this.#summary(allowlistIds, sourceIps)}</span>${icon(open ? 'chevron-up' : 'chevron-down')}
    </button>`;
  }

  /** Inhalt der Auswahl: Allowlists zum Anhaken, darunter eigene IPs oder Netze. */
  #sourcesPanel(allowlistIds: number[], sourceIps: string, problem: string, onToggle: (id: number) => void, onIps: (value: string) => void): TemplateResult {
    return html`<span class="label">${t('groups.firewall.allowlists')}</span>
      ${this.allowlists.length === 0
        ? html`<div class="empty">${t('groups.firewall.noAllowlists')}</div>`
        : this.allowlists.map(
            (list) => html`<label class="option"><input type="checkbox" .checked=${allowlistIds.includes(list.id)} @change=${() => onToggle(list.id)} />${list.name}</label>`,
          )}
      <div class="ips">
        <label class="label" for="ips">${t('groups.firewall.sourceIps')}</label>
        <input
          id="ips"
          class="mono"
          placeholder="203.0.113.7, 10.0.0.0/24"
          aria-invalid=${String(problem !== '')}
          .value=${sourceIps}
          @input=${(e: Event) => onIps((e.target as HTMLInputElement).value)}
        />
        ${problem ? html`<p class="error">${problem}</p>` : html`<p class="hint">${t('groups.firewall.sourcesHint')}</p>`}
      </div>`;
  }

  /** Meldung zu den eigenen Quell-Adressen; leer, wenn alles passt. */
  #sourcesProblem(errors: FirewallRowErrors | undefined, version: IpVersion): string {
    if (!errors) return '';
    const list = (entries: string[]) => (entries.length > 3 ? `${entries.slice(0, 3).join(', ')} …` : entries.join(', '));
    if (errors.badSources.length > 0) return t('groups.firewall.sourcesBad', { list: list(errors.badSources) });
    if (errors.wrongFamily.length > 0) {
      const label = IP_VERSIONS.find((entry) => entry.value === version)?.label ?? '';
      return t('groups.firewall.sourcesFamily', { list: list(errors.wrongFamily), version: label });
    }
    return errors.tooManySources ? t('groups.firewall.sourcesTooMany', { limit: LIMITS.firewallSources }) : '';
  }

  #sshErrors(ssh: SshSources): FirewallRowErrors | undefined {
    return firewallErrors([{ ...emptyFirewallRow(), port: String(ssh.port), source_ips: ssh.source_ips }])[0];
  }

  /** Die offene Auswahl: ein Popover unter dem Quellen-Feld der Zeile. */
  #renderPopover(): TemplateResult | typeof nothing {
    const target = this._open;
    if (target === null) return nothing;
    const errors = firewallErrors(this.rows);
    let body: TemplateResult | typeof nothing = nothing;
    if (target === 'ssh' && this.ssh) {
      const ssh = this.ssh;
      const problem = this.#sourcesProblem(this.#sshErrors(ssh), 'any');
      body = this.#sourcesPanel(ssh.allowlist_ids, ssh.source_ips, problem, (id) => this.#patchSsh({ allowlist_ids: this.#toggleIn(ssh.allowlist_ids, id) }), (value) => this.#patchSsh({ source_ips: value }));
    } else if (typeof target === 'number' && this.rows[target]) {
      const row = this.rows[target];
      const problem = this.#sourcesProblem(errors[target], row.ip_version);
      body = this.#sourcesPanel(row.allowlist_ids, row.source_ips, problem, (id) => this.#patch(target, { allowlist_ids: this.#toggleIn(row.allowlist_ids, id) }), (value) => this.#patch(target, { source_ips: value }));
    }
    return html`<div class="pop" popover="auto" role="dialog" aria-label=${t('groups.firewall.sources')} @toggle=${(e: ToggleEvent) => e.newState === 'closed' && (this._open = null)}>${body}</div>`;
  }

  override updated(): void {
    const pop = this.renderRoot.querySelector<HTMLElement>('.pop');
    if (!pop || this._open === null || pop.matches(':popover-open')) return;
    const anchor = this.renderRoot.querySelector<HTMLElement>(`[data-src="${String(this._open)}"]`);
    if (!anchor) return;
    pop.showPopover();
    placeFloating(anchor, pop, { align: 'start', gap: 4 });
  }

  #toggleIn(ids: number[], id: number): number[] {
    return ids.includes(id) ? ids.filter((entry) => entry !== id) : [...ids, id];
  }

  #renderSsh(ssh: SshSources): TemplateResult {
    const invalid = this.#sshErrors(ssh)?.sources === true;
    return html`<tr class="fixed">
        <td><span class="static mono">${ssh.port}</span></td>
        <td><span class="static">${FIREWALL_PROTOCOLS[0]?.label}</span></td>
        <td><span class="static">${IP_VERSIONS[0]?.label}</span></td>
        <td>${this.#sourcesButton('ssh', ssh.allowlist_ids, ssh.source_ips, invalid)}</td>
        <td><span class="static">${this.sshLabel || t('groups.firewall.sshAccess')}</span></td>
        <td class="act"><span class="static" title=${t('groups.firewall.sshFixed')}>${icon('lock')}</span></td>
      </tr>
`;
  }

  #renderRow(row: FirewallRow, index: number, errors: FirewallRowErrors | undefined): TemplateResult {
    const badPort = !!errors?.port && (this.showErrors || row.port !== '');
    const badSources = !!errors?.sources;
    const badComment = !!errors?.comment;
    return html`<tr>
        <td>
          <input
            class="mono"
            inputmode="numeric"
            maxlength="5"
            placeholder="443"
            aria-label=${t('groups.firewall.port')}
            aria-invalid=${String(badPort)}
            .value=${row.port}
            @input=${(e: Event) => {
              const input = e.target as HTMLInputElement;
              input.value = digitsOnly(input.value);
              this.#patch(index, { port: input.value });
            }}
          />
          ${badPort ? html`<p class="error">${t('groups.firewall.portInvalid')}</p>` : nothing}
        </td>
        <td>
          <select aria-label=${t('groups.firewall.protocol')} @change=${(e: Event) => this.#patch(index, { proto: protoOf((e.target as HTMLSelectElement).value) })}>
            ${FIREWALL_PROTOCOLS.map((p) => html`<option value=${p.value} ?selected=${row.proto === p.value}>${p.label}</option>`)}
          </select>
        </td>
        <td>
          <select aria-label=${t('groups.firewall.ipVersion')} @change=${(e: Event) => this.#patch(index, { ip_version: versionOf((e.target as HTMLSelectElement).value) })}>
            ${IP_VERSIONS.map((v) => html`<option value=${v.value} ?selected=${row.ip_version === v.value}>${v.label}</option>`)}
          </select>
        </td>
        <td>${this.#sourcesButton(index, row.allowlist_ids, row.source_ips, badSources)}</td>
        <td>
          <input
            aria-label=${t('groups.firewall.comment')}
            aria-invalid=${String(badComment)}
            maxlength=${LIMITS.firewallComment}
            placeholder=${t('groups.firewall.commentPlaceholder')}
            .value=${row.comment}
            @input=${(e: Event) => this.#patch(index, { comment: (e.target as HTMLInputElement).value })}
          />
          ${badComment ? html`<p class="error">${t('groups.firewall.commentTooLong', { limit: LIMITS.firewallComment })}</p>` : nothing}
        </td>
        <td class="act">
          <lcm-button variant="ghost" size="small" icon="trash" icon-only label=${t('groups.firewall.remove', { number: index + 1 })} @click=${() => this.#remove(index)}></lcm-button>
        </td>
      </tr>
`;
  }

  override render() {
    const errors = firewallErrors(this.rows);
    return html`<div class="frame">
        <table>
          <thead>
            <tr>
              <th class="port">${t('groups.firewall.port')}</th>
              <th class="proto">${t('groups.firewall.protocol')}</th>
              <th class="ver">${t('groups.firewall.ipVersion')}</th>
              <th class="src">${t('groups.firewall.sources')}</th>
              <th>${t('groups.firewall.comment')}</th>
              <th class="act"><span class="sr-only">${t('groups.firewall.actions')}</span></th>
            </tr>
          </thead>
          <tbody>
            ${this.ssh ? this.#renderSsh(this.ssh) : nothing} ${this.rows.map((row, index) => this.#renderRow(row, index, errors[index]))}
          </tbody>
        </table>
      </div>
      ${this.#renderPopover()}
      <lcm-button class="add" size="small" icon="plus" @click=${() => this.#change([...this.rows, emptyFirewallRow()])}>${t('groups.firewall.add')}</lcm-button>`;
  }
}
customElements.define('lcm-groups-firewall-editor', LcmGroupsFirewallEditor);
