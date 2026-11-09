import { css } from 'lit';
import { columnStyles, formLayoutStyles } from '../../../components/layout-styles';
import { utilStyles } from '../../../components/shared';

/** Gemeinsame Stile der Einstellungsseiten: Spalten, Kartenstapel, Feldaufbau, Kopier-Zeilen, Tabellen. */
export const settingsStyles = [
  columnStyles,
  formLayoutStyles,
  utilStyles,
  css`
  :host {
    display: block;
    min-inline-size: 0;
  }
  .stack {
    display: flex;
    flex-direction: column;
    gap: var(--gap);
    min-inline-size: 0;
  }
  .fields {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(13rem, 1fr));
    gap: var(--space-4);
    align-items: start;
  }
  .grid.wide {
    grid-template-columns: repeat(auto-fit, minmax(20rem, 1fr));
  }
  .switches {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .row.end {
    justify-content: flex-end;
  }
  .grow {
    flex: 1 1 18rem;
  }
  .spaced-top {
    margin-block-start: var(--space-4);
  }
  .spaced-bottom {
    margin-block-end: var(--space-4);
  }
  .muted {
    font-size: var(--fs-s);
  }
  .mono {
    font-family: var(--font-mono);
    font-size: var(--fs-s);
    overflow-wrap: anywhere;
  }
  dl.facts {
    display: grid;
    grid-template-columns: minmax(8rem, 14rem) minmax(0, 1fr);
    gap: var(--space-2) var(--space-4);
    margin: 0;
    font-size: var(--fs-s);
  }
  dl.facts dt {
    color: var(--text-2);
  }
  dl.facts dd {
    margin: 0;
    color: var(--text-1);
    font-weight: var(--fw-medium);
    overflow-wrap: anywhere;
  }
  dl.facts dd.warn {
    color: var(--warn-text);
  }
  dl.facts dd.crit {
    color: var(--crit-text);
  }
  .skeletons {
    max-inline-size: var(--content-w, 48rem);
    display: flex;
    flex-direction: column;
    gap: var(--gap);
  }
  code {
    font-family: var(--font-mono);
    font-size: 0.9em;
  }
`,
];
