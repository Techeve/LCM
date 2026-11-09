import { css } from 'lit';

/** Monospace-Ausgabe, wie sie Jobs und SSH-Kommandos zeigen. */
export const logStyles = css`
  pre {
    margin: 0;
    padding: var(--space-3);
    border-radius: var(--radius-m);
    background: var(--surface-2);
    color: var(--text-1);
    font-family: var(--font-mono);
    font-size: var(--fs-s);
    line-height: 1.55;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    overflow: auto;
    max-block-size: 22rem;
    tab-size: 4;
  }
  pre.empty {
    color: var(--text-2);
    font-family: var(--font-sans);
  }
`;
