import { LitElement, css, html, nothing } from 'lit';
import { baseStyles } from './shared.js';

/**
 * <lcm-skeleton shape="text" lines="3"> – Ladeplatzhalter. `shape`: text, block, circle, row.
 * `label` wird nur Screenreadern vorgelesen („Lädt …“); der Platzhalter selbst ist stumm.
 */
export class LcmSkeleton extends LitElement {
  static override properties = {
    shape: { type: String, reflect: true },
    lines: { type: Number },
    label: { type: String },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      .bone {
        display: block;
        border-radius: var(--radius-s);
        background: linear-gradient(90deg, var(--surface-2) 30%, var(--surface-3) 50%, var(--surface-2) 70%);
        background-size: 250% 100%;
        animation: shimmer var(--dur-pulse) var(--ease-loop) infinite;
      }
      .line {
        block-size: 0.85em;
        margin-block: 0.5em;
      }
      .line:last-child:not(:only-child) {
        inline-size: 62%;
      }
      :host([shape='block']) .bone {
        block-size: var(--skeleton-h, 7rem);
        border-radius: var(--radius-m);
      }
      :host([shape='circle']) .bone {
        inline-size: 2.5rem;
        block-size: 2.5rem;
        border-radius: 50%;
      }
      :host([shape='row']) .bone {
        block-size: var(--row-h);
        margin-block-end: 2px;
      }
      @keyframes shimmer {
        from {
          background-position: 100% 0;
        }
        to {
          background-position: -100% 0;
        }
      }
      @media (prefers-reduced-motion: reduce) {
        .bone {
          background: var(--surface-2);
        }
      }
    `,
  ];

  declare shape: 'text' | 'block' | 'circle' | 'row';
  declare lines: number;
  declare label: string;

  constructor() {
    super();
    this.shape = 'text';
    this.lines = 3;
    this.label = '';
  }

  override render() {
    const count = this.shape === 'text' || this.shape === 'row' ? this.lines : 1;
    const bones = Array.from({ length: count }, () => html`<span class=${this.shape === 'text' ? 'bone line' : 'bone'} aria-hidden="true"></span>`);
    return html`${this.label ? html`<span class="sr-only" role="status">${this.label}</span>` : nothing}${bones}`;
  }
}
customElements.define('lcm-skeleton', LcmSkeleton);
