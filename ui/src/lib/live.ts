// Verbindet den Ereigniskanal mit dem Datenspeicher: Ein Ereignis macht die
// betroffenen Adressen ungültig, abonnierte werden neu geladen. Nach `resync`
// wird alles Sichtbare einmal neu geladen.

import { events, type EventMessage } from './events';
import { store, type InvalidateOptions } from './store';
import { checkForNewVersion } from './sw-register';

interface Target extends InvalidateOptions {
  prefix: string;
}

const SERVERS = '/api/v1/servers';
const DASHBOARD: Target = { prefix: '/api/v1/dashboard' };
const JOBS = '/api/v1/jobs';

function topicId(topic: string, kind: string): string | null {
  return topic.startsWith(`${kind}:`) ? topic.slice(kind.length + 1) : null;
}

/** Welche Adressen ein Ereignis betrifft. */
export function targetsFor(message: EventMessage): Target[] {
  switch (message.event) {
    case 'server.busy':
    case 'server.status': {
      const id = topicId(message.topic, 'server');
      return [
        ...(id ? [{ prefix: `${SERVERS}/${id}` }] : []),
        { prefix: SERVERS, withChildren: false },
        { prefix: `${SERVERS}/statuses` },
        DASHBOARD,
        // Ein Job startet oder reiht sich ein: der Server meldet es als „belegt“, die Jobliste ändert sich.
        ...(message.event === 'server.busy' ? [{ prefix: JOBS, withChildren: false }] : []),
      ];
    }
    case 'job.progress':
      return [{ prefix: JOBS }];
    case 'job.finished':
      return [{ prefix: JOBS }, DASHBOARD];
    case 'alert.fired':
      return [{ prefix: '/api/v1/alert-events' }, DASHBOARD];
    case 'bulk.progress':
      return [{ prefix: '/api/v1/security/update-all' }];
    case 'system.build':
      // Fortschritt des Selbst-Updates; die Zielversion steht im Update-Stand.
      return [{ prefix: '/api/v1/system/self-update' }, { prefix: '/api/v1/system/update-info' }];
    case 'system.maintenance':
      return [{ prefix: '/api/v1/system/maintenance' }];
    default:
      return [];
  }
}

/** Einmal beim Start aufrufen; der Rückgabewert löst die Verbindung wieder. */
export function connectLiveData(): () => void {
  const stopEvents = events.onEvent((message) => {
    if (message.event === 'system.build') checkForNewVersion();
    for (const { prefix, ...options } of targetsFor(message)) store.invalidate(prefix, options);
  });
  const stopResync = events.onResync(() => store.refreshSubscribed());
  return () => {
    stopEvents();
    stopResync();
  };
}
