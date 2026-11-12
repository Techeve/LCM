// Wartet auf das Ende eines Jobs über den Ereigniskanal (Thema `job:<id>`).

import type { ReactiveController, ReactiveControllerHost } from 'lit';
import { FINISHED_STATUSES, jobOutputUrl, normaliseOutput } from '../api/jobs';
import { api } from './api';
import { events } from './events';

/**
 * Ruft `onDone(status)` einmal auf, sobald der Job fertig ist; der Rückgabewert bricht das Warten ab.
 *
 * Ein Job, der vor dem Abonnement fertig wird (Demo, schnelle Befehle), schickt kein Ereignis mehr. Deshalb
 * fragt der Aufruf den Stand einmal nach, sobald das Abonnement steht, und nach jedem Resync erneut: Während
 * die Live-Verbindung getrennt war oder nur abgefragt wurde, kann das Abschlussereignis verloren gegangen sein.
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
  const stopResync = events.onResync(check);

  function stop(): void {
    stopEvents();
    stopResync();
    stopTopic();
  }

  function finish(status: string): void {
    if (finished) return;
    finished = true;
    stop();
    onDone(status);
  }

  function check(): void {
    void api
      .get<unknown>(jobOutputUrl(jobId))
      .then((data) => {
        const { status } = normaliseOutput(data);
        if ((FINISHED_STATUSES as readonly string[]).includes(status)) finish(status);
      })
      .catch(() => {
        // Ohne Leserecht oder bei einem Fehler bleibt es beim Ereignis.
      });
  }

  check();
  return () => {
    finished = true;
    stop();
  };
}

/** Die Jobs, die ein Element selbst gestartet hat: fremde Jobs zählen nicht mit, beim Abhängen endet das Warten. */
export class OwnJobs implements ReactiveController {
  readonly #host: ReactiveControllerHost;
  readonly #onDone: (status: string) => void;
  readonly #stops = new Map<string, () => void>();

  constructor(host: ReactiveControllerHost, onDone: (status: string) => void) {
    this.#host = host;
    this.#onDone = onDone;
    host.addController(this);
  }

  /** Läuft noch einer der eigenen Jobs? */
  get running(): boolean {
    return this.#stops.size > 0;
  }

  watch(jobId: string): void {
    if (!jobId || this.#stops.has(jobId)) return;
    this.#stops.set(
      jobId,
      watchJob(jobId, (status) => {
        this.#stops.delete(jobId);
        this.#host.requestUpdate();
        this.#onDone(status);
      }),
    );
    this.#host.requestUpdate();
  }

  hostDisconnected(): void {
    this.#stops.forEach((stop) => stop());
    this.#stops.clear();
  }
}
