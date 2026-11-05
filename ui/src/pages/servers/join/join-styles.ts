import { css } from 'lit';

/** Stile der Assistentenschritte: Fläche, Liste „Das macht LCM“, Ergebnisdaten. */
export const joinStyles = css`
  .panel {
    display: grid;
    gap: var(--space-5);
    padding: var(--space-card);
    border-radius: var(--radius-l);
    background: var(--surface-2);
    box-shadow: var(--ring-card), var(--elev-1);
  }
  .small {
    font-size: var(--fs-s);
  }
  .stack.tight {
    gap: var(--space-2);
  }
  ol.does {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding-inline-start: 1.3rem;
    line-height: 1.55;
  }
  dl.facts {
    display: grid;
    grid-template-columns: max-content minmax(0, 1fr);
    gap: var(--space-2) var(--space-5);
    margin: 0;
  }
  dl.facts dt {
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  dl.facts dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
`;
