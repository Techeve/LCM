// Rückmeldung zu einem Server-Job: beim Start und, unabhängig von der gerade offenen Seite, am Ende. Jeweils mit Link zum Job.

import { toast, type ToastOptions } from '../components/lcm-toaster';
import { t } from './i18n';
import { jobLink } from './job-link';
import { watchJob } from './job-watch';

const ENDINGS: Record<string, { tone: ToastOptions['tone']; key: string }> = {
  success: { tone: 'ok', key: 'servers.action.finished' },
  failed: { tone: 'crit', key: 'servers.action.finishedFailed' },
  aborted: { tone: 'warn', key: 'servers.action.finishedAborted' },
  blocked: { tone: 'warn', key: 'servers.action.finishedBlocked' },
};

export function announceJob(label: string, name: string, jobId: string | undefined): void {
  toast({ tone: 'info', title: t('servers.action.started', { label, name }), ...jobLink(jobId) });
  if (!jobId) return;
  watchJob(jobId, (status) => {
    const ending = ENDINGS[status];
    if (ending) toast({ tone: ending.tone, title: t(ending.key, { label, name }), ...jobLink(jobId) });
  });
}
