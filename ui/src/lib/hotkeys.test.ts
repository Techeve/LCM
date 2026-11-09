import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { Hotkeys, SAVE_EVENT, SEQUENCE_MS, gotoPages, isTyping, type HotkeyHost } from './hotkeys';

const allPermissions = () => true;
let stop: () => void;
let host: HotkeyHost & { go: ReturnType<typeof vi.fn>; toggleHelp: ReturnType<typeof vi.fn> };

function press(key: string, init: KeyboardEventInit = {}, target: EventTarget = document.body): KeyboardEvent {
  const event = new KeyboardEvent('keydown', { key, bubbles: true, composed: true, cancelable: true, ...init });
  target.dispatchEvent(event);
  return event;
}

function start(overrides: Partial<HotkeyHost> = {}): void {
  host = { isAuthenticated: () => true, can: allPermissions, go: vi.fn(), toggleHelp: vi.fn(), ...overrides } as typeof host;
  stop = new Hotkeys(host).start();
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  stop();
  vi.useRealTimers();
  document.body.replaceChildren();
});

describe('jumps with g and a letter', () => {
  it.each([
    ['d', '/'],
    ['v', '/servers'],
    ['g', '/groups'],
    ['u', '/linux-users'],
    ['s', '/security'],
    ['c', '/containers'],
    ['j', '/jobs'],
    ['e', '/settings'],
    ['a', '/account'],
    ['h', '/docs'],
  ])('g %s goes to %s', (key, path) => {
    start();
    press('g');
    const event = press(key);
    expect(host.go).toHaveBeenCalledWith(path);
    expect(event.defaultPrevented).toBe(true);
  });

  it('does nothing for a letter alone, for unknown letters and for pages without permission', () => {
    start({ can: (permission) => permission === 'jobs:read' });
    press('j');
    press('g');
    press('x');
    press('g');
    press('d'); // servers:read fehlt
    expect(host.go).not.toHaveBeenCalled();
    press('g');
    press('j');
    expect(host.go).toHaveBeenCalledWith('/jobs');
  });

  it('forgets the g after the sequence time', () => {
    start();
    press('g');
    vi.advanceTimersByTime(SEQUENCE_MS + 1);
    press('d');
    expect(host.go).not.toHaveBeenCalled();
  });

  it('forgets the g on Escape and ignores keys with modifiers', () => {
    start();
    press('g');
    press('Escape');
    press('d');
    press('g');
    press('d', { ctrlKey: true });
    expect(host.go).not.toHaveBeenCalled();
  });

  it('is off while typing, also inside a shadow root, and while signed out', () => {
    start();
    const input = document.createElement('input');
    const host2 = document.createElement('div');
    const root = host2.attachShadow({ mode: 'open' });
    const inner = document.createElement('input');
    root.append(inner);
    document.body.append(input, host2);
    for (const field of [input, inner]) {
      press('g', {}, field);
      press('d', {}, field);
    }
    press('?', {}, input);
    expect(host.go).not.toHaveBeenCalled();
    expect(host.toggleHelp).not.toHaveBeenCalled();
    stop();

    start({ isAuthenticated: () => false });
    press('g');
    press('d');
    press('?');
    expect(host.go).not.toHaveBeenCalled();
    expect(host.toggleHelp).not.toHaveBeenCalled();
  });

  it('does not jump behind an open dialog, but the help key still works there', () => {
    start();
    const dialog = document.createElement('dialog');
    dialog.setAttribute('open', '');
    const button = document.createElement('button');
    dialog.append(button);
    document.body.append(dialog);
    press('g', {}, button);
    press('d', {}, button);
    expect(host.go).not.toHaveBeenCalled();
    press('?', {}, button);
    expect(host.toggleHelp).toHaveBeenCalledOnce();
  });
});

describe('help and save', () => {
  it('toggles the help with ?', () => {
    start();
    expect(press('?').defaultPrevented).toBe(true);
    expect(host.toggleHelp).toHaveBeenCalledOnce();
  });

  it('dispatches the save event, also while typing, and keeps the browser quiet only if a page handled it', () => {
    start();
    const saved = vi.fn((event: Event) => event.preventDefault());
    document.addEventListener(SAVE_EVENT, saved);
    const input = document.createElement('input');
    document.body.append(input);

    const handled = press('s', { ctrlKey: true, metaKey: true }, input);
    expect(saved).toHaveBeenCalledOnce();
    expect(handled.defaultPrevented).toBe(true);

    document.removeEventListener(SAVE_EVENT, saved);
    const ignored = press('s', { ctrlKey: true, metaKey: true });
    expect(ignored.defaultPrevented).toBe(false);
  });
});

describe('helpers', () => {
  it('knows what counts as typing', () => {
    const text = document.createElement('input');
    const box = document.createElement('input');
    box.type = 'checkbox';
    expect(isTyping(text)).toBe(true);
    expect(isTyping(document.createElement('textarea'))).toBe(true);
    expect(isTyping(box)).toBe(false);
    expect(isTyping(document.createElement('button'))).toBe(false);
    expect(isTyping(undefined)).toBe(false);
  });

  it('lists the jump targets by permission', () => {
    expect(gotoPages((permission) => permission === 'groups:read').map((page) => page.hotkey)).toEqual(['g', 'a', 'h']);
  });
});

describe('page keys', () => {
  it('runs the page command for a single key and focuses the search with /', () => {
    const pageKey = vi.fn((key: string) => key === 'n');
    const focusSearch = vi.fn(() => true);
    start({ pageKey, focusSearch });
    expect(press('n').defaultPrevented).toBe(true);
    expect(press('x').defaultPrevented).toBe(false);
    expect(press('/').defaultPrevented).toBe(true);
    expect(focusSearch).toHaveBeenCalledOnce();
    expect(pageKey).toHaveBeenCalledWith('n');
  });

  it('keeps g sequences: g u jumps instead of running the page key u', () => {
    const pageKey = vi.fn(() => true);
    start({ pageKey });
    press('g');
    press('u');
    expect(host.go).toHaveBeenCalledWith('/linux-users');
    expect(pageKey).not.toHaveBeenCalled();
  });
});
