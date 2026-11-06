import { css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-ampel';
import '../../components/lcm-button';
import '../../components/lcm-card';
import { baseStyles } from '../../components/shared';
import { capabilitiesUrl, capabilityLevel, normaliseCapabilities, serversApi, type Capability } from '../../api/servers';
import { normaliseStatusDetail, serverStatusUrl, type ServerStatusDetail } from '../../api/server-detail';
import { jobOutputUrl, normaliseOutput } from '../../api/jobs';
import { api } from '../../lib/api';
import { auth } from '../../lib/auth';
import { formatRelative } from '../../lib/format';
import { errorText, t } from '../../lib/i18n';
import { causeText, durationSince } from '../../lib/insight-text';
import { openJobOnClick } from '../../lib/job-window';
import { LiveData } from '../../lib/live-data';
import './ov-facts';
import './ov-kernels';
import './ov-lcm-host';
import './srv-action';
import './srv-groups';
import { SrvTab } from './srv-tab';
import { tabStyles } from './srv-styles';

const LEVEL_STATE = { ok: 'servers.capability.state.ok', warn: 'servers.capability.state.warn', crit: 'servers.capability.state.crit' } as const;
const WARNING_CLASSES = new Set(['repo_error', 'apt_warning']);
const KNOWN_CLASSES = new Set(['lock_busy', 'dpkg_interrupted', 'broken_deps', 'disk_full', 'repo_error', 'network', 'timeout', 'scanner_unavailable']);
/**
 * Übersicht in der Gliederung der bisherigen Oberfläche: beim LCM-Host dessen Einrichtung, Betriebsfähigkeit, Hardware
 * und System, installierte Kernel, Gruppen. Befunde stehen in der Blase an der Ampel im Kopf, Jobs im Reiter „Jobs“.
 */
export class SrvOverview extends SrvTab {
  static override properties = { ...SrvTab.properties, _outputs: { state: true } };
  static override styles = [
    baseStyles,
    tabStyles,
    css`
      :host {
        container-type: inline-size;
      }
      .page {
        display: flex;
        flex-direction: column;
        gap: var(--gap);
        max-inline-size: 100rem;
      }
      .cats {
        display: flex;
        flex-direction: column;
        margin: 0;
        padding: 0;
        list-style: none;
      }
      .cat {
        display: grid;
        grid-template-columns: auto minmax(0, 1fr);
        gap: var(--space-1) var(--space-3);
        padding-block: var(--space-1);
      }
      .cat:first-child {
        padding-block-start: 0;
      }
      .cat:last-child {
        padding-block-end: 0;
      }
      .cat lcm-ampel {
        margin-block-start: 0.2rem;
      }
      .cat-head {
        display: flex;
        flex-wrap: wrap;
        align-items: baseline;
        gap: var(--space-1) var(--space-3);
      }
      .cat-name {
        font-weight: var(--fw-semibold);
      }
      .cat-state {
        font-weight: var(--fw-medium);
      }
      .cat-meta {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      .cat-body {
        grid-column: 2;
      }
      .error {
        margin: var(--space-2) 0 0;
        padding: var(--space-3);
        border-radius: var(--radius-m);
        background: var(--crit-soft);
        color: var(--crit-text);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        line-height: 1.5;
        overflow-wrap: anywhere;
        white-space: pre-wrap;
      }
      .error.warn {
        background: var(--warn-soft);
        color: var(--warn-text);
      }
      .cause {
        margin: var(--space-2) 0 0;
        font-weight: var(--fw-medium);
      }
      .hint {
        margin: var(--space-2) 0 0;
        font-size: var(--fs-s);
      }
      .cat-body.actions {
        margin-block-start: var(--space-2);
      }
      .note {
        margin: var(--space-2) 0 0;
        font-size: var(--fs-s);
      }
      .note.warn {
        color: var(--warn-text);
      }
      .output {
        margin: var(--space-2) 0 0;
        padding: var(--space-3);
        max-block-size: 20rem;
        overflow: auto;
        border-radius: var(--radius-m);
        background: var(--surface-2);
        font-family: var(--font-mono);
        font-size: var(--fs-xs);
        line-height: 1.5;
        white-space: pre-wrap;
        overflow-wrap: anywhere;
      }
    `,
  ];

  /** Job-Ausgaben, die der Nutzer in der Karte aufgeklappt hat, je Kategorie (Text oder Fehlermeldung). */
  declare _outputs: Partial<Record<string, string>>;

  constructor() {
    super();
    this._outputs = {};
  }

  readonly #capabilities = new LiveData<Capability[]>(this, {
    url: () => capabilitiesUrl(this.server.id),
    topics: () => [`server:${this.server.id}`],
    transform: normaliseCapabilities,
  });
  readonly #status = new LiveData<ServerStatusDetail>(this, { url: () => serverStatusUrl(this.server.id), topics: () => [`server:${this.server.id}`], transform: normaliseStatusDetail });

  // ---- Betriebsfähigkeit ----

  #meta(capability: Capability): string {
    const parts: string[] = [];
    if (capability.failing_since) parts.push(t('servers.capability.since', { duration: durationSince(capability.failing_since) }));
    if (capability.consecutive_failures > 0) parts.push(t('servers.capability.failures', { count: capability.consecutive_failures }));
    if (capability.last_success_at) parts.push(t('servers.capability.lastSuccess', { when: formatRelative(capability.last_success_at) }));
    else if (capability.effective !== '') parts.push(t('servers.capability.neverSucceeded'));
    return parts.join(' · ');
  }

  /** Ursache, Hinweis und Fehlerauszug eines Fehlschlags; bei einer belegten Sperre sagt die Ursache schon alles. */
  #errorBody(capability: Capability, level: 'warn' | 'crit'): TemplateResult | typeof nothing {
    const cls = capability.last_error_class;
    const message = capability.last_error_class === 'lock_busy' ? '' : capability.last_error_message;
    if (capability.effective === '' || (!cls && !message)) return nothing;
    return html`<div class="cat-body">
      ${cls ? html`<p class="cause">${causeText(cls, capability.last_error_params)}</p>` : nothing}
      ${message ? html`<p class=${level === 'crit' ? 'error' : 'error warn'}>${message}</p>` : nothing}
      ${KNOWN_CLASSES.has(cls) ? html`<p class="hint"><strong>${t('servers.whatToDo')}</strong> ${t(`servers.capability.hint.${cls}`)}</p>` : nothing}
      ${this.#jobLinks(capability)}
    </div>`;
  }

  /** Teilproblem eines erfolgreichen Laufs (etwa eine kaputte Paketquelle unter vielen) und veralteter Paketstand. */
  #notes(capability: Capability): TemplateResult | typeof nothing {
    const { warning_class: cls, warning_message: message, stale } = capability;
    if (!cls && !stale) return nothing;
    const warning = cls ? [WARNING_CLASSES.has(cls) ? t(`servers.capability.warningClass.${cls}`) : '', message].filter(Boolean).join(': ') : '';
    const since = capability.last_success_at ? t('servers.capability.stale.since', { duration: durationSince(capability.last_success_at) }) : t('servers.capability.stale.never');
    return html`<div class="cat-body">
      ${warning ? html`<p class="note warn">${warning}</p>` : nothing} ${stale ? html`<p class="note warn">${since}</p>` : nothing}
    </div>`;
  }

  /** Verweis auf den Job des Fehlschlags und seine Ausgabe auf Abruf; beides verlangt `jobs:read`. */
  #jobLinks(capability: Capability): TemplateResult | typeof nothing {
    if (!capability.last_job_id || !auth.can('jobs:read')) return nothing;
    const output = this._outputs[capability.category];
    return html`<p class="hint">
        <a href=${`/jobs/${capability.last_job_id}`} @click=${openJobOnClick(capability.last_job_id)}>${t('servers.capability.job', { name: capability.last_job_name || capability.last_job_id })}</a>
        <lcm-button size="small" variant="ghost" @click=${() => void this.#toggleOutput(capability)}
          >${output === undefined ? t('servers.capability.output.show') : t('servers.capability.output.hide')}</lcm-button
        >
      </p>
      ${output === undefined ? nothing : html`<pre class="output">${output}</pre>`}`;
  }

  async #toggleOutput(capability: Capability): Promise<void> {
    const { category, last_job_id: jobId } = capability;
    if (this._outputs[category] !== undefined) {
      this._outputs = { ...this._outputs, [category]: undefined };
      return;
    }
    this._outputs = { ...this._outputs, [category]: t('jobs.detail.loading') };
    const text = await api
      .get<unknown>(jobOutputUrl(jobId))
      .then((data) => normaliseOutput(data).output || t('jobs.detail.noOutput'))
      .catch((error: unknown) => errorText(error));
    // Inzwischen wieder zugeklappt: nichts mehr eintragen.
    if (this._outputs[category] !== undefined) this._outputs = { ...this._outputs, [category]: text };
  }

  #repairLabel(kind: string): string {
    return t(`servers.repair.${kind}.label`);
  }

  #actions(capability: Capability): TemplateResult | typeof nothing {
    if (!this.canWrite || capability.effective === '') return nothing;
    const server = this.server;
    return html`<div class="cat-body actions">
      ${capability.repair
        ? html`<lcm-srv-action
            variant="primary"
            icon="repair"
            server-id=${server.id}
            server-name=${server.name}
            action="repair"
            .params=${{ kind: capability.repair }}
            label=${this.#repairLabel(capability.repair)}
            help=${t(`servers.repair.${capability.repair}.help`)}
            meaning=${t(`servers.repair.${capability.repair}.meaning`)}
            .run=${() => serversApi.repair(server.id, capability.repair)}
          ></lcm-srv-action>`
        : nothing}
      ${capability.retryable
        ? html`<lcm-srv-action
            icon="refresh"
            server-id=${server.id}
            server-name=${server.name}
            action="retry"
            .params=${{ category: capability.category }}
            label=${t('servers.capability.retry.label')}
            help=${t(`servers.capability.retry.help.${capability.category}`)}
            .run=${() => serversApi.retry(server.id, capability.category)}
          ></lcm-srv-action>`
        : nothing}
    </div>`;
  }

  #renderCapability(capability: Capability): TemplateResult {
    const level = capabilityLevel(capability);
    const state = t(LEVEL_STATE[level]);
    const meta = this.#meta(capability);
    return html`<li class="cat">
      <lcm-ampel level=${level} size="m" label=${state}></lcm-ampel>
      <div>
        <div class="cat-head">
          <span class="cat-name">${t(`servers.capability.${capability.category}`)}</span><span class="cat-state">${state}</span>
          ${meta ? html`<span class="cat-meta">${meta}</span>` : nothing}
        </div>
      </div>
      ${this.#errorBody(capability, level === 'ok' ? 'warn' : level)} ${this.#notes(capability)} ${this.#actions(capability)}
    </li>`;
  }

  /** Eine Zeile je Bereich, auch wenn alles läuft; bei einem Problem stehen Ursache, Abhilfe und Aktionen darunter. */
  #renderHealth(): TemplateResult | typeof nothing {
    const { data, error } = this.#capabilities;
    if (error && !data) return html`<lcm-card heading=${t('servers.capability.title')}><p class="error">${error}</p></lcm-card>`;
    if (!data || data.length === 0) return nothing;
    return html`<lcm-card heading=${t('servers.capability.title')} subtitle=${t('servers.capability.subtitle')}>
      <ul class="cats">
        ${data.map((capability) => this.#renderCapability(capability))}
      </ul>
    </lcm-card>`;
  }

  #renderKernels(): TemplateResult | typeof nothing {
    const kernel = this.#status.data?.kernel;
    if (!kernel?.managed || kernel.installed.length === 0) return nothing;
    return html`<lcm-srv-kernels .server=${this.server} .kernel=${kernel} ?can-write=${this.canWrite}></lcm-srv-kernels>`;
  }

  override render() {
    const server = this.server;
    return html`<div class="page">
      ${server.is_lcm_host ? html`<lcm-srv-lcm-host .server=${server} ?can-write=${this.canWrite}></lcm-srv-lcm-host>` : nothing}
      ${this.#renderHealth()}
      <lcm-srv-facts .server=${server} .status=${this.#status.data} ?can-write=${this.canWrite} @lcm-changed=${() => this.#status.reload()}></lcm-srv-facts>
      ${this.#renderKernels()}
      <lcm-srv-groups .server=${server}></lcm-srv-groups>
    </div>`;
  }
}
customElements.define('lcm-srv-overview', SrvOverview);
