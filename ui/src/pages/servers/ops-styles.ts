import { css } from 'lit';

/** Hinweise und Zellen der Reiter Container, Benutzer, Deep-Scan und Speicher. */
export const opsStyles = css`
  .sub {
    display: block;
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .cell {
    display: inline-flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1) var(--space-2);
  }
`;
