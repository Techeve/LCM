import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-modal';
import '../../components/lcm-notice';
import '../../components/lcm-skeleton';
import '../../components/lcm-status';
import './jobs-ssh';
import {
  isActive,
  jobOutputUrl,
  jobSessionsUrl,
  liveOutput,
  normaliseOutput,
  normaliseSessions,
  type JobItem,
  type JobOutput,
  type SshSession,
} from '../../api/jobs';
import { baseStyles, emit } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { formatDateTime, formatDuration, formatRelative } from '../../lib/format';
import { t } from '../../lib/i18n';
import { LiveData } from '../../lib/live-data';
import { kindLabel, statusLabel, statusLevel } from './job-labels';
import { logStyles } from './jobs-styles';

/** Die bekannten Angaben zum Job; die Liste kennt mehr als die Ausgabe-Antwort, die Ausgabe ist dafür frischer. */
export function mergeJob(id: string, listed: JobItem | undefined, output: JobOutput | undefined): JobItem {
  const base: JobItem = listed ?? {
    id,
    server_id: null,
    server_name: '',
    kind: '',
    name: '',
    status: '',
    triggered_by: '',
    created_at: null,
    started_at: null,
    finished_at: null,
    duration_seconds: null,
    exit_code: null,
  };
  if (!output) return base;
  return { ...base, name: output.name || base.name, status: output.status || base.status, exit_code: output.exit_code ?? base.exit_code };
}

/** Laufzeit in Sekunden; bei einem laufenden Job bis jetzt. */
function elapsedSeconds(job: JobItem): number | null {
  if (job.status === 'running' && job.started_at) return Math.max(0, (Date.now() - Date.parse(job.started_at)) / 1000);
  return job.duration_seconds;
}

/**
 * <lcm-jobs-detail job-id="…" .job=${…} can-abort> – Seitenschublade zu einem Job: Angaben, Ausgabe
 * (läuft der Job, wächst sie live mit) und SSH-Sitzungen. Meldet `lcm-close` und `lcm-job-abort` { job }.
 */
export class JobsDetail extends LitElement {
  static override properties = {
    jobId: { type: String, attribute: 'job-id' },
    job: { attribute: false },
    canAbort: { type: Boolean, attribute: 'can-abort' },
  };
  static override styles = [
    baseStyles,
    logStyles,
    css`
      :host {
        display: contents;
      }
      h3 {
        margin: var(--space-5) 0 var(--space-2);
        font-size: var(--fs-m);
        font-weight: var(--fw-semibold);
      }
      dl {
        display: grid;
        grid-template-columns: auto minmax(0, 1fr);
        gap: var(--space-2) var(--space-4);
        margin: 0;
      }
      dt {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      dd {
        margin: 0;
        overflow-wrap: anywhere;
      }
      dd small {
        margin-inline-start: var(--space-2);
        color: var(--text-2);
      }
      a {
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
        text-decoration: none;
      }
      a:hover {
        text-decoration: underline;
      }
      .note {
        margin: 0 0 var(--space-2);
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare jobId: string;
  declare job: JobItem | undefined;
  declare canAbort: boolean;

  readonly #output = new LiveData<JobOutput>(this, {
    url: () => (this.jobId ? jobOutputUrl(this.jobId) : null),
    topics: () => (this.jobId ? [`job:${this.jobId}`] : []),
    transform: normaliseOutput,
  });
  #tick: ReturnType<typeof setInterval> | undefined;
  readonly #sessions = new LiveData<SshSession[]>(this, {
    url: () => (this.jobId ? jobSessionsUrl(this.jobId) : null),
    topics: () => (this.jobId ? [`job:${this.jobId}`] : []),
    transform: normaliseSessions,
  });

  constructor() {
    super();
    this.jobId = '';
    this.job = undefined;
    this.canAbort = false;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    // Dauer und „vor … Sekunden“ eines laufenden Jobs zählen mit, auch wenn gerade keine Ausgabe kommt.
    // Das SSH-Protokoll wächst ohne eigenes Ereignis: Es wird im selben Takt wie die Dauer abgeholt.
    this.#tick = setInterval(() => {
      if (!this.jobId || !isActive(this.#merged().status)) return;
      this.requestUpdate();
      this.#sessions.reload();
    }, 1000);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    clearInterval(this.#tick);
  }

  override updated(): void {
    const log = this.renderRoot.querySelector<HTMLElement>('pre.log');
    if (log && this.#merged().status === 'running') log.scrollTop = log.scrollHeight;
  }

  #merged(): JobItem {
    const output = this.#output.data;
    const listed = this.job?.id === this.jobId ? this.job : undefined;
    return mergeJob(this.jobId, listed, output?.job_id === this.jobId ? output : undefined);
  }

  #row(label: string, value: TemplateResult | string | null): TemplateResult | typeof nothing {
    return value === null || value === '' ? nothing : html`<dt>${label}</dt><dd>${value}</dd>`;
  }

  #renderFacts(job: JobItem): TemplateResult {
    const server = job.server_id === null || !job.server_name ? job.server_name : html`<a href=${`/servers/${job.server_id}`}>${job.server_name}</a>`;
    const started = job.started_at;
    const elapsed = elapsedSeconds(job);
    return html`<dl>
      ${this.#row(t('jobs.detail.status'), html`<lcm-status level=${statusLevel(job.status)} label=${statusLabel(job.status)}></lcm-status>`)}
      ${this.#row(t('jobs.detail.server'), server)} ${this.#row(t('jobs.detail.kind'), job.kind ? kindLabel(job.kind) : '')}
      ${this.#row(t('jobs.detail.triggeredBy'), job.triggered_by)}
      ${this.#row(t('jobs.detail.started'), started ? html`${formatDateTime(started)}<small>${formatRelative(started)}</small>` : '')}
      ${this.#row(t('jobs.detail.finished'), job.finished_at ? formatDateTime(job.finished_at) : '')}
      ${this.#row(t('jobs.detail.duration'), elapsed === null ? '' : formatDuration(elapsed))}
      ${this.#row(t('jobs.detail.exitCode'), job.exit_code === null ? '' : String(job.exit_code))}
    </dl>`;
  }

  #renderOutput(job: JobItem): TemplateResult {
    if (job.status === 'pending') return html`<pre class="empty">${t('jobs.detail.pending')}</pre>`;
    if (this.#output.error && !this.#output.data) return html`<pre class="empty error">${this.#output.error}</pre>`;
    if (this.#output.loading && !this.#output.data) return html`<lcm-skeleton lines="4" label=${t('jobs.detail.loading')}></lcm-skeleton>`;
    const text = liveOutput(this.#output.data, this.#sessions.data ?? []);
    if (text === '') return html`<pre class="empty">${t(isActive(job.status) ? 'jobs.detail.noOutputYet' : 'jobs.detail.noOutput')}</pre>`;
    return html`${job.status === 'running' ? html`<p class="note">${t('jobs.detail.live')}</p>` : nothing}<pre class="log">${text}</pre>`;
  }

  #renderSessions(): TemplateResult {
    if (this.#sessions.error && !this.#sessions.data) return html`<lcm-notice tone="crit">${this.#sessions.error}</lcm-notice>`;
    return html`<lcm-jobs-ssh .sessions=${this.#sessions.data ?? []} ?active=${isActive(this.#merged().status)}></lcm-jobs-ssh>`;
  }

  #abort(job: JobItem): void {
    emit(this, 'lcm-job-abort', { job });
  }

  override render() {
    const job = this.#merged();
    const subtitle = [job.kind ? kindLabel(job.kind) : '', job.server_name].filter(Boolean).join(' · ');
    return html`<lcm-modal
      size="l"
      ?open=${this.jobId !== ''}
      heading=${job.name || t('jobs.detail.title')}
      subtitle=${subtitle}
      close-label=${t('jobs.detail.close')}
    >
      ${this.jobId === ''
        ? nothing
        : html`${this.#renderFacts(job)}
            <h3>${t('jobs.detail.output')}</h3>
            ${this.#renderOutput(job)}
            <h3>${t('jobs.detail.ssh')}</h3>
            ${this.#renderSessions()}`}
      ${this.canAbort && isActive(job.status)
        ? html`<lcm-button slot="footer" variant="danger" icon="x" @click=${() => this.#abort(job)}>${t('jobs.abort.label')}</lcm-button>`
        : nothing}
    </lcm-modal>`;
  }
}
customElements.define('lcm-jobs-detail', JobsDetail);
