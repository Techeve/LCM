import { afterEach, describe, expect, it, vi } from 'vitest';
import type { ReactiveController, ReactiveControllerHost } from 'lit';
import { FadeOnChange, FreshKeys, fadeIn, fadeOut, freshKeys, parseDuration, prefersReducedMotion } from './motion';

function reduceMotion(reduce: boolean): void {
  vi.stubGlobal('matchMedia', (query: string) => ({ matches: reduce && query.includes('reduce') }));
}

function element(tokens: Record<string, string> = { '--dur-1': '0.16s', '--ease': 'ease-in-out' }) {
  const el = document.body.appendChild(document.createElement('div'));
  for (const [name, value] of Object.entries(tokens)) el.style.setProperty(name, value);
  const animation = { finished: Promise.resolve() };
  const animate = vi.fn(() => animation as unknown as Animation);
  el.animate = animate;
  return { el, animate };
}

afterEach(() => vi.unstubAllGlobals());

describe('parseDuration', () => {
  it('liest Sekunden und Millisekunden', () => {
    expect(parseDuration('0.16s')).toBe(160);
    expect(parseDuration(' 120ms ')).toBe(120);
    expect(parseDuration('.5s')).toBe(500);
  });

  it('macht aus Unlesbarem 0', () => {
    expect(parseDuration('')).toBe(0);
    expect(parseDuration('fast')).toBe(0);
    expect(parseDuration('-1s')).toBe(0);
    expect(parseDuration('0s')).toBe(0);
  });
});

describe('prefersReducedMotion', () => {
  it('folgt der Medienabfrage', () => {
    reduceMotion(true);
    expect(prefersReducedMotion()).toBe(true);
    reduceMotion(false);
    expect(prefersReducedMotion()).toBe(false);
  });
});

describe('fadeIn / fadeOut', () => {
  it('blendet mit Token-Dauer und -Kurve ein', () => {
    reduceMotion(false);
    const { el, animate } = element();
    fadeIn(el);
    expect(animate).toHaveBeenCalledOnce();
    const [frames, options] = animate.mock.calls[0] as unknown as [Keyframe[], KeyframeAnimationOptions];
    expect(frames[0]).toMatchObject({ opacity: 0.3, transform: 'translateY(4px)' });
    expect(frames[1]).toMatchObject({ opacity: 1, transform: 'none' });
    expect(options).toMatchObject({ duration: 160, easing: 'ease-in-out' });
  });

  it('bewegt nichts bei reduzierter Bewegung', async () => {
    reduceMotion(true);
    const { el, animate } = element();
    expect(fadeIn(el)).toBeNull();
    await fadeOut(el);
    expect(animate).not.toHaveBeenCalled();
  });

  it('bewegt nichts, wenn das Token 0s ist', () => {
    reduceMotion(false);
    const { el, animate } = element({ '--dur-1': '0s' });
    expect(fadeIn(el)).toBeNull();
    expect(animate).not.toHaveBeenCalled();
  });

  it('hält das ausgeblendete Element unsichtbar, bis es entfernt wird', async () => {
    reduceMotion(false);
    const { el, animate } = element();
    await fadeOut(el);
    const [, options] = animate.mock.calls[0] as unknown as [Keyframe[], KeyframeAnimationOptions];
    expect(options.fill).toBe('forwards');
  });
});

describe('FadeOnChange', () => {
  function host() {
    let controller: ReactiveController | undefined;
    const fake = { addController: (c: ReactiveController) => (controller = c) } as unknown as ReactiveControllerHost;
    return { fake, update: () => controller?.hostUpdated?.(), disconnect: () => controller?.hostDisconnected?.() };
  }

  it('bewegt sich beim ersten Rendern nicht, danach bei jedem neuen Schlüssel', () => {
    reduceMotion(false);
    const { el, animate } = element();
    const h = host();
    let key = 1;
    new FadeOnChange(h.fake, () => key, () => el);
    h.update();
    expect(animate).not.toHaveBeenCalled();
    h.update();
    expect(animate).not.toHaveBeenCalled();
    key = 2;
    h.update();
    expect(animate).toHaveBeenCalledOnce();
  });

  it('fängt nach dem Abhängen wieder ohne Bewegung an', () => {
    reduceMotion(false);
    const { el, animate } = element();
    const h = host();
    let key = 'a';
    new FadeOnChange(h.fake, () => key, () => el);
    h.update();
    h.disconnect();
    key = 'b';
    h.update();
    expect(animate).not.toHaveBeenCalled();
  });
});

describe('freshKeys', () => {
  const set = (...keys: string[]) => new Set(keys);

  it('nennt neu hinzugekommene Einträge', () => {
    expect(freshKeys(set('1', '2'), set('3', '1', '2'))).toEqual(set('3'));
  });

  it('nennt nichts, wenn nichts übereinstimmt (andere Seite, anderer Filter)', () => {
    expect(freshKeys(set('1', '2'), set('3', '4'))).toEqual(set());
  });

  it('nennt den ersten Eintrag einer bisher leeren Liste', () => {
    expect(freshKeys(set(), set('1'))).toEqual(set('1'));
  });
});

describe('FreshKeys', () => {
  it('die erste Liste ist nie neu, dieselbe Liste liefert dieselbe Antwort', () => {
    const fresh = new FreshKeys<{ id: number }>((item) => String(item.id));
    const first = [{ id: 1 }];
    expect(fresh.track(first)).toEqual(new Set());
    const second = [{ id: 2 }, { id: 1 }];
    expect(fresh.track(second)).toEqual(new Set(['2']));
    expect(fresh.track(second)).toEqual(new Set(['2']));
    expect(fresh.track([...second])).toEqual(new Set());
  });
});
