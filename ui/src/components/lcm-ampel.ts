import { LitElement, css, html, svg } from 'lit';
import { baseStyles, levelStyles, type StatusLevel } from './shared.js';

/**
 * Die LCM-Ampel: ein Punkt im Ring. Die Stufe steht in Farbe UND Form, nie nur in Farbe.
 *   hervorragend: Punkt, voller Ring und äußerer Hof, Häkchen
 *   in Ordnung:   Punkt im ruhigen Ring
 *   Warnung:      Ring mit Lücke
 *   kritisch:     großer Punkt, kräftiger Ring, Ausrufezeichen
 *   unbekannt:    gestrichelter Ring, kleiner Mittelpunkt
 *   läuft:        kreisender Bogen, pulsierender Punkt
 */
export class LcmAmpel extends LitElement {
  static override properties = {
    level: { type: String, reflect: true },
    size: { type: String, reflect: true },
    label: { type: String },
  };
  static override styles = [
    baseStyles,
    levelStyles,
    css`
      :host {
        --size: 1rem;
        display: inline-flex;
        flex: none;
        inline-size: var(--size);
        block-size: var(--size);
        color: var(--c);
      }
      :host([size='s']) {
        --size: 0.75rem;
      }
      :host([size='l']) {
        --size: 1.5rem;
      }
      :host([size='xl']) {
        --size: 2.75rem;
      }
      svg {
        inline-size: 100%;
        block-size: 100%;
        overflow: visible;
      }
      circle,
      path {
        transform-box: fill-box;
        transform-origin: center;
      }
      .ring {
        fill: none;
        stroke: currentColor;
        stroke-width: 1.8;
      }
      .track {
        fill: none;
        stroke: currentColor;
        stroke-width: 1.8;
        opacity: 0.25;
      }
      .halo {
        fill: none;
        stroke: currentColor;
        stroke-width: 1;
        opacity: 0.35;
      }
      .core {
        fill: currentColor;
      }
      .glyph {
        fill: none;
        stroke: var(--surface-1);
        stroke-width: 2.2;
        stroke-linecap: round;
        stroke-linejoin: round;
      }
      :host(:not([size='l']):not([size='xl'])) .glyph {
        display: none;
      }
      .arc-static {
        transform: rotate(-125deg);
      }
      :host([level='ok']) .ring {
        opacity: 0.55;
      }
      :host([level='crit']) .ring {
        stroke-width: 2.4;
      }
      :host([level='running']) .arc {
        animation: spin var(--dur-spin) linear infinite;
      }
      :host([level='running']) .core {
        animation: pulse var(--dur-pulse) var(--ease-loop) infinite;
      }
      @keyframes spin {
        to {
          transform: rotate(360deg);
        }
      }
      @keyframes pulse {
        50% {
          transform: scale(0.78);
          opacity: 0.7;
        }
      }
    `,
  ];

  declare level: StatusLevel;
  declare size: 's' | 'm' | 'l' | 'xl';
  declare label: string;

  constructor() {
    super();
    this.level = 'unknown';
    this.size = 'm';
    this.label = '';
  }

  override updated(): void {
    if (this.label) {
      this.setAttribute('role', 'img');
      this.setAttribute('aria-label', this.label);
    } else {
      this.removeAttribute('role');
      this.removeAttribute('aria-label');
    }
  }

  private shape() {
    switch (this.level) {
      case 'great':
        return svg`<circle class="halo" cx="12" cy="12" r="11.4"/><circle class="ring" cx="12" cy="12" r="9"/>
          <circle class="core" cx="12" cy="12" r="5.6"/><path class="glyph" d="m9.4 12.2 1.9 1.9 3.4-3.8"/>`;
      case 'ok':
        return svg`<circle class="ring" cx="12" cy="12" r="9"/><circle class="core" cx="12" cy="12" r="5.2"/>`;
      case 'warn':
        return svg`<circle class="track" cx="12" cy="12" r="9"/>
          <circle class="ring arc-static" cx="12" cy="12" r="9" pathLength="100" stroke-dasharray="72 28" stroke-linecap="round"/>
          <circle class="core" cx="12" cy="12" r="5.2"/>`;
      case 'crit':
        return svg`<circle class="ring" cx="12" cy="12" r="9.2"/><circle class="core" cx="12" cy="12" r="6.6"/>
          <path class="glyph" d="M12 8.9v3.5M12 15.1v.01"/>`;
      case 'running':
        return svg`<circle class="track" cx="12" cy="12" r="9"/>
          <circle class="ring arc" cx="12" cy="12" r="9" pathLength="100" stroke-dasharray="30 70" stroke-linecap="round"/>
          <circle class="core" cx="12" cy="12" r="4.8"/>`;
      default:
        return svg`<circle class="ring" cx="12" cy="12" r="9" pathLength="100" stroke-dasharray="7 4.4" opacity="0.8"/>
          <circle class="core" cx="12" cy="12" r="2.2" opacity="0.7"/>`;
    }
  }

  override render() {
    return html`<svg viewBox="0 0 24 24" aria-hidden="true">${this.shape()}</svg>`;
  }
}
customElements.define('lcm-ampel', LcmAmpel);
