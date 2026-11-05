import { LitElement, css, html, nothing, svg } from 'lit';
import { baseStyles } from '../../components/shared';
import type { HistoryPoint } from '../../api/servers-ops';
import { I18nController } from '../../lib/controllers';
import { formatBytes, formatDate, formatNumber } from '../../lib/format';
import { t } from '../../lib/i18n';
import { chartGeometry, nearestIndex, type ChartBox } from './ops-chart';

const HEIGHT = 200;
const GRID = [0, 50, 100];
const MB = 1024 * 1024;
const TIP_W = 150;

/**
 * `<lcm-ops-storage-chart .points warn=85>`: Verlauf der Belegung des Root-Volumes (Tagesdurchschnitte) als ruhige
 * Fläche mit Warnschwelle. Die Breite folgt dem Platz (ResizeObserver), damit Schrift nicht mitskaliert.
 */
export class OpsStorageChart extends LitElement {
  static override properties = {
    points: { attribute: false },
    warn: { type: Number },
    _width: { state: true },
    _hover: { state: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        min-inline-size: 0;
      }
      svg {
        display: block;
        inline-size: 100%;
        block-size: 12.5rem;
        overflow: visible;
      }
      .grid {
        stroke: var(--line);
        stroke-width: 1;
      }
      .axis {
        fill: var(--text-3);
        font-size: 11px;
        font-variant-numeric: tabular-nums;
      }
      .warn {
        stroke: var(--warn);
        stroke-width: 1.25;
        stroke-dasharray: 5 4;
      }
      .warn-label {
        fill: var(--warn-text);
        font-size: 11px;
      }
      .area {
        fill: var(--accent-soft);
        opacity: 0.8;
      }
      .line {
        fill: none;
        stroke: var(--accent);
        stroke-width: 2;
        stroke-linejoin: round;
      }
      .dot {
        fill: var(--accent);
      }
      .guide {
        stroke: var(--text-3);
        stroke-dasharray: 3 3;
      }
      .tip rect {
        fill: var(--surface-overlay);
        stroke: var(--line);
      }
      .tip text {
        fill: var(--text-1);
        font-size: 12px;
        font-variant-numeric: tabular-nums;
      }
      .tip .muted {
        fill: var(--text-2);
      }
    `,
  ];

  declare points: HistoryPoint[];
  declare warn: number;
  declare _width: number;
  declare _hover: number;

  #observer: ResizeObserver | undefined;

  constructor() {
    super();
    this.points = [];
    this.warn = 0;
    this._width = 720;
    this._hover = -1;
    new I18nController(this);
  }

  override connectedCallback(): void {
    super.connectedCallback();
    this.#observer = new ResizeObserver((entries) => {
      const width = Math.round(entries[0]?.contentRect.width ?? 0);
      if (width > 0 && width !== this._width) this._width = width;
    });
    this.#observer.observe(this);
  }

  override disconnectedCallback(): void {
    super.disconnectedCallback();
    this.#observer?.disconnect();
  }

  get #box(): ChartBox {
    return { width: this._width, height: HEIGHT, left: 40, right: 12, top: 12, bottom: 26 };
  }

  #onMove(event: PointerEvent, xs: number[]): void {
    const rect = (event.currentTarget as SVGSVGElement).getBoundingClientRect();
    const x = ((event.clientX - rect.left) / Math.max(1, rect.width)) * this._width;
    this._hover = nearestIndex(xs.map((px) => ({ x: px, y: 0, percent: 0 })), x);
  }

  #tip(x: number, y: number, point: HistoryPoint) {
    const left = Math.min(Math.max(x - TIP_W / 2, 0), this._width - TIP_W);
    const top = Math.max(y - 56, 0);
    return svg`<g class="tip" transform=${`translate(${left},${top})`}>
      <rect width=${TIP_W} height="44" rx="6"></rect>
      <text x="10" y="18">${t('serverOps.storage.chart.tipPercent', { percent: formatNumber(point.percent, { maximumFractionDigits: 1 }) })}</text>
      <text class="muted" x="10" y="35">${formatDate(point.day)} · ${formatBytes(point.usedMb * MB)}</text>
    </g>`;
  }

  override render() {
    const box = this.#box;
    const geo = chartGeometry(
      this.points.map((p) => p.percent),
      box,
    );
    if (geo.points.length === 0) return nothing;
    const last = geo.points[geo.points.length - 1]!;
    const hover = this._hover >= 0 ? geo.points[this._hover] : undefined;
    const peak = Math.max(...this.points.map((p) => p.percent));
    const label = t('serverOps.storage.chart.aria', { days: this.points.length, peak: formatNumber(peak, { maximumFractionDigits: 0 }) });
    const first = this.points[0]!;
    const end = this.points[this.points.length - 1]!;
    return html`<svg
      viewBox="0 0 ${box.width} ${HEIGHT}"
      role="img"
      aria-label=${label}
      @pointermove=${(e: PointerEvent) => this.#onMove(
        e,
        geo.points.map((p) => p.x),
      )}
      @pointerleave=${() => (this._hover = -1)}
    >
      ${GRID.map((g) => svg`<line class="grid" x1=${box.left} x2=${box.width - box.right} y1=${geo.yOf(g)} y2=${geo.yOf(g)}></line><text class="axis" x=${box.left - 8} y=${geo.yOf(g) + 4} text-anchor="end">${g} %</text>`)}
      ${this.warn > 0
        ? svg`<line class="warn" x1=${box.left} x2=${box.width - box.right} y1=${geo.yOf(this.warn)} y2=${geo.yOf(this.warn)}></line><text class="warn-label" x=${box.width - box.right} y=${geo.yOf(this.warn) - 5} text-anchor="end">${t('serverOps.storage.chart.warn', { percent: this.warn })}</text>`
        : nothing}
      <path class="area" d=${geo.area}></path>
      <path class="line" d=${geo.line}></path>
      <circle class="dot" cx=${last.x} cy=${last.y} r="3.5"></circle>
      <text class="axis" x=${box.left} y=${HEIGHT - 6}>${formatDate(first.day)}</text>
      <text class="axis" x=${box.width - box.right} y=${HEIGHT - 6} text-anchor="end">${formatDate(end.day)}</text>
      ${hover
        ? svg`<line class="guide" x1=${hover.x} x2=${hover.x} y1=${box.top} y2=${geo.baseY}></line><circle class="dot" cx=${hover.x} cy=${hover.y} r="4.5"></circle>${this.#tip(hover.x, hover.y, this.points[this._hover]!)}`
        : nothing}
    </svg>`;
  }
}
customElements.define('lcm-ops-storage-chart', OpsStorageChart);
