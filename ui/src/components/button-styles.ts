import { css } from 'lit';

/** Stile für Knöpfe, geteilt von <lcm-button> und <lcm-server-action>. */
export const buttonStyles = css`
  .btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: var(--space-2);
    min-block-size: var(--btn-h, var(--control-h));
    padding-inline: var(--btn-px, 0.85rem);
    border: 1px solid var(--line);
    /* In einer Knopfleiste (groupStyles) setzt der Rahmen die inneren Ecken auf 0. */
    border-start-start-radius: var(--btn-r-start, var(--radius-s));
    border-end-start-radius: var(--btn-r-start, var(--radius-s));
    border-start-end-radius: var(--btn-r-end, var(--radius-s));
    border-end-end-radius: var(--btn-r-end, var(--radius-s));
    background: var(--surface-1);
    color: var(--text-1);
    font: inherit;
    font-size: var(--btn-fs, var(--fs-m));
    font-weight: var(--fw-regular);
    line-height: 1;
    white-space: nowrap;
    text-decoration: none;
    cursor: pointer;
    transition:
      background var(--dur-1) var(--ease),
      border-color var(--dur-1) var(--ease),
      color var(--dur-1) var(--ease);
  }
  .btn:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent-text);
  }
  .btn:disabled,
  .btn[aria-disabled='true'] {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .btn.primary {
    border-color: var(--accent-solid);
    background: var(--accent-solid);
    color: var(--text-on-accent);
  }
  .btn.primary:hover:not(:disabled) {
    border-color: var(--accent-solid-hover);
    background: var(--accent-solid-hover);
    color: var(--text-on-accent);
  }
  .btn.primary:disabled,
  .btn.primary[aria-disabled='true'] {
    opacity: 1;
    border-color: var(--line);
    background: var(--surface-3);
    color: var(--text-3);
  }
  .btn.ghost {
    border-color: transparent;
    background: transparent;
  }
  .btn.ghost:hover:not(:disabled) {
    border-color: transparent;
    background: var(--surface-2);
  }
  .btn.outline {
    background: transparent;
  }
  .btn.danger {
    color: var(--crit-text);
  }
  .btn.danger:hover:not(:disabled) {
    border-color: var(--crit);
    color: var(--crit-text);
  }
  .btn.danger-solid {
    border-color: var(--crit);
    background: var(--crit);
    color: var(--text-on-accent);
  }
  .btn.danger-solid:hover:not(:disabled) {
    color: var(--text-on-accent);
    filter: brightness(1.08);
  }
  .btn.small {
    min-block-size: calc(var(--control-h) - 0.4rem);
    padding-inline: 0.6rem;
    font-size: var(--fs-s);
  }
  .btn.icon-only {
    padding-inline: 0;
    inline-size: var(--control-h);
  }
  .btn.small.icon-only {
    inline-size: calc(var(--control-h) - 0.4rem);
  }
  .btn.busy {
    pointer-events: none;
  }
  .spinner {
    inline-size: 1em;
    block-size: 1em;
    border: 2px solid currentColor;
    border-inline-end-color: transparent;
    border-radius: 50%;
    animation: btn-spin var(--dur-spin) linear infinite;
  }
  @keyframes btn-spin {
    to {
      transform: rotate(360deg);
    }
  }
`;

/**
 * Knopfleiste: Knöpfe direkt nebeneinander, gemeinsame Rahmenlinien, nur die äußeren Ecken gerundet.
 * Auf den Container `.btn-group` anwenden; Kinder dürfen <lcm-button>, <lcm-srv-action> o. Ä. sein.
 */
export const groupStyles = css`
  .btn-group {
    display: inline-flex;
    flex-wrap: nowrap;
  }
  .btn-group > * + * {
    margin-inline-start: -1px;
  }
  .btn-group > :not(:first-child) {
    --btn-r-start: 0;
  }
  .btn-group > :not(:last-child) {
    --btn-r-end: 0;
  }
  .btn-group > :hover,
  .btn-group > :focus-within {
    position: relative;
    z-index: var(--z-raised);
  }
`;

export type ButtonVariant = 'secondary' | 'primary' | 'ghost' | 'outline' | 'danger' | 'danger-solid';
