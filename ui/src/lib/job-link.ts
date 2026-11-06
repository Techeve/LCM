// Folgeaktion für Meldungen zu einem Job: „Job ansehen“ öffnet ihn als Fenster über der aktuellen Seite.
// Jobs lesen verlangt `jobs:read`; ohne das Recht antwortet die API mit 403, also bietet keine Meldung den Sprung an.

import type { ToastOptions } from '../components/lcm-toaster';
import { auth } from './auth';
import { t } from './i18n';
import { jobWindow } from './job-window';
import { router } from './router';

type Action = Pick<ToastOptions, 'actionLabel' | 'onAction'>;

/** Link zum Job für `toast({...})`; ohne Kennung oder ohne Recht bleibt die Meldung ohne Aktion. */
export function jobLink(jobId: string | undefined): Action {
  if (!jobId || !auth.can('jobs:read')) return {};
  return { actionLabel: t('app.showJob'), onAction: () => jobWindow.open(jobId) };
}

/** Link zur Jobliste für `toast({...})` (Läufe ohne einzelne Kennung); ohne Recht ohne Aktion. */
export function jobsLink(label: string): Action {
  if (!auth.can('jobs:read')) return {};
  return { actionLabel: label, onAction: () => void router.navigate('/jobs', { ask: true }) };
}
