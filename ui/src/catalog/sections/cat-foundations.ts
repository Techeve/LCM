import { LitElement, css, html } from 'lit';
import '../../components/lcm-ampel.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';

const GROUPS: [string, string[]][] = [
  ['Flächenstufen', ['bg', 'surface-1', 'surface-2', 'surface-3', 'surface-overlay']],
  ['Text und Akzent', ['text-1', 'text-2', 'text-3', 'line-strong', 'accent', 'accent-solid', 'accent-soft']],
  ['Status', ['great', 'ok', 'warn', 'crit', 'info', 'unknown', 'running']],
];

export class CatFoundations extends LitElement {
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .shape {
        display: grid;
        place-items: center;
        min-block-size: 5rem;
        background: var(--surface-1);
        font-size: var(--fs-xs);
        color: var(--text-2);
      }
      .r-s {
        border-radius: var(--radius-s);
      }
      .r-m {
        border-radius: var(--radius-m);
      }
      .r-l {
        border-radius: var(--radius-l);
      }
      .e1 {
        box-shadow: var(--ring-card), var(--elev-1);
      }
      .e2 {
        box-shadow: var(--elev-2);
      }
      .e3 {
        box-shadow: var(--elev-3);
      }
      .type > * {
        margin: 0;
      }
      .t-xl {
        font-size: var(--fs-xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      .t-l {
        font-size: var(--fs-l);
        font-weight: var(--head-weight);
      }
      .t-m {
        font-size: var(--fs-m);
      }
      .t-s {
        font-size: var(--fs-s);
        color: var(--text-2);
      }
      .t-label {
        font-family: var(--label-font);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
        color: var(--text-2);
      }
      .t-data {
        font-family: var(--font-mono);
        font-size: var(--fs-s);
      }
      .ampel-row {
        display: flex;
        gap: var(--space-5);
        align-items: center;
      }
    `,
  ];

  override render() {
    return html`<section>
      <h2>Grundlagen</h2>
      <p class="lead">Alles, was die Richtungen unterscheidet, steckt in Design-Tokens (CSS Custom Properties). Die Komponenten lesen nur diese Tokens, hell und dunkel entstehen allein über <span class="mono">light-dark()</span>.</p>
      ${GROUPS.map(
        ([title, names]) => html`<h3 class="sub">${title}</h3>
          <div class="grid cols-auto">${names.map((n) => html`<div class=${`swatch sw-${n}`}><span>${n}</span></div>`)}</div>`,
      )}
      <h3 class="sub">Schrift, Radien, Erhebung</h3>
      <div class="grid cols-3">
        <div class="stack type">
          <p class="t-xl">Überschrift mit Haltung</p>
          <p class="t-l">Zwischenüberschrift</p>
          <p class="t-m">Fließtext: 27 Server warten auf Sicherheitsupdates.</p>
          <p class="t-s">Hilfetext und Nebeninformation</p>
          <p class="t-label">Beschriftung</p>
          <p class="t-data">6.1.0-28-amd64 · web03.prod.techeve.de</p>
        </div>
        <div class="grid cols-3">
          <div class="shape r-s">Radius S</div>
          <div class="shape r-m">Radius M</div>
          <div class="shape r-l">Radius L</div>
          <div class="shape r-m e1">Stufe 1</div>
          <div class="shape r-m e2">Stufe 2</div>
          <div class="shape r-m e3">Stufe 3</div>
        </div>
        <div class="stack">
          <div class="ampel-row">
            ${(['great', 'ok', 'warn', 'crit', 'unknown', 'running'] as const).map((l) => html`<lcm-ampel level=${l} size="xl" label=${l}></lcm-ampel>`)}
          </div>
          <p class="small muted">Die Ampel in der Größe XL: Punkt im Ring. Form und Farbe tragen die Stufe, nie nur die Farbe.</p>
        </div>
      </div>
    </section>`;
  }
}
customElements.define('cat-foundations', CatFoundations);
