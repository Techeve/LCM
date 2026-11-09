import { LitElement, html } from 'lit';
import '../../components/lcm-dialog';
import { abortJob, type JobItem } from '../../api/jobs';
import type { Impact } from '../../components/lcm-dialog';
import { toast } from '../../components/lcm-toaster';
import { emit } from '../../components/shared';
import { I18nController } from '../../lib/controllers';
import { errorText, t } from '../../lib/i18n';

/** <lcm-job-abort-dialog .job=${…}> – fragt vor dem Abbruch eines Jobs nach. Offen, solange `job` gesetzt ist; meldet `lcm-close`. */
export class LcmJobAbortDialog extends LitElement {
  static override properties = { job: { attribute: false }, _error: { state: true }, _busy: { state: true } };

  declare job: JobItem | undefined;
  declare _error: string;
  declare _busy: boolean;

  constructor() {
    super();
    this.job = undefined;
    this._error = '';
    this._busy = false;
    new I18nController(this);
  }

  #impacts(job: JobItem): Impact[] {
    const running = job.status === 'running';
    return [
      ...(job.server_name ? [{ label: t('jobs.abort.impact.server'), value: job.server_name }] : []),
      { label: t('jobs.abort.impact.effect'), value: t(running ? 'jobs.abort.impact.effectRunning' : 'jobs.abort.impact.effectPending') },
      { label: t('jobs.abort.impact.reversible'), value: t(running ? 'jobs.abort.impact.reversibleRunning' : 'jobs.abort.impact.reversiblePending'), tone: 'warn' },
    ];
  }

  async #confirm(): Promise<void> {
    const job = this.job;
    if (!job) return;
    this._busy = true;
    this._error = '';
    try {
      await abortJob(job.id);
      toast({ tone: 'ok', title: t('jobs.abort.done', { name: job.name }) });
      emit(this, 'lcm-close');
    } catch (error) {
      this._error = errorText(error);
    } finally {
      this._busy = false;
    }
  }

  override render() {
    const job = this.job;
    return html`<lcm-dialog
      ?open=${job !== undefined}
      tone="danger"
      heading=${t('jobs.abort.confirm', { name: job?.name ?? '' })}
      confirm-label=${t('jobs.abort.action')}
      cancel-label=${t('jobs.abort.cancel')}
      .impacts=${job ? this.#impacts(job) : []}
      error=${this._error}
      ?busy=${this._busy}
      @lcm-cancel=${() => emit(this, 'lcm-close')}
      @lcm-confirm=${() => void this.#confirm()}
    ></lcm-dialog>`;
  }
}
customElements.define('lcm-job-abort-dialog', LcmJobAbortDialog);
