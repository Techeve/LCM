// Geometrie des Speicherverlaufs: Tagesprozente → Punkte, Linie und Fläche in einer Zeichenfläche fester Höhe.
// Ohne Bibliothek und ohne DOM, damit sie sich testen lässt.

export interface ChartBox {
  width: number;
  height: number;
  /** Rand für Achsenbeschriftung: links (Prozent), unten (Tage), oben und rechts Luft. */
  left: number;
  right: number;
  top: number;
  bottom: number;
}

export interface ChartPoint {
  x: number;
  y: number;
  percent: number;
}

export interface ChartGeometry {
  points: ChartPoint[];
  line: string;
  area: string;
  baseY: number;
  /** y einer Prozentlinie (Raster, Warnschwelle). */
  yOf: (percent: number) => number;
}

const round = (value: number) => Math.round(value * 10) / 10;
const clamp = (percent: number) => Math.max(0, Math.min(100, percent));

/** Ein einzelner Tag steht in der Mitte; sonst reicht der Verlauf vom linken bis zum rechten Rand. */
export function chartGeometry(percents: number[], box: ChartBox): ChartGeometry {
  const plotW = Math.max(0, box.width - box.left - box.right);
  const plotH = Math.max(0, box.height - box.top - box.bottom);
  const baseY = box.top + plotH;
  const yOf = (percent: number) => round(box.top + plotH - (clamp(percent) * plotH) / 100);
  const n = percents.length;
  const xOf = (i: number) => round(box.left + (n === 1 ? plotW / 2 : (i * plotW) / (n - 1)));
  const points = percents.map((percent, i) => ({ x: xOf(i), y: yOf(percent), percent }));
  if (points.length === 0) return { points, line: '', area: '', baseY, yOf };
  const line = points.map((p, i) => `${i === 0 ? 'M' : 'L'}${p.x},${p.y}`).join(' ');
  const first = points[0]!;
  const last = points[points.length - 1]!;
  const area = `M${first.x},${baseY} ${points.map((p) => `L${p.x},${p.y}`).join(' ')} L${last.x},${baseY} Z`;
  return { points, line, area, baseY, yOf };
}

/** Der Punkt, der einer waagerechten Position am nächsten liegt (Überfahren mit der Maus). */
export function nearestIndex(points: ChartPoint[], x: number): number {
  let best = -1;
  let distance = Number.POSITIVE_INFINITY;
  points.forEach((point, index) => {
    const d = Math.abs(point.x - x);
    if (d < distance) {
      best = index;
      distance = d;
    }
  });
  return best;
}
