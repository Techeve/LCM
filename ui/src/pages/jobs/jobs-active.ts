import { LitElement, css, html, nothing } from 'lit';
import { repeat } from 'lit/directives/repeat.js';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-job-progress';
import { isActive, type JobItem } from '../../api/jobs';
import { baseStyles, emit } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { formatRelative } from '../../lib/format';
import { FreshKeys } from '../../lib/motion';
import { t } from '../../lib/i18n';
import { kindLabel, statusLabel } from './job-labels';

/** <lcm-jobs-active .jobs=${…} can-abort> – laufende und wartende Jobs. Meldet `lcm-job-open` { id } und `lcm-job-abort` { job }. */
export class JobsActive extends LitElement {
  static override properties = { jobs: { attribute: false }, canAbort: { type: Boolean, attribute: 'can-abort' } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      ul {
        display: grid;
        grid-template-columns: repeat(auto-fill, minmax(22rem, 1fr));
        gap: var(--space-4);
        margin: 0;
        padding: 0;
        list-style: none;
      }
      li {
        display: flex;
        flex-direction: column;
        gap: var(--space-2);
      }
      .row {
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: var(--space-2);
      }
      .server {
        color: var(--text-2);
        font-size: var(--fs-s);
      }
    `,
  ];

  declare jobs: JobItem[];
  declare canAbort: boolean;
  readonly #fresh = new FreshKeys<JobItem>((job) => job.id);

  constructor() {
    super();
    this.jobs = [];
    this.canAbort = false;
    new I18nController(this);
  }

  #renderJob(job: JobItem, fresh: ReadonlySet<string>) {
    const running = job.status === 'running';
    const since = job.started_at ?? job.created_at;
    return html`<li ?data-fresh=${fresh.has(job.id)}>
      <lcm-job-progress
        state=${running ? 'running' : 'queued'}
        indeterminate
        label=${job.name || kindLabel(job.kind)}
        state-label=${statusLabel(job.status)}
        meta=${since ? formatRelative(since) : ''}
        detail=${job.server_name}
      ></lcm-job-progress>
      <div class="row">
        <lcm-button size="small" variant="outline" @click=${() => emit(this, 'lcm-job-open', { id: job.id })}>${t('jobs.active.open')}</lcm-button>
        ${this.canAbort && isActive(job.status)
          ? html`<lcm-button size="small" variant="ghost" icon="x" @click=${() => emit(this, 'lcm-job-abort', { job })}>${t('jobs.abort.label')}</lcm-button>`
          : nothing}
      </div>
    </li>`;
  }

  override render() {
    const fresh = this.#fresh.track(this.jobs);
    if (this.jobs.length === 0) return nothing;
    return html`<lcm-card level="2" heading=${t('jobs.active.title')} subtitle=${t('jobs.active.subtitle', { count: this.jobs.length })}>
      <ul>${repeat(this.jobs, (job) => job.id, (job) => this.#renderJob(job, fresh))}</ul>
    </lcm-card>`;
  }
}
customElements.define('lcm-jobs-active', JobsActive);
