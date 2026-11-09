import { LitElement, css, html } from 'lit';
import { baseStyles, emit } from '../../../components/shared';

/**
 * <lcm-row-switch label="AdGuard Home" checked> – kleiner Schalter für eine Tabellenzeile, ohne sichtbaren Text
 * (`label` ist der Name für Screenreader und die Blase). Ein Klick öffnet die Zeile nicht. Meldet `lcm-change` { checked }
 * und ändert sich selbst nicht: Den Stand setzt die Seite, sobald er gespeichert ist.
 */
export class LcmRowSwitch extends LitElement {
  static override properties = {
    label: { type: String },
    checked: { type: Boolean, reflect: true },
    disabled: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
        flex: none;
        vertical-align: middle;
      }
      button {
        display: inline-flex;
        padding: 0;
        border: 0;
        border-radius: var(--radius-pill);
        background: none;
        cursor: pointer;
      }
      button:disabled {
        opacity: 0.55;
        cursor: not-allowed;
      }
      .track {
        position: relative;
        inline-size: 2rem;
        block-size: 1.125rem;
        border-radius: var(--radius-pill);
        background: var(--surface-3);
        box-shadow: inset 0 0 0 1px var(--line-strong);
        transition: background var(--dur-2) var(--ease);
      }
      .thumb {
        position: absolute;
        inset-block-start: 2px;
        inset-inline-start: 2px;
        inline-size: 0.875rem;
        block-size: 0.875rem;
        border-radius: 50%;
        background: var(--text-2);
        transition:
          transform var(--dur-2) var(--ease),
          background var(--dur-2) var(--ease);
      }
      :host([checked]) .track {
        background: var(--accent);
        box-shadow: none;
      }
      :host([checked]) .thumb {
        /* Weg des Knopfs: Spurbreite minus Knopf minus beide Ränder. */
        transform: translateX(calc(2rem - 0.875rem - 4px));
        background: var(--text-on-accent);
      }
    `,
  ];

  declare label: string;
  declare checked: boolean;
  declare disabled: boolean;

  constructor() {
    super();
    this.label = '';
    this.checked = false;
    this.disabled = false;
  }

  #toggle(event: Event): void {
    event.stopPropagation(); // die Zeile soll sich nicht öffnen
    emit(this, 'lcm-change', { checked: !this.checked });
  }

  override render() {
    return html`<button
      type="button"
      role="switch"
      aria-checked=${String(this.checked)}
      aria-label=${this.label}
      title=${this.label}
      ?disabled=${this.disabled}
      @click=${this.#toggle}
      @keydown=${(e: KeyboardEvent) => e.key === 'Enter' && e.stopPropagation()}
    >
      <span class="track"><span class="thumb"></span></span>
    </button>`;
  }
}
customElements.define('lcm-row-switch', LcmRowSwitch);
