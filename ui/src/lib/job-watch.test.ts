import { beforeEach, describe, expect, it, vi } from 'vitest';

const listeners = vi.hoisted(() => new Set<(message: { event: string; topic: string; data: unknown }) => void>());
const resyncs = vi.hoisted(() => new Set<() => void>());
const subscribe = vi.hoisted(() => vi.fn(() => () => {}));
const get = vi.hoisted(() => vi.fn());
vi.mock('./events', () => ({
  events: {
    subscribe,
    onEvent: (listener: (message: { event: string; topic: string; data: unknown }) => void) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    onResync: (listener: () => void) => {
      resyncs.add(listener);
      return () => resyncs.delete(listener);
    },
  },
}));
vi.mock('./api', () => ({ api: { get } }));

import { OwnJobs, watchJob } from './job-watch';

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

describe('watchJob', () => {
  beforeEach(() => {
    listeners.clear();
    resyncs.clear();
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

  it('fragt nach einem Resync nach und meldet einen verpassten Abschluss', async () => {
    get.mockResolvedValueOnce({ status: 'running' }).mockResolvedValueOnce({ status: 'failed' });
    const done = vi.fn();
    watchJob('j1', done);
    await flush();
    expect(done).not.toHaveBeenCalled();
    for (const resync of resyncs) resync();
    await flush();
    expect(get).toHaveBeenCalledTimes(2);
    expect(done).toHaveBeenCalledWith('failed');
    expect(listeners.size).toBe(0);
    expect(resyncs.size).toBe(0);
  });

  it('ein noch laufender Job bleibt nach dem Resync beobachtet', async () => {
    get.mockResolvedValue({ status: 'running' });
    const done = vi.fn();
    const stop = watchJob('j1', done);
    await flush();
    for (const resync of resyncs) resync();
    await flush();
    expect(done).not.toHaveBeenCalled();
    expect(resyncs.size).toBe(1);
    stop();
    expect(resyncs.size).toBe(0);
  });
});

describe('OwnJobs', () => {
  beforeEach(() => {
    listeners.clear();
    resyncs.clear();
    get.mockReset();
    get.mockResolvedValue({ status: 'running' });
  });

  const host = () => ({ addController: vi.fn(), removeController: vi.fn(), requestUpdate: vi.fn(), updateComplete: Promise.resolve(true) });
  const finish = (jobId: string) => {
    for (const listener of [...listeners]) listener({ event: 'job.finished', topic: `job:${jobId}`, data: { status: 'success' } });
  };

  it('zählt nur die eigenen Jobs, ein fremder Abschluss ändert nichts', async () => {
    const done = vi.fn();
    const jobs = new OwnJobs(host(), done);
    jobs.watch('eigen');
    await flush();
    expect(jobs.running).toBe(true);

    finish('fremd');
    expect(jobs.running).toBe(true);
    expect(done).not.toHaveBeenCalled();

    finish('eigen');
    expect(jobs.running).toBe(false);
    expect(done).toHaveBeenCalledWith('success');
  });

  it('hört beim Abhängen auf zu warten', async () => {
    const done = vi.fn();
    const jobs = new OwnJobs(host(), done);
    jobs.watch('eigen');
    await flush();
    jobs.hostDisconnected();
    expect(listeners.size).toBe(0);
    expect(jobs.running).toBe(false);
  });
});
