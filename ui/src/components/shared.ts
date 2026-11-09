import { css } from 'lit';

/** Gemeinsame Grundstile jeder Komponente (Shadow DOM erbt nur Tokens, keine Regeln). */
export const baseStyles = css`
  :host {
    box-sizing: border-box;
  }
  *,
  *::before,
  *::after {
    box-sizing: inherit;
  }
  [hidden] {
    display: none !important;
  }
  :focus-visible {
    outline: 2px solid var(--focus-ring);
    outline-offset: 2px;
  }
  .icon {
    inline-size: 1.25em;
    block-size: 1.25em;
    flex: none;
  }
  .sr-only {
    position: absolute;
    inline-size: 1px;
    block-size: 1px;
    margin: -1px;
    padding: 0;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  [popover] {
    position: fixed;
    inset: auto;
    margin: 0;
    padding: 0;
    border: 0;
    background: none;
    color: inherit;
    overflow: visible;
    max-inline-size: calc(100vw - 16px);
  }
  /* Bewegung (lib/motion.ts): Popover blenden und skalieren leicht ein und aus. */
  [popover] {
    opacity: 0;
    transform: scale(0.97);
    transform-origin: 50% 0;
    transition:
      opacity var(--dur-1) var(--ease),
      transform var(--dur-1) var(--ease);
  }
  [popover][data-placement='top'] {
    transform-origin: 50% 100%;
  }
  [popover]:popover-open {
    opacity: 1;
    transform: none;
  }
  @starting-style {
    [popover]:popover-open {
      opacity: 0;
      transform: scale(0.97);
    }
  }
  /* Neue Einträge in live aktualisierten Listen (FreshKeys). */
  [data-fresh] {
    animation: lcm-fresh var(--dur-2) var(--ease);
  }
  @keyframes lcm-fresh {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
  }
  /*
   * Ausblenden von Popovern und Dialogen: Sie bleiben dafür einen Augenblick im Top Layer (overlay).
   * Nur, wo der Browser das kann; sonst verschwinden sie wie bisher sofort. Schon geschlossen nehmen sie keine Klicks an.
   */
  @supports (overlay: auto) {
    [popover] {
      transition:
        opacity var(--dur-1) var(--ease),
        transform var(--dur-1) var(--ease),
        overlay var(--dur-1) allow-discrete,
        display var(--dur-1) allow-discrete;
    }
    [popover]:not(:popover-open),
    dialog:not([open]) {
      pointer-events: none;
    }
    dialog,
    dialog::backdrop {
      transition:
        opacity var(--dur-1) var(--ease),
        transform var(--dur-1) var(--ease),
        overlay var(--dur-1) allow-discrete,
        display var(--dur-1) allow-discrete;
    }
    dialog:not([open]) {
      opacity: 0;
      transform: var(--dialog-exit, translateY(0.5rem) scale(0.98));
    }
    dialog:not([open])::backdrop {
      opacity: 0;
    }
  }
  button {
    font: inherit;
    color: inherit;
  }
  @media (prefers-reduced-motion: reduce) {
    *,
    *::before,
    *::after {
      animation: none !important;
      transition: none !important;
    }
  }
`;

/** Kleine Helfer, die viele Komponenten brauchen: Festbreitenschrift, gedämpfter Text, veraltete Daten. */
export const utilStyles = css`
  .mono {
    font-family: var(--font-mono);
    font-size: 0.92em;
  }
  .muted {
    color: var(--text-2);
  }
  /* Anzeige der letzten bekannten Daten, während neue geladen werden. */
  .stale {
    opacity: 0.6;
    transition: opacity var(--dur-2) var(--ease);
  }
`;

/**
 * Rahmen aller modalen <dialog>: Fläche, Rundung, Schatten, abgedunkelter Hintergrund, Einblenden. Breite, Höhe,
 * Lage und Überlauf setzt jeder Dialog selbst. `--dialog-enter` (Standard: von unten) und `--dialog-exit` (baseStyles)
 * legen fest, aus welcher Richtung er ein- und ausblendet.
 */
export const dialogStyles = css`
  dialog {
    padding: 0;
    border: 1px solid transparent;
    border-radius: var(--radius-l);
    background: var(--surface-overlay);
    color: var(--text-1);
    box-shadow: var(--elev-3);
    overflow: hidden;
  }
  dialog::backdrop {
    background: var(--scrim);
    backdrop-filter: blur(2px);
  }
  dialog[open] {
    animation: dialog-in var(--dur-2) var(--ease);
  }
  @keyframes dialog-in {
    from {
      opacity: 0;
      transform: var(--dialog-enter, translateY(0.75rem) scale(0.98));
    }
  }
`;

/** Ampel-Stufen: setzen `--c`, `--c-soft` und `--c-text` aus den Status-Tokens. */
export const levelStyles = css`
  :host([level='great']) {
    --c: var(--great);
    --c-soft: var(--great-soft);
    --c-text: var(--great-text);
  }
  :host([level='ok']) {
    --c: var(--ok);
    --c-soft: var(--ok-soft);
    --c-text: var(--ok-text);
  }
  :host([level='warn']) {
    --c: var(--warn);
    --c-soft: var(--warn-soft);
    --c-text: var(--warn-text);
  }
  :host([level='crit']) {
    --c: var(--crit);
    --c-soft: var(--crit-soft);
    --c-text: var(--crit-text);
  }
  :host([level='info']) {
    --c: var(--info);
    --c-soft: var(--info-soft);
    --c-text: var(--info-text);
  }
  :host([level='unknown']) {
    --c: var(--unknown);
    --c-soft: var(--unknown-soft);
    --c-text: var(--unknown-text);
  }
  :host([level='running']) {
    --c: var(--running);
    --c-soft: var(--running-soft);
    --c-text: var(--running-text);
  }
`;

export type StatusLevel = 'great' | 'ok' | 'warn' | 'crit' | 'unknown' | 'running';

let counter = 0;
/** Eindeutige Kennung für aria-Verweise innerhalb eines Shadow-Roots. */
export function uid(prefix: string): string {
  counter += 1;
  return `${prefix}-${counter}`;
}

/** Ereignis, das aus dem Shadow DOM aufsteigt. */
export function emit<T>(host: HTMLElement, name: string, detail?: T): boolean {
  return host.dispatchEvent(new CustomEvent<T>(name, { detail, bubbles: true, composed: true }));
}

/** Liest und schreibt localStorage, ohne zu scheitern (privates Fenster, gesperrter Speicher). */
export const safeStorage = {
  get(key: string): string | null {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key: string, value: string | null): void {
    try {
      if (value === null) localStorage.removeItem(key);
      else localStorage.setItem(key, value);
    } catch {
      // ohne Speicher gilt die Wahl nur für diese Sitzung
    }
  },
};
