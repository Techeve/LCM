import { css } from 'lit';

/**
 * Breite eines Formularfelds nach Inhalt (Attribut `width`). Begrenzt wird die Eingabefläche; Beschriftung,
 * Hinweis und Fehler nehmen die Breite des Elements und bremsen eine Zeile nebeneinander liegender Felder nicht.
 *
 * | Stufe  | Breite | wofür                                                    |
 * |--------|--------|----------------------------------------------------------|
 * | `xs`   | 7rem   | Port, kleine Zahlen (bis fünf Stellen)                   |
 * | `s`    | 11rem  | Zahlen mit Einheit, Minuten, Tage, kurze Kürzel          |
 * | `m`    | 18rem  | Benutzernamen, Hostnamen, Namen (Vorgabe)                |
 * | `l`    | 30rem  | E-Mail-Adressen, URLs, Pfade                             |
 * | `full` | 100 %  | Schlüssel, Tokens, lange Texte; Felder, deren Breite der Elternteil vorgibt |
 *
 * Von außen lässt sich die Breite zusätzlich über `--field-w` setzen.
 */
export type FieldWidth = 'xs' | 's' | 'm' | 'l' | 'full';

/** Eigenschaft `width` für die Formularbausteine (spiegelt ins Attribut, damit die Stile greifen). */
export const fieldWidthProperty = { type: String, reflect: true } as const;

/** Gemeinsame Stile der Formularbausteine: Beschriftung, Eingabefläche, Hinweis, Fehler, Breitenstufen. */
export const fieldStyles = css`
  :host {
    --field-w: 18rem;
    display: block;
    min-inline-size: 0;
  }
  :host([width='xs']) {
    --field-w: 7rem;
  }
  :host([width='s']) {
    --field-w: 11rem;
  }
  :host([width='m']) {
    --field-w: 18rem;
  }
  :host([width='l']) {
    --field-w: 30rem;
  }
  :host([width='full']) {
    --field-w: 100%;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  label,
  .label {
    color: var(--text-2);
    font-size: var(--fs-s);
    font-weight: var(--fw-regular);
  }
  .control {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    inline-size: var(--field-w);
    max-inline-size: 100%;
    min-block-size: var(--control-h);
    padding-inline: 0.6rem;
    border-radius: var(--radius-s);
    background: var(--surface-1);
    box-shadow: inset 0 0 0 1px var(--line);
    color: var(--text-1);
    transition: box-shadow var(--dur-1) var(--ease);
  }
  .control:hover {
    box-shadow: inset 0 0 0 1px var(--line-strong);
  }
  .control:focus-within {
    box-shadow: inset 0 0 0 2px var(--focus-ring);
  }
  :host([invalid]) .control {
    box-shadow: inset 0 0 0 2px var(--crit);
  }
  :host([disabled]) .control {
    opacity: 0.55;
    background: var(--surface-2);
  }
  input,
  select {
    flex: 1;
    min-inline-size: 0;
    min-block-size: calc(var(--control-h) - 0.25rem);
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    font-size: var(--fs-m);
    outline: none;
  }
  input:focus-visible,
  select:focus-visible {
    outline: none;
  }
  input.code {
    text-align: center;
    letter-spacing: 0.2em;
  }
  input::placeholder {
    color: var(--text-3);
  }
  .hint,
  .error {
    contain: inline-size;
  }
  .hint {
    color: var(--text-2);
    font-size: var(--fs-s);
  }
  .error {
    color: var(--crit-text);
    font-size: var(--fs-s);
    font-weight: var(--fw-medium);
  }
  .adorn {
    display: inline-flex;
    color: var(--text-3);
  }
  .icon-btn {
    display: grid;
    place-items: center;
    inline-size: 1.9rem;
    block-size: 1.9rem;
    margin-inline-end: -0.4rem;
    border: 0;
    border-radius: var(--radius-s);
    background: transparent;
    color: var(--text-2);
    cursor: pointer;
  }
  .icon-btn:hover {
    background: var(--surface-2);
    color: var(--text-1);
  }
`;
