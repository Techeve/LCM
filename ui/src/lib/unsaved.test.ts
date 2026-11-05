import { afterEach, describe, expect, it, vi } from 'vitest';
import { unsaved } from './unsaved';

const owner = {};

afterEach(() => unsaved.set(owner, false));

describe('unsaved', () => {
  it('lets the page go without asking while nothing is held', async () => {
    const ask = vi.fn(async () => false);
    unsaved.setPrompt(ask);
    await expect(unsaved.mayLeave()).resolves.toBe(true);
    expect(ask).not.toHaveBeenCalled();
  });

  it('asks while something is held and passes the answer on', async () => {
    unsaved.setPrompt(async () => false);
    unsaved.set(owner, true);
    await expect(unsaved.mayLeave()).resolves.toBe(false);
  });

  it('warns the browser only while something is held', () => {
    const leaving = () => {
      const event = new Event('beforeunload', { cancelable: true });
      dispatchEvent(event);
      return event.defaultPrevented;
    };
    expect(leaving()).toBe(false);
    unsaved.set(owner, true);
    expect(leaving()).toBe(true);
  });
});
