import { beforeEach, describe, expect, it, vi } from 'vitest';

const listeners = vi.hoisted(() => new Set<(message: { event: string; topic: string; data: unknown }) => void>());
const subscribe = vi.hoisted(() => vi.fn(() => () => {}));
const get = vi.hoisted(() => vi.fn());
vi.mock('./events', () => ({
  events: {
    subscribe,
    onEvent: (listener: (message: { event: string; topic: string; data: unknown }) => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  },
}));
vi.mock('./api', () => ({ api: { get } }));

import { watchJob } from './job-watch';

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('watchJob', () => {
  beforeEach(() => {
    listeners.clear();
    subscribe.mockClear();
    get.mockReset();
  });

  it('meldet das Ende aus dem Ereignis genau einmal', async () => {
    get.mockResolvedValue({ status: 'running' });
    const done = vi.fn();
    watchJob('j1', done);
    await flush();
    for (const listener of listeners) listener({ event: 'job.finished', topic: 'job:j1', data: { status: 'success' } });
    expect(done).toHaveBeenCalledTimes(1);
    expect(done).toHaveBeenCalledWith('success');
    expect(listeners.size).toBe(0);
  });

  it('meldet einen Job, der schon vor dem Abonnement fertig war', async () => {
    get.mockResolvedValue({ job_id: 'j1', status: 'success' });
    const done = vi.fn();
    watchJob('j1', done);
    await flush();
    expect(done).toHaveBeenCalledWith('success');
    expect(listeners.size).toBe(0);
  });

  it('meldet einen laufenden Job nicht vorzeitig und übersteht einen Fehler der Abfrage', async () => {
    get.mockResolvedValueOnce({ status: 'running' }).mockRejectedValueOnce(new Error('403'));
    const done = vi.fn();
    watchJob('j1', done);
    watchJob('j2', done);
    await flush();
    expect(done).not.toHaveBeenCalled();
  });

  it('das Abbrechen verhindert die Meldung', async () => {
    get.mockResolvedValue({ status: 'success' });
    const done = vi.fn();
    watchJob('j1', done)();
    await flush();
    expect(done).not.toHaveBeenCalled();
  });
});
