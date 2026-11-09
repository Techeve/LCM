import { css } from 'lit';

/** Lesetypografie der Doku: begrenzte Zeilenlänge, ruhige Abstände, Code und Tabellen im LCM-Look. */
export const docsStyles = css`
  article.doc {
    min-inline-size: 0;
    color: var(--text-1);
    font-size: var(--fs-m);
    line-height: 1.7;
  }
  .doc h1 {
    margin: 0 0 var(--space-4);
    font-size: var(--fs-3xl);
    font-weight: var(--head-weight);
    letter-spacing: var(--head-tracking);
    line-height: 1.2;
  }
  .doc h2 {
    margin: var(--space-7) 0 var(--space-3);
    padding-block-end: var(--space-2);
    border-block-end: 1px solid var(--line);
    font-size: var(--fs-xl);
    font-weight: var(--head-weight);
    letter-spacing: var(--head-tracking);
    line-height: 1.3;
  }
  .doc h3 {
    margin: var(--space-5) 0 var(--space-2);
    font-size: var(--fs-l);
    font-weight: var(--fw-semibold);
  }
  .doc h4 {
    margin: var(--space-4) 0 var(--space-2);
    font-size: var(--fs-m);
    font-weight: var(--fw-semibold);
  }
  .doc h2,
  .doc h3 {
    scroll-margin-block-start: var(--space-5);
  }
  .doc :is(h1, h2, h3, h4):focus {
    outline: none;
  }
  .doc .anchor {
    margin-inline-start: var(--space-2);
    color: var(--text-3);
    font-weight: var(--fw-regular);
    text-decoration: none;
    opacity: 0;
    transition: opacity var(--dur-1) var(--ease);
  }
  .doc :is(h2, h3):hover .anchor,
  .doc .anchor:focus-visible {
    opacity: 1;
  }
  .doc p,
  .doc ul,
  .doc ol {
    margin: 0 0 var(--space-3);
  }
  .doc ul,
  .doc ol {
    padding-inline-start: 1.4rem;
  }
  .doc li {
    margin-block-end: var(--space-1);
  }
  .doc li > ul,
  .doc li > ol {
    margin-block: var(--space-1) 0;
  }
  .doc a {
    color: var(--accent-text);
    text-underline-offset: 0.15em;
  }
  .doc code {
    padding: 0.1em 0.35em;
    border-radius: var(--radius-s);
    background: var(--surface-2);
    font-family: var(--font-mono);
    font-size: 0.88em;
  }
  .doc pre {
    margin: 0 0 var(--space-4);
    padding: var(--space-3) var(--space-4);
    overflow-x: auto;
    border-radius: var(--radius-m);
    background: var(--surface-2);
    box-shadow: inset 0 0 0 1px var(--line);
    line-height: 1.55;
  }
  .doc pre code {
    padding: 0;
    background: none;
    font-size: var(--fs-s);
  }
  .doc blockquote {
    margin: 0 0 var(--space-4);
    padding: var(--space-3) var(--space-4);
    border-inline-start: 3px solid var(--accent);
    border-radius: 0 var(--radius-m) var(--radius-m) 0;
    background: var(--accent-soft);
    color: var(--text-1);
  }
  .doc hr {
    margin: var(--space-6) 0;
    border: 0;
    border-block-start: 1px solid var(--line);
  }
  .doc .table {
    margin: 0 0 var(--space-4);
    overflow-x: auto;
  }
  .doc table {
    inline-size: 100%;
    border-collapse: collapse;
    font-size: var(--fs-s);
  }
  .doc th,
  .doc td {
    padding: var(--space-2) var(--space-3);
    border-block-end: 1px solid var(--line);
    text-align: start;
    vertical-align: top;
  }
  .doc th {
    background: var(--table-head-bg);
    font-weight: var(--fw-semibold);
  }
`;
