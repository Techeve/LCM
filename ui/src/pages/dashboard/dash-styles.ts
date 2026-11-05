import { css } from 'lit';

/** Stile des Dashboards: Kopf, Kacheln, die vier Spalten und ihre Listen. */
export const dashStyles = css`
  :host {
    display: block;
    container-type: inline-size;
  }
  .head {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: var(--space-4);
    margin-block-end: var(--space-5);
  }
  h1 {
    margin: 0;
    font-size: var(--fs-2xl);
    font-weight: var(--head-weight);
    letter-spacing: var(--head-tracking);
    line-height: 1.15;
  }
  .live {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: var(--space-1) 0 0;
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .tiles {
    display: grid;
    grid-template-columns: repeat(5, minmax(0, 1fr));
    gap: var(--gap);
    margin-block-end: var(--gap);
  }
  .columns {
    display: grid;
    grid-template-columns: 1.3fr 1.1fr 1fr 1fr;
    gap: var(--gap);
    align-items: stretch;
  }
  .columns.no-alerts {
    grid-template-columns: 1.3fr 1.1fr 1fr;
  }
  .columns > * {
    min-inline-size: 0;
  }
  @container (max-width: 75rem) {
    .tiles {
      grid-template-columns: repeat(3, minmax(0, 1fr));
    }
    .columns,
    .columns.no-alerts {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @container (max-width: 44rem) {
    .tiles {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .columns,
    .columns.no-alerts {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @container (max-width: 28rem) {
    .tiles {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  ul {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: grid;
    grid-template-columns: auto minmax(0, 1fr) auto;
    gap: var(--space-1) var(--space-3);
    align-items: start;
    padding-block: var(--space-3);
    border-block-start: 1px solid var(--line);
  }
  li:first-child {
    padding-block-start: 0;
    border-block-start: 0;
  }
  li:last-child {
    padding-block-end: 0;
  }
  li lcm-ampel {
    margin-block-start: 0.2rem;
  }
  .upcoming li,
  .two li {
    grid-template-columns: auto minmax(0, 1fr);
  }
  .title {
    font-weight: var(--fw-semibold);
    overflow-wrap: break-word;
  }
  .title a {
    color: var(--text-1);
    text-decoration: none;
  }
  .title a.srv {
    color: var(--accent-text);
  }
  .title a:hover {
    text-decoration: underline;
  }
  .sub {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-s);
    overflow-wrap: break-word;
  }
  .meta {
    color: var(--text-2);
    font-family: var(--font-mono);
    font-size: var(--fs-s);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .srv {
    color: var(--accent-text);
    font-family: var(--font-mono);
    font-weight: var(--fw-semibold);
    text-decoration: none;
  }
  a.srv:hover {
    text-decoration: underline;
  }
  .act {
    grid-column: 2 / -1;
    justify-self: start;
    margin-block-start: var(--space-1);
  }
  .when {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    min-inline-size: 4.75rem;
    font-family: var(--font-mono);
    font-size: var(--fs-s);
    font-variant-numeric: tabular-nums;
  }
  .when strong {
    font-size: var(--fs-m);
  }
  .when span {
    color: var(--text-2);
  }
  lcm-empty-state {
    padding: var(--space-5) var(--space-2);
  }
  .error {
    margin-block-end: var(--gap);
  }
  .tile-skeleton {
    block-size: 7.5rem;
    border-radius: var(--radius-l);
    background: var(--surface-1);
    box-shadow: var(--ring-card);
    padding: var(--space-card);
  }
`;
