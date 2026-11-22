import { afterEach, describe, expect, it, vi } from 'vitest';
import { pickCorner, SrvLcmTour, type TourStep } from './ov-lcm-host-tour';

vi.mock('../../lib/i18n', async (original) => ({
  ...(await original<typeof import('../../lib/i18n')>()),
  t: (key: string, params?: Record<string, unknown>) => (params ? `${key} ${JSON.stringify(params)}` : key),
}));

const STEPS: TourStep[] = [
  { key: 'a', title: 'A', text: 'text a', targets: ['#target-a'] },
  { key: 'b', title: 'B', text: 'text b', badge: 'Empfohlen', targets: [] },
  { key: 'c', title: 'C', text: 'text c', targets: [] },
];

async function mount(step = 0): Promise<SrvLcmTour> {
  const tour = new SrvLcmTour();
  tour.steps = STEPS;
  tour.step = step;
  document.body.append(tour);
  await tour.updateComplete;
  return tour;
}
const field = (tour: SrvLcmTour, name: string) => tour.shadowRoot!.querySelector(`[data-field="${name}"]`)?.textContent?.trim();
const buttons = (tour: SrvLcmTour) => [...tour.shadowRoot!.querySelectorAll('lcm-button')].map((button) => button.getAttribute('data-action'));

afterEach(() => document.body.replaceChildren());

describe('Einführungsfenster', () => {
  it('zählt alle Schritte durchgehend und zeigt Titel und Text', async () => {
    const tour = await mount(1);
    expect(field(tour, 'tour-progress')).toBe('setup.intro.progress {"number":2,"total":3}');
    expect(tour.shadowRoot!.querySelectorAll('.dot')).toHaveLength(3);
    expect(tour.shadowRoot!.querySelector('.dot[data-active]')).toBe(tour.shadowRoot!.querySelectorAll('.dot')[1]);
    expect(field(tour, 'tour-title')).toContain('B');
    expect(field(tour, 'tour-title')).toContain('Empfohlen');
    expect(field(tour, 'tour-text')).toBe('text b');
  });

  it('hat nur Weiter, Zurück und Schließen, keine Aktionsknöpfe', async () => {
    const first = await mount(0);
    expect(buttons(first)).toEqual(['tour-next', 'close-intro']);
    expect(first.shadowRoot!.querySelector('slot')).toBeNull();
    document.body.replaceChildren();
    const middle = await mount(1);
    expect(buttons(middle)).toEqual(['tour-back', 'tour-next', 'close-intro']);
    document.body.replaceChildren();
    const last = await mount(2);
    expect(buttons(last)).toEqual(['tour-back', 'close-intro']);
    expect(last.shadowRoot!.querySelector('[data-action="close-intro"]')!.textContent).toContain('setup.intro.finish');
  });

  it('meldet Weiter, Zurück und Schließen', async () => {
    const tour = await mount(1);
    const steps: number[] = [];
    let closed = 0;
    tour.addEventListener('lcm-tour-step', (event) => steps.push((event as CustomEvent<{ step: number }>).detail.step));
    tour.addEventListener('lcm-tour-close', () => (closed += 1));
    tour.shadowRoot!.querySelector<HTMLElement>('[data-action="tour-next"]')!.click();
    tour.shadowRoot!.querySelector<HTMLElement>('[data-action="tour-back"]')!.click();
    tour.shadowRoot!.querySelector<HTMLElement>('[data-action="close-intro"]')!.click();
    expect(steps).toEqual([2, 0]);
    expect(closed).toBe(1);
  });

  it('umrahmt das Ziel, auch wenn es erst später erscheint, und scrollt es ins Bild, wenn es hinter der Kopfleiste liegt', async () => {
    vi.useFakeTimers();
    try {
      const tour = await mount(0);
      expect(tour.shadowRoot!.querySelector('[data-field="tour-ring"]')).toBeNull();
      const target = document.createElement('div');
      target.id = 'target-a';
      target.getBoundingClientRect = () => ({ left: 10, top: 50, right: 210, bottom: 110, width: 200, height: 60 }) as DOMRect;
      const scroll = vi.fn();
      target.scrollIntoView = scroll;
      document.body.append(target);
      await vi.advanceTimersByTimeAsync(300);
      await tour.updateComplete;
      const ring = tour.shadowRoot!.querySelector<HTMLElement>('[data-field="tour-ring"]')!;
      expect(ring).not.toBeNull();
      expect(ring.style.getPropertyValue('inset-inline-start')).toBe('6px');
      expect(ring.style.getPropertyValue('inline-size')).toBe('208px');
      expect(scroll).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe('pickCorner', () => {
  const panel = { w: 368, h: 300 };
  const view = { w: 1440, h: 900 };

  it('bleibt, wo das Fenster nichts verdeckt', () => {
    expect(pickCorner({ x: 100, y: 300, w: 300, h: 100 }, panel, view, 'right-top')).toBe('right-top');
    expect(pickCorner(null, panel, view, 'left-bottom')).toBe('left-bottom');
  });

  it('weicht nach links aus, wenn das Ziel unter dem Fenster liegt', () => {
    expect(pickCorner({ x: 1100, y: 200, w: 200, h: 100 }, panel, view, 'right-top')).toBe('left-top');
  });

  it('nimmt eine untere Ecke, wenn das Ziel die ganze Breite oben einnimmt', () => {
    expect(pickCorner({ x: 40, y: 130, w: 1360, h: 60 }, panel, view, 'right-top')).toBe('right-bottom');
  });

  it('bleibt, wo es ist, wenn keine Ecke frei ist', () => {
    expect(pickCorner({ x: 0, y: 0, w: 1440, h: 900 }, panel, view, 'right-top')).toBe('right-top');
  });
});
