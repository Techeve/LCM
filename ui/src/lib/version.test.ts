import { afterEach, describe, expect, it, vi } from 'vitest';
import { unsaved } from './unsaved';
import { VersionWatcher } from './version';

// BroadcastChannel liefert nicht an den eigenen Kontext; eine zweite Instanz ist der „andere Tab“.
const otherTab = new BroadcastChannel('lcm');
const send = async (message: unknown) => {
  otherTab.postMessage(message);
  await new Promise((resolve) => setTimeout(resolve, 20));
};

let stop: () => void = () => {};
afterEach(() => stop());

describe('VersionWatcher', () => {
  it('announces the reload and reloads shortly after when nothing is unsaved', async () => {
    const reload = vi.fn();
    const watcher = new VersionWatcher(reload, 30);
    const notice = vi.fn();
    watcher.onPending(notice);
    stop = watcher.start();
    await send({ type: 'new-version' });
    expect(watcher.reloading).toBe(true);
    expect(notice).toHaveBeenCalledOnce();
    expect(reload).not.toHaveBeenCalled();
    await new Promise((resolve) => setTimeout(resolve, 40));
    expect(reload).toHaveBeenCalledOnce();
  });

  it('reloads at once on request', async () => {
    const reload = vi.fn();
    const watcher = new VersionWatcher(reload, 10_000);
    stop = watcher.start();
    await send({ type: 'new-version' });
    watcher.reloadNow();
    expect(reload).toHaveBeenCalledOnce();
  });

  it('ignores other messages', async () => {
    const reload = vi.fn();
    stop = new VersionWatcher(reload, 0).start();
    await send({ type: 'logout' });
    await send({ type: 'something-else' });
    expect(reload).not.toHaveBeenCalled();
  });

  it('shows a notice while something is unsaved and reloads after saving', async () => {
    const reload = vi.fn();
    const watcher = new VersionWatcher(reload, 0);
    const notice = vi.fn();
    watcher.onPending(notice);
    stop = watcher.start();
    const form = {};
    unsaved.set(form, true);

    await send({ type: 'new-version' });
    expect(reload).not.toHaveBeenCalled();
    expect(watcher.pending).toBe(true);
    expect(notice).toHaveBeenCalledWith(true);

    unsaved.set(form, false);
    expect(watcher.pending).toBe(false);
    expect(watcher.reloading).toBe(true);
    await new Promise((resolve) => setTimeout(resolve, 5));
    expect(reload).toHaveBeenCalledOnce();
  });
});

describe('unsaved', () => {
  it('tracks several owners and notifies on empty/non-empty changes only', () => {
    const listener = vi.fn();
    const off = unsaved.subscribe(listener);
    const a = {};
    const b = {};
    unsaved.set(a, true);
    unsaved.set(b, true);
    expect(listener).toHaveBeenCalledTimes(1);
    unsaved.set(a, false);
    expect(unsaved.has()).toBe(true);
    unsaved.set(b, false);
    expect(unsaved.has()).toBe(false);
    expect(listener).toHaveBeenCalledTimes(2);
    off();
  });
});
