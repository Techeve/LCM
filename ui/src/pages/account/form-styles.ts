import { css } from 'lit';
import { utilStyles } from '../../components/shared';

/** Gemeinsame Stile der Konto-, Aktivierungs- und Assistentenformulare: Stapel, Knopfzeile. */
export const formStyles = css`
  ${utilStyles}
  .stack {
    display: grid;
    gap: var(--space-4);
  }
  /* Kurze, zusammengehörige Felder nebeneinander (etwa Host und Port); die Breite kommt aus \`width\` der Felder. */
  .row,
  .pair {
    display: flex;
    flex-wrap: wrap;
    align-items: start;
    gap: var(--space-4);
  }
  .row > *,
  .pair > * {
    flex: none;
    max-inline-size: 100%;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    align-items: center;
  }
  a {
    color: var(--accent-text);
  }
  .lead {
    margin: 0;
    color: var(--text-2);
    line-height: 1.55;
  }
  pre {
    margin: 0;
    padding: var(--space-3);
    overflow-x: auto;
    border-radius: var(--radius-m);
    background: var(--surface-1);
    white-space: pre-wrap;
    word-break: break-all;
  }
  h2 {
    margin: 0;
    font-size: var(--fs-l);
    font-weight: var(--head-weight);
    letter-spacing: var(--head-tracking);
  }
  h3 {
    margin: 0;
    font-size: var(--fs-m);
    font-weight: var(--fw-semibold);
  }
`;
