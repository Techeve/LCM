import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import './lcm-toaster';
import { liftOver, toastDuration, type LcmToaster } from './lcm-toaster';
import { modalClosed, modalOpened } from './modal-watch';

let toaster: LcmToaster;
const modal = document.createElement('div');

// happy-dom kennt die Popover-API noch nicht; der Stapel ruft sie nur auf.
const proto = HTMLElement.prototype as Partial<Pick<HTMLElement, 'showPopover' | 'hidePopover'>>;
proto.showPopover ??= () => {};
proto.hidePopover ??= () => {};

beforeEach(async () => {
  vi.useFakeTimers();
  toaster = document.createElement('lcm-toaster') as LcmToaster;
  toaster.closeLabel = 'Schließen';
  toaster.moreText = (count) => `+${count} weitere`;
  toaster.waitingText = (count) => `${count} wartet`;
  document.body.append(toaster);
  await toaster.updateComplete;
});

afterEach(() => {
  modalClosed(modal);
  toaster.remove();
  vi.useRealTimers();
});

const toasts = () => [...toaster.shadowRoot!.querySelectorAll<HTMLElement>('.toast')];
const titles = () => toasts().map((element) => element.querySelector('.title')!.textContent);
const chips = () => [...toaster.shadowRoot!.querySelectorAll<HTMLElement>('.more')].map((element) => element.textContent);
const stack = () => toaster.shadowRoot!.querySelector<HTMLElement>('.stack')!;

async function settle(ms = 0): Promise<void> {
  await vi.advanceTimersByTimeAsync(ms);
  await toaster.updateComplete;
}

describe('durations', () => {
  it('keeps success 5 s, background events 8 s and errors until closed', () => {
    expect(toastDuration({ tone: 'ok', title: '' })).toBe(5000);
    expect(toastDuration({ tone: 'ok', title: '', background: true })).toBe(8000);
    expect(toastDuration({ tone: 'crit', title: '' })).toBe(0);
    expect(toastDuration({ tone: 'crit', title: '', background: true })).toBe(0);
    expect(toastDuration({ tone: 'info', title: '', duration: 1000 })).toBe(1000);
  });

  it('removes a success toast after its time and keeps an error', async () => {
    toaster.show({ tone: 'ok', title: 'Gespeichert' });
    toaster.show({ tone: 'crit', title: 'Fehlgeschlagen' });
    await settle();
    expect(titles()).toEqual(['Gespeichert', 'Fehlgeschlagen']);
    await settle(6000);
    expect(titles()).toEqual(['Fehlgeschlagen']);
  });
});

describe('stack', () => {
  it('shows the newest three at the bottom and counts the rest', async () => {
    for (const title of ['a', 'b', 'c', 'd', 'e']) toaster.show({ tone: 'crit', title });
    await settle();
    expect(titles()).toEqual(['c', 'd', 'e']);
    expect(chips()).toEqual(['+2 weitere']);
  });

  it('announces politely, errors as alert, and never takes focus', async () => {
    const before = document.activeElement;
    toaster.show({ tone: 'ok', title: 'ok' });
    toaster.show({ tone: 'crit', title: 'crit' });
    await settle();
    expect(stack().getAttribute('aria-live')).toBe('polite');
    expect(toasts().map((element) => element.getAttribute('role'))).toEqual(['status', 'alert']);
    expect(document.activeElement).toBe(before);
  });

  it('pauses while hovered and resumes afterwards', async () => {
    toaster.show({ tone: 'ok', title: 'ok' });
    await settle(1000);
    stack().dispatchEvent(new PointerEvent('pointerenter'));
    await settle(10000);
    expect(titles()).toEqual(['ok']);
    stack().dispatchEvent(new PointerEvent('pointerleave'));
    await settle(4500);
    expect(titles()).toEqual([]);
  });

  it('pauses while focus is inside', async () => {
    toaster.show({ tone: 'ok', title: 'ok' });
    await settle();
    stack().dispatchEvent(new FocusEvent('focusin'));
    await settle(10000);
    expect(titles()).toEqual(['ok']);
  });
});

describe('open window', () => {
  it('holds new toasts while a window is open and shows them after it closes', async () => {
    modalOpened(modal);
    toaster.show({ tone: 'ok', title: 'Job fertig' });
    await settle(10000);
    expect(titles()).toEqual([]);
    expect(chips()).toEqual(['1 wartet']);
    modalClosed(modal);
    await settle();
    expect(titles()).toEqual(['Job fertig']);
    expect(chips()).toEqual([]);
    await settle(5500);
    expect(titles()).toEqual([]);
  });

  it('shows critical toasts at once, even with a window open', async () => {
    modalOpened(modal);
    toaster.show({ tone: 'crit', title: 'backup01 ist kritisch' });
    await settle();
    expect(titles()).toEqual(['backup01 ist kritisch']);
  });

  it('shows the answer to a click inside the open window at once', async () => {
    modalOpened(modal);
    toaster.show({ tone: 'ok', title: 'Kopiert', immediate: true });
    await settle();
    expect(titles()).toEqual(['Kopiert']);
  });
});

describe('liftOver', () => {
  const stack = { left: 880, right: 1260, top: 700, bottom: 780 };

  it('stays put when no bar is in the way', () => {
    expect(liftOver(stack, [], 800)).toBe(0);
    // links vom Stapel, über ihm und unter ihm
    expect(liftOver(stack, [{ left: 300, right: 860, top: 730, bottom: 780 }], 800)).toBe(0);
    expect(liftOver(stack, [{ left: 880, right: 1260, top: 100, bottom: 150 }], 800)).toBe(0);
  });

  it('lifts the stack above the bar it would cover', () => {
    const bar = { left: 310, right: 1080, top: 722, bottom: 782 };
    expect(liftOver(stack, [bar], 800)).toBe(800 - 722 + 12);
  });

  it('clears the topmost of several bars', () => {
    const near = { left: 900, right: 1200, top: 740, bottom: 790 };
    const far = { left: 900, right: 1200, top: 690, bottom: 740 };
    expect(liftOver(stack, [near, far], 800)).toBe(800 - 690 + 12);
  });
});
