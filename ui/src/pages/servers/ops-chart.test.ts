import { describe, expect, it } from 'vitest';
import { chartGeometry, nearestIndex } from './ops-chart';

const box = { width: 120, height: 120, left: 10, right: 10, top: 10, bottom: 10 };

describe('storage chart geometry', () => {
  it('spans the plot from left to right and maps percent to height', () => {
    const geo = chartGeometry([0, 50, 100], box);
    expect(geo.points.map((p) => [p.x, p.y])).toEqual([
      [10, 110],
      [60, 60],
      [110, 10],
    ]);
    expect(geo.line).toBe('M10,110 L60,60 L110,10');
    expect(geo.area).toBe('M10,110 L10,110 L60,60 L110,10 L110,110 Z');
    expect(geo.yOf(85)).toBe(25);
  });

  it('centres a single day and clamps out-of-range values', () => {
    const geo = chartGeometry([140], box);
    expect(geo.points[0]).toEqual({ x: 60, y: 10, percent: 140 });
  });

  it('is empty without points', () => {
    expect(chartGeometry([], box)).toMatchObject({ points: [], line: '', area: '' });
  });

  it('finds the point nearest to the pointer', () => {
    const { points } = chartGeometry([10, 20, 30], box);
    expect(nearestIndex(points, 0)).toBe(0);
    expect(nearestIndex(points, 70)).toBe(1);
    expect(nearestIndex(points, 500)).toBe(2);
    expect(nearestIndex([], 5)).toBe(-1);
  });
});
