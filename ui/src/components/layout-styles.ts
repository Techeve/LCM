import { css } from 'lit';

/**
 * Seitenspalten für Formulare und Tabellen: links eine feste Inhaltsspalte, rechts (optional) die Erklärspalte.
 * Breiter wird nichts; was rechts frei bleibt, bleibt frei.
 *
 * - `.layout` – Raster; mit `.explain` daneben die `lcm-explainer`. Unter 75rem Fensterbreite rutscht die
 *   Erklärspalte über den Inhalt und nimmt die volle Breite.
 * - `.layout.wide` bzw. `.column.wide` – für Tabellen, die wirklich mehr Platz brauchen.
 * - `.column` – nur die Inhaltsspalte, für Seiten ohne Raster.
 *
 * Breiten: `--content-w` (Formulare, 48rem), `--content-wide-w` (breite Tabellen, 68rem). Ein Elternteil kann
 * beide überschreiben; sie erben in die Schatten-DOMs hinein.
 */
export const columnStyles = css`
  .layout {
    display: grid;
    grid-template-columns: minmax(0, var(--content-w, 48rem));
    gap: var(--gap);
    align-items: start;
    justify-content: start;
  }
  .layout.explain {
    grid-template-columns: minmax(0, var(--content-w, 48rem)) auto;
  }
  .layout.wide {
    grid-template-columns: minmax(0, var(--content-wide-w, 68rem));
  }
  .layout.wide.explain {
    grid-template-columns: minmax(0, var(--content-wide-w, 68rem)) auto;
  }
  .column {
    max-inline-size: var(--content-w, 48rem);
    min-inline-size: 0;
  }
  .column.wide {
    max-inline-size: var(--content-wide-w, 68rem);
  }
  lcm-explainer p {
    margin: 0 0 var(--space-3);
  }
  lcm-explainer ul {
    margin: 0 0 var(--space-3);
    padding-inline-start: 1.1rem;
  }
  lcm-explainer li {
    margin-block-end: var(--space-2);
  }
  @media (max-width: 75rem) {
    .layout.explain {
      grid-template-columns: minmax(0, var(--content-w, 48rem));
    }
    .layout.wide.explain {
      grid-template-columns: minmax(0, var(--content-wide-w, 68rem));
    }
    .layout.explain lcm-explainer {
      grid-row: 1;
      inline-size: auto;
      position: static;
    }
  }
`;

/**
 * Formularaufbau: Felder untereinander (`.form-stack`), kurze zusammengehörige Felder nebeneinander
 * (`.field-row`, etwa Host und Port). Die Breite jedes Felds kommt aus seinem `width`.
 */
export const formLayoutStyles = css`
  .form-stack {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    min-inline-size: 0;
  }
  .field-row {
    display: flex;
    flex-wrap: wrap;
    align-items: start;
    gap: var(--space-4);
    min-inline-size: 0;
  }
  .field-row > * {
    flex: none;
    max-inline-size: 100%;
  }
`;
