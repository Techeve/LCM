import { beforeEach, describe, expect, it, vi } from 'vitest';

const toast = vi.hoisted(() => vi.fn());
const finish = vi.hoisted(() => ({ done: (_status: string) => {} }));
vi.mock('../components/lcm-toaster', () => ({ toast }));
vi.mock('./job-watch', () => ({
  watchJob: (_id: string, done: (status: string) => void) => {
    finish.done = done;
    return () => {};
  },
}));
vi.mock('./i18n', () => ({ t: (key: string) => key }));
vi.mock('./router', () => ({ router: { navigate: vi.fn() } }));
const can = vi.hoisted(() => vi.fn((_permission: string) => true));
vi.mock('./auth', () => ({ auth: { can } }));

import { announceJob } from './job-toast';

describe('announceJob', () => {
  beforeEach(() => {
    toast.mockClear();
    can.mockReturnValue(true);
  });

  it('meldet Start und Ende, beide mit Link', () => {
    announceJob('Updates', 'web1', 'j1');
    finish.done('failed');
    expect(toast).toHaveBeenCalledTimes(2);
    expect(toast.mock.calls[1]?.[0]).toMatchObject({ tone: 'crit', title: 'servers.action.finishedFailed', actionLabel: 'app.showJob' });
  });

  it('bietet ohne jobs:read keinen Sprung zum Job an', () => {
    can.mockImplementation((permission) => permission !== 'jobs:read');
    announceJob('Updates', 'web1', 'j1');
    finish.done('failed');
    expect(toast).toHaveBeenCalledTimes(2);
    for (const [options] of toast.mock.calls) expect(options).not.toHaveProperty('actionLabel');
  });

  it('ohne Job bleibt es beim Start', () => {
    announceJob('Updates', 'web1', undefined);
    expect(toast).toHaveBeenCalledTimes(1);
  });
});
