import { css } from 'lit';

/** Abschnitte in den Fenster (Schlüssel, Zugang, Entfernen) und gemeinsame Zeilen. */
export const sectionStyles = css`
  :host {
    display: block;
  }
  section {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  h3 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0;
    font-size: var(--fs-m);
    font-weight: var(--fw-semibold);
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-2) var(--space-3);
  }
  /* Eingaben nebeneinander, der Knopf am Ende der Zeile. */
  .inline-form {
    display: grid;
    grid-template-columns: minmax(0, 13rem) minmax(0, 1fr) auto;
    gap: var(--space-3);
    align-items: end;
  }
  @media (max-width: 40rem) {
    .inline-form {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  h3 + p,
  .head + p,
  .note {
    margin: 0;
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .rows {
    display: flex;
    flex-direction: column;
    margin: 0;
    padding: 0;
    list-style: none;
    border: 1px solid var(--line);
    border-radius: var(--radius-m);
  }
  .rows > li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2) var(--space-3);
    padding: var(--space-2) var(--space-3);
  }
  .rows > li + li {
    border-block-start: 1px solid var(--line);
  }
  .rows .grow {
    flex: 1;
    min-inline-size: 0;
  }
  .rows .name {
    font-weight: var(--fw-semibold);
  }
  .rows .mono {
    display: block;
    overflow: hidden;
    color: var(--text-2);
    font-family: var(--font-mono);
    font-size: var(--fs-xs);
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .empty {
    padding: var(--space-3);
    border: 1px dashed var(--line-strong);
    border-radius: var(--radius-m);
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
`;
