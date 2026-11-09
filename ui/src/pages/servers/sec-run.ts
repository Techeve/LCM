// Start einer Sicherheitsaktion als Job, mit denselben Rückmeldungen wie `lcm-srv-action`.

import { toast } from '../../components/lcm-toaster';
import { refreshServer, type StartedJob } from '../../api/servers';
import { errorText, t } from '../../lib/i18n';
import { announceJob } from '../../lib/job-toast';

/** Startet den Job und meldet ihn mit Link; bei einem Fehler steht er im Toast. Ergebnis `null`, wenn nichts lief. */
export async function startJob(serverId: number, serverName: string, label: string, call: () => Promise<StartedJob>): Promise<StartedJob | null> {
  try {
    const job = await call();
    announceJob(label, serverName, job.job_id);
    refreshServer(serverId);
    return job;
  } catch (error) {
    toast({ tone: 'crit', title: t('servers.action.failed', { label }), message: errorText(error) });
    return null;
  }
}
