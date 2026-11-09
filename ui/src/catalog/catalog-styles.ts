import { css, unsafeCSS } from 'lit';

const swatches = [
  'bg',
  'surface-1',
  'surface-2',
  'surface-3',
  'surface-overlay',
  'accent',
  'accent-solid',
  'accent-soft',
  'great',
  'ok',
  'warn',
  'crit',
  'info',
  'unknown',
  'running',
  'text-1',
  'text-2',
  'text-3',
  'line-strong',
];

const swatchCss = unsafeCSS(swatches.map((name) => `.sw-${name}{background:var(--${name})}`).join('\n'));

/** Layout-Hilfen der Katalogseiten (nur der Katalog, keine Produktkomponente). */
export const catalogStyles = css`
  :host {
    display: block;
    scroll-margin-block-start: 9.5rem;
  }
  section {
    padding-block: var(--space-6);
  }
  h2 {
    margin: 0 0 var(--space-1);
    font-size: var(--fs-2xl);
    font-weight: var(--head-weight);
    letter-spacing: var(--head-tracking);
  }
  .lead {
    max-inline-size: 52rem;
    margin: 0 0 var(--space-5);
    color: var(--text-2);
  }
  h3.sub {
    margin: var(--space-6) 0 var(--space-3);
    color: var(--text-2);
    font-family: var(--label-font);
    font-size: var(--fs-xs);
    font-weight: var(--fw-semibold);
    letter-spacing: var(--label-spacing);
    text-transform: var(--label-transform);
  }
  .grid {
    display: grid;
    gap: var(--gap);
  }
  .cols-2 {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .cols-3 {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
  .cols-4 {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
  .cols-5 {
    grid-template-columns: repeat(5, minmax(0, 1fr));
  }
  .cols-auto {
    grid-template-columns: repeat(auto-fill, minmax(18rem, 1fr));
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .frame {
    overflow: hidden;
    border-radius: var(--radius-l);
    background: var(--bg);
    background-image: var(--bg-wash);
    box-shadow:
      0 0 0 1px var(--line),
      var(--elev-2);
  }
  .frame.narrow {
    max-inline-size: 26rem;
  }
  .page {
    padding: var(--page-pad);
  }
  .muted {
    color: var(--text-2);
  }
  .small {
    font-size: var(--fs-s);
  }
  .mono {
    font-family: var(--font-mono);
  }
  p {
    margin: 0;
  }
  ul.plain {
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .swatch {
    display: flex;
    align-items: flex-end;
    min-block-size: 4.5rem;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-m);
    box-shadow: inset 0 0 0 1px var(--line);
  }
  .swatch span {
    padding: 0.1rem 0.45rem;
    border-radius: var(--radius-s);
    background: var(--surface-overlay);
    color: var(--text-1);
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
  }
  ${swatchCss}
`;
