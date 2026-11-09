import { LitElement, css, html, nothing } from 'lit';
import './lcm-ampel.js';
import { baseStyles, type StatusLevel } from './shared.js';

export interface JobStep {
  label: string;
  state: 'done' | 'active' | 'pending' | 'failed';
}

const LEVEL_BY_STATE: Record<string, StatusLevel> = { queued: 'unknown', running: 'running', ok: 'great', failed: 'crit' };

/**
 * <lcm-job-progress state="running" percent="62" label="…" detail="…" state-label="Läuft"> – Jobfortschritt.
 * `indeterminate` zeigt einen laufenden Balken, wenn kein Prozentwert bekannt ist. `steps` listet die Etappen.
 */
export class LcmJobProgress extends LitElement {
  static override properties = {
    state: { type: String, reflect: true },
    percent: { type: Number },
    indeterminate: { type: Boolean },
    label: { type: String },
    detail: { type: String },
    stateLabel: { type: String, attribute: 'state-label' },
    meta: { type: String },
    steps: { attribute: false },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
        --c: var(--running);
        --c-text: var(--running-text);
      }
      :host([state='ok']) {
        --c: var(--great);
        --c-text: var(--great-text);
      }
      :host([state='failed']) {
        --c: var(--crit);
        --c-text: var(--crit-text);
      }
      :host([state='queued']) {
        --c: var(--unknown);
        --c-text: var(--unknown-text);
      }
      .head {
        display: flex;
        align-items: center;
        gap: var(--space-3);
        margin-block-end: var(--space-2);
      }
      .title {
        flex: 1;
        min-inline-size: 0;
        font-weight: var(--fw-semibold);
      }
      .state {
        color: var(--c-text);
        font-size: var(--fs-s);
        font-weight: var(--fw-semibold);
      }
      .meta {
        color: var(--text-2);
        font-family: var(--font-mono);
        font-size: var(--fs-s);
        font-variant-numeric: tabular-nums;
      }
      .track {
        position: relative;
        block-size: 0.5rem;
        border-radius: var(--radius-pill);
        background: var(--surface-3);
        overflow: hidden;
      }
      svg {
        display: block;
        inline-size: 100%;
        block-size: 100%;
      }
      .fill {
        fill: var(--c);
      }
      .moving {
        position: absolute;
        inset-block: 0;
        inline-size: 34%;
        border-radius: var(--radius-pill);
        background: var(--c);
        animation: slide var(--dur-pulse) var(--ease-loop) infinite;
      }
      /* Prozente des eigenen Balkens: von ganz links draußen bis ganz rechts draußen (100 % / 34 %). */
      @keyframes slide {
        from {
          transform: translateX(-100%);
        }
        to {
          transform: translateX(294%);
        }
      }
      /* Ohne Bewegung wäre der Balken ein fester Füllstand: stattdessen voll und blass, „läuft, Stand unbekannt“. */
      @media (prefers-reduced-motion: reduce) {
        .moving {
          inline-size: 100%;
          opacity: 0.4;
        }
      }
      .detail {
        margin: var(--space-2) 0 0;
        color: var(--text-2);
        font-size: var(--fs-s);
      }
      ol {
        display: flex;
        flex-wrap: wrap;
        gap: var(--space-2) var(--space-4);
        margin: var(--space-3) 0 0;
        padding: 0;
        list-style: none;
        font-size: var(--fs-s);
      }
      li {
        display: inline-flex;
        align-items: center;
        gap: var(--space-2);
        color: var(--text-3);
      }
      li .dot {
        inline-size: 0.5rem;
        block-size: 0.5rem;
        border-radius: 50%;
        background: var(--surface-3);
        box-shadow: inset 0 0 0 1.5px var(--line-strong);
      }
      li.done {
        color: var(--text-2);
      }
      li.done .dot {
        background: var(--great);
        box-shadow: none;
      }
      li.active {
        color: var(--text-1);
        font-weight: var(--fw-semibold);
      }
      li.active .dot {
        background: var(--running);
        box-shadow: none;
      }
      li.failed {
        color: var(--crit-text);
        font-weight: var(--fw-semibold);
      }
      li.failed .dot {
        background: var(--crit);
        box-shadow: none;
      }
    `,
  ];

  declare state: 'queued' | 'running' | 'ok' | 'failed';
  declare percent: number;
  declare indeterminate: boolean;
  declare label: string;
  declare detail: string;
  declare stateLabel: string;
  declare meta: string;
  declare steps: JobStep[];

  constructor() {
    super();
    this.state = 'running';
    this.percent = 0;
    this.indeterminate = false;
    this.label = '';
    this.detail = '';
    this.stateLabel = '';
    this.meta = '';
    this.steps = [];
  }

  private bar() {
    const value = Math.min(Math.max(this.percent, 0), 100);
    if (this.indeterminate && this.state === 'running') {
      return html`<div class="track" role="progressbar" aria-label=${this.label}><span class="moving"></span></div>`;
    }
    return html`<div class="track" role="progressbar" aria-label=${this.label} aria-valuemin="0" aria-valuemax="100" aria-valuenow=${value}>
      <svg viewBox="0 0 100 1" preserveAspectRatio="none" aria-hidden="true"><rect class="fill" x="0" y="0" width=${value} height="1"></rect></svg>
    </div>`;
  }

  override render() {
    const done = this.state === 'ok' ? 100 : this.percent;
    return html`
      <div class="head">
        <lcm-ampel level=${LEVEL_BY_STATE[this.state] ?? 'unknown'} size="m"></lcm-ampel>
        <span class="title">${this.label}</span>
        <span class="state">${this.stateLabel}</span>
        <span class="meta">${this.indeterminate ? this.meta : new Intl.NumberFormat(document.documentElement.lang || undefined, { style: 'percent' }).format(done / 100)}</span>
      </div>
      ${this.bar()}
      ${this.detail ? html`<p class="detail">${this.detail}</p>` : nothing}
      ${this.steps.length
        ? html`<ol>${this.steps.map((s) => html`<li class=${s.state}><span class="dot"></span>${s.label}</li>`)}</ol>`
        : nothing}
    `;
  }
}
customElements.define('lcm-job-progress', LcmJobProgress);
