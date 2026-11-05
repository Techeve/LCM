import { css } from 'lit';
import { utilStyles } from '../../components/shared';

/** Gemeinsame Stile der Reiter im Serverdetail. */
export const tabStyles = css`
  ${utilStyles}
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
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-3) var(--space-5);
    align-items: center;
    margin: 0;
  }
  .kv dt {
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .kv dd {
    margin: 0;
    min-inline-size: 0;
  }
  .big {
    font-size: var(--fs-xl);
    font-weight: var(--head-weight);
    font-variant-numeric: tabular-nums;
  }
  .num {
    font-variant-numeric: tabular-nums;
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  a {
    color: var(--accent-text);
    text-decoration: none;
  }
  a:hover {
    text-decoration: underline;
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
    margin-block-end: var(--space-3);
  }
  .toolbar .grow {
    flex: 1;
  }
`;
