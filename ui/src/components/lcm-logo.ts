import { LitElement, css, html } from 'lit';
import { baseStyles } from './shared.js';

/**
 * Das LCM-Zeichen als Inline-SVG; Farben folgen dem Theme (hell: Papierkachel, dunkel: Graphitkachel).
 * Die Farben stehen bewusst hier und nicht in den Tokens: Sie gehören zur festen Bildmarke, nicht zur Oberfläche,
 * und kein anderer Baustein verwendet sie.
 */
export class LcmLogo extends LitElement {
  static override properties = { label: { type: String } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
        inline-size: 2rem;
        block-size: 2rem;
        flex: none;
      }
      svg {
        inline-size: 100%;
        block-size: 100%;
      }
      .top {
        stop-color: light-dark(#ffffff, #1b212b);
      }
      .bottom {
        stop-color: light-dark(#e8edf1, #0a0c11);
      }
      .glow {
        stop-color: light-dark(#0e8aa6, #0dcaf0);
      }
      .edge {
        fill: none;
        stroke: light-dark(#cdd5de, rgb(13 202 240 / 30%));
      }
      .chevron {
        fill: none;
        stroke: light-dark(#0e8aa6, #0dcaf0);
        stroke-width: 46;
        stroke-linecap: round;
        stroke-linejoin: round;
      }
      .bar {
        fill: light-dark(#14b3d4, #7fe9f7);
      }
    `,
  ];

  declare label: string;

  constructor() {
    super();
    this.label = 'LCM';
  }

  override render() {
    return html`<svg viewBox="0 0 512 512" role="img" aria-label=${this.label}>
      <defs>
        <linearGradient id="tile" x1="0" y1="0" x2="0" y2="1">
          <stop class="top" offset="0" /><stop class="bottom" offset="1" />
        </linearGradient>
        <radialGradient id="glow" cx="0.44" cy="0.5" r="0.55">
          <stop class="glow" offset="0" stop-opacity="0.16" /><stop class="glow" offset="1" stop-opacity="0" />
        </radialGradient>
      </defs>
      <rect x="32" y="32" width="448" height="448" rx="116" fill="url(#tile)" />
      <rect x="32" y="32" width="448" height="448" rx="116" fill="url(#glow)" />
      <rect class="edge" x="32.5" y="32.5" width="447" height="447" rx="115.5" />
      <path class="chevron" d="M176 176 L288 256 L176 336" />
      <rect class="bar" x="246" y="316" width="126" height="40" rx="20" />
    </svg>`;
  }
}
customElements.define('lcm-logo', LcmLogo);
