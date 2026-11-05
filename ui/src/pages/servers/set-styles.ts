import { css } from 'lit';
import { utilStyles } from '../../components/shared';

/** Gemeinsame Stile der Abschnitte im Einstellungsfenster und der Paket-Fenster. */
export const settingsStyles = css`
  ${utilStyles}
  :host {
    display: block;
  }
  .panel {
    padding-block-start: var(--space-4);
  }
  h3 {
    margin: var(--space-4) 0 0;
    color: var(--text-2);
    font-size: var(--fs-xs);
    font-weight: var(--fw-semibold);
    letter-spacing: var(--label-spacing);
    text-transform: var(--label-transform);
  }
  h3:first-child {
    margin-block-start: 0;
  }
  p {
    margin: 0;
  }
  .note {
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .warn {
    color: var(--warn-text);
  }
  .suggest {
    margin-block-start: calc(var(--space-3) * -1);
  }
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--space-3);
  }
  .row.center {
    align-items: center;
  }
  .slots {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3) var(--space-4);
  }
  /* Festes Raster aus zwei Feldspalten (Nameserver); schmal untereinander. */
  .grid-2 {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 18rem));
    gap: var(--space-3) var(--space-4);
    align-items: start;
  }
  @media (max-width: 36rem) {
    .grid-2 {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  .presets {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .presets .label {
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .kv {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: var(--space-2) var(--space-5);
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
  .mono {
    overflow-wrap: anywhere;
  }
  pre {
    max-block-size: 14rem;
    margin: var(--space-2) 0 0;
    padding: var(--space-2) var(--space-3);
    overflow: auto;
    border-radius: var(--radius-s);
    background: var(--surface-1);
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    line-height: 1.55;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
`;
