import { css } from 'lit';
import { utilStyles } from '../../components/shared';

/** Gemeinsame Stile der Sicherheitsbausteine im Serverdetail: Abschnitte, Auswahlchips, Hinweise. */
export const secStyles = css`
  ${utilStyles}
  .section {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .section p {
    margin: 0;
    max-inline-size: 72ch;
  }
  .section + .section {
    margin-block-start: var(--space-5);
  }
  h3 {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-weight: var(--fw-semibold);
    letter-spacing: var(--label-spacing);
    text-transform: var(--label-transform);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
  }
  .row.end {
    align-items: end;
  }
  .lead {
    margin: 0 0 var(--space-4);
    color: var(--text-2);
  }
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .chip {
    padding: 0.25rem 0.75rem;
    border: 0;
    border-radius: var(--radius-pill);
    background: var(--surface-1);
    box-shadow: inset 0 0 0 1px var(--line-strong);
    color: var(--text-1);
    font: inherit;
    font-size: var(--fs-s);
    cursor: pointer;
  }
  .chip:hover {
    background: var(--surface-3);
  }
  .chip:focus-visible {
    outline: 2px solid var(--focus-ring);
    outline-offset: 2px;
  }
  .chip[aria-pressed='true'] {
    background: var(--accent-soft);
    box-shadow: none;
    color: var(--accent-text);
    font-weight: var(--fw-semibold);
  }
  .chip:disabled {
    cursor: default;
    opacity: 0.55;
  }
  .box {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    border-radius: var(--radius-m);
    background: var(--surface-2);
  }
`;
