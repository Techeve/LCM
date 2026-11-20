import { LitElement, css, html, nothing, svg } from 'lit';
import { osLogoFor } from './os-logos.js';
import { baseStyles } from './shared.js';

/** Kachel, Logo und Name; geteilt von <lcm-os> und <lcm-server-os>. Kachelfarben: Tokens `--os-tile-*`. */
export const osTileStyles = css`
  :host {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    min-inline-size: 0;
    vertical-align: middle;
  }
  .tile {
    display: grid;
    place-items: center;
    flex: none;
    inline-size: 1.5rem;
    block-size: 1.5rem;
    border-radius: var(--radius-s);
    background: var(--os-tile-bg);
    box-shadow: inset 0 0 0 1px var(--os-tile-edge);
  }
  svg {
    inline-size: 1rem;
    block-size: 1rem;
  }
  .name {
    min-inline-size: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
`;

/**
 * <lcm-os os="debian" label="Debian 12" size="l"> – Logo (Kachel) mit optionalem Namen; `size="l"` für Seitenköpfe.
 * `product` (Proxmox-Typ) und `model` (Hardware-Modell) wählen das Produktlogo wie in der Serverliste (`osLogoFor`).
 */
export class LcmOs extends LitElement {
  static override properties = {
    os: { type: String, reflect: true },
    product: { type: String },
    model: { type: String },
    label: { type: String },
    size: { type: String, reflect: true },
  };
  static override styles = [
    baseStyles,
    osTileStyles,
    css`
      :host([size='l']) .tile {
        inline-size: 2.75rem;
        block-size: 2.75rem;
        border-radius: var(--radius-m);
      }
      :host([size='l']) svg {
        inline-size: 1.75rem;
        block-size: 1.75rem;
      }
    `,
  ];

  declare os: string;
  declare product: string;
  declare model: string;
  declare label: string;
  declare size: 'm' | 'l';

  constructor() {
    super();
    this.os = 'linux';
    this.product = '';
    this.model = '';
    this.label = '';
    this.size = 'm';
  }

  override render() {
    const logo = osLogoFor({ os_id: this.os, proxmox_type: this.product, hardware_model: this.model });
    const named = this.label !== '';
    return html`<span class="tile" role=${named ? 'presentation' : 'img'} aria-label=${named ? nothing : logo.title}>
        <svg viewBox="0 0 24 24" aria-hidden="true">${svg`<path d=${logo.path} fill=${`#${logo.hex}`}></path>`}</svg>
      </span>
      ${named ? html`<span class="name">${this.label}</span>` : null}`;
  }
}
customElements.define('lcm-os', LcmOs);
