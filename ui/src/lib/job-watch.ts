// Wartet auf das Ende eines Jobs über den Ereigniskanal (Thema `job:<id>`).

import { FINISHED_STATUSES, jobOutputUrl, normaliseOutput } from '../api/jobs';
import { api } from './api';
import { events } from './events';

/**
 * Ruft `onDone(status)` einmal auf, sobald der Job fertig ist; der Rückgabewert bricht das Warten ab.
 *
 * Ein Job, der vor dem Abonnement fertig wird (Demo, schnelle Befehle), schickt kein Ereignis mehr. Deshalb
 * fragt der Aufruf den Stand einmal nach, sobald das Abonnement steht.
 */
export function watchJob(jobId: string, onDone: (status: string) => void): () => void {
  if (!jobId) return () => {};
  const topic = `job:${jobId}`;
  const stopTopic = events.subscribe([topic]);
  let finished = false;
  const stopEvents = events.onEvent((message) => {
    if (message.event !== 'job.finished' || message.topic !== topic) return;
    const status = (message.data as { status?: unknown } | null)?.status;
    finish(typeof status === 'string' ? status : '');
  });

  function stop(): void {
    stopEvents();
    stopTopic();
  }

  function finish(status: string): void {
    if (finished) return;
    finished = true;
    stop();
    onDone(status);
  }

  void api
    .get<unknown>(jobOutputUrl(jobId))
    .then((data) => {
      const { status } = normaliseOutput(data);
      if ((FINISHED_STATUSES as readonly string[]).includes(status)) finish(status);
    })
    .catch(() => {
      // Ohne Leserecht oder bei einem Fehler bleibt es beim Ereignis.
    });
  return () => {
    finished = true;
    stop();
  };
}
