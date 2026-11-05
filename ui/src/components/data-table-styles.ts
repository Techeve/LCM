import { css } from 'lit';

/** Stile von <lcm-data-table>: Werkzeugleiste, Kopf mit Sortieren und Filtern, Zeilen, Zellarten, Leerzustand. */
export const dataTableStyles = css`
  :host {
    display: block;
    min-inline-size: 0;
  }
  :host([density='compact']) {
    --row-h: 2.25rem;
  }
  :host([density='comfortable']) {
    --row-h: 3.25rem;
  }
  .toolbar,
  .bulk {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    min-block-size: calc(var(--control-h) + var(--space-2));
    margin-block-end: var(--space-3);
  }
  .toolbar .grow {
    flex: 1;
  }
  .bulk {
    padding: var(--space-1) var(--space-2) var(--space-1) var(--space-4);
    border-radius: var(--radius-m);
    background: var(--accent-soft);
    color: var(--accent-text);
    font-weight: var(--fw-semibold);
    animation: bulk-in var(--dur-2) var(--ease);
  }
  .bulk .count {
    margin-inline-end: auto;
  }
  .tool-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-block-size: calc(var(--control-h) - 0.25rem);
    padding-inline: var(--space-3);
    border: 0;
    border-radius: var(--radius-m);
    background: var(--surface-2);
    color: var(--text-1);
    font-size: var(--fs-s);
    font-weight: var(--fw-medium);
    cursor: pointer;
  }
  .tool-btn:hover,
  .tool-btn[aria-expanded='true'] {
    background: var(--surface-3);
  }
  .seg {
    display: inline-flex;
    padding: 3px;
    border-radius: var(--radius-m);
    background: var(--surface-2);
  }
  .seg button {
    min-block-size: calc(var(--control-h) - 0.75rem);
    padding-inline: var(--space-3);
    border: 0;
    border-radius: var(--radius-s);
    background: transparent;
    color: var(--text-2);
    font-size: var(--fs-s);
    font-weight: var(--fw-medium);
    cursor: pointer;
  }
  .seg button[aria-pressed='true'] {
    background: var(--surface-1);
    color: var(--text-1);
    box-shadow: var(--elev-1);
  }
  .chooser {
    min-inline-size: 14rem;
    padding: var(--space-2);
    border: 1px solid transparent;
    border-radius: var(--radius-m);
    background: var(--surface-overlay);
    box-shadow: var(--elev-2);
  }
  .chooser label {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    min-block-size: calc(var(--control-h) - 0.25rem);
    padding-inline: var(--space-3);
    border-radius: var(--radius-s);
    cursor: pointer;
  }
  .chooser label:hover {
    background: var(--surface-2);
  }
  .scroll {
    overflow: auto;
    border: 1px solid var(--line-muted);
    border-radius: var(--radius-m);
    background: var(--surface-1);
    max-block-size: var(--table-max-h, none);
  }
  table {
    inline-size: 100%;
    border-collapse: separate;
    border-spacing: 0;
    font-size: var(--fs-m);
  }
  th,
  td {
    padding-inline: var(--space-3);
    text-align: start;
    white-space: nowrap;
  }
  th:first-child,
  td:first-child {
    padding-inline-start: var(--space-4);
  }
  th:last-child,
  td:last-child {
    padding-inline-end: var(--space-4);
  }
  th.end,
  td.end {
    text-align: end;
  }
  thead th {
    position: sticky;
    inset-block-start: 0;
    z-index: var(--z-sticky);
    background-color: var(--surface-1);
    background-image: linear-gradient(var(--table-head-bg), var(--table-head-bg));
    border-block-end: 1px solid var(--line-muted);
    color: var(--text-2);
    font-family: var(--label-font);
    font-size: var(--fs-xs);
    font-weight: var(--fw-semibold);
    letter-spacing: var(--label-spacing);
    text-transform: var(--label-transform);
  }
  .labels th {
    block-size: calc(var(--control-h) - 0.25rem);
  }
  .filters td {
    position: sticky;
    inset-block-start: calc(var(--control-h) - 0.25rem);
    z-index: var(--z-raised);
    padding-block: 0 var(--space-2);
    background: var(--surface-1);
  }
  .sort-btn {
    display: inline-flex;
    align-items: center;
    gap: var(--space-1);
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
    cursor: pointer;
  }
  .sort-btn:hover {
    color: var(--text-1);
  }
  .sort-btn .icon {
    inline-size: 1.1em;
    block-size: 1.1em;
    opacity: 0.45;
  }
  th[aria-sort='ascending'] .sort-btn,
  th[aria-sort='descending'] .sort-btn {
    color: var(--accent-text);
  }
  th[aria-sort='ascending'] .icon,
  th[aria-sort='descending'] .icon {
    opacity: 1;
  }
  .filter-input {
    inline-size: 100%;
    min-inline-size: 5rem;
    min-block-size: 1.875rem;
    padding-inline: var(--space-2);
    border: 0;
    border-radius: var(--radius-s);
    background: var(--surface-2);
    color: var(--text-1);
    font: inherit;
    font-size: var(--fs-s);
    font-weight: var(--fw-regular);
    text-transform: none;
    letter-spacing: normal;
  }
  .filter-input::placeholder {
    color: var(--text-3);
  }
  .filter-input:focus-visible {
    outline-offset: 0;
  }
  tbody tr {
    block-size: var(--row-h);
    cursor: default;
    transition:
      background var(--dur-1) var(--ease),
      box-shadow var(--dur-1) var(--ease);
  }
  tbody td {
    /* Luft über und unter zweizeiligen Zellen; einzeilige Zeilen bleiben bei --row-h. */
    padding-block: var(--space-2);
    border-block-start: calc(var(--row-sep) * 1px) solid var(--line-muted);
    color: var(--text-1);
    max-inline-size: 22rem;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  tbody tr:first-child td {
    border-block-start-color: transparent;
  }
  tbody tr:hover {
    background: var(--surface-2);
  }
  tbody tr[aria-selected='true'] {
    background: var(--accent-soft);
  }
  tbody tr[data-active='true'] {
    background: color-mix(in srgb, var(--accent-soft) 70%, var(--surface-1));
    box-shadow: inset 3px 0 0 var(--accent);
  }
  tbody tr:focus-visible {
    outline: 2px solid var(--focus-ring);
    outline-offset: -2px;
  }
  td.mono {
    font-family: var(--font-mono);
    font-size: 0.92em;
    font-variant-numeric: tabular-nums;
  }
  td.sel,
  th.sel {
    inline-size: 2.75rem;
  }
  input[type='checkbox'] {
    inline-size: 1.05rem;
    block-size: 1.05rem;
    margin: 0;
    accent-color: var(--accent);
    cursor: pointer;
    vertical-align: middle;
  }
  .empty {
    padding: var(--space-7) var(--space-4);
    color: var(--text-2);
    text-align: center;
  }
  @keyframes bulk-in {
    from {
      opacity: 0;
      transform: translateY(-0.25rem);
    }
  }
`;
