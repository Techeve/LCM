import { LitElement, css, html } from 'lit';
import { baseStyles } from '../../components/shared';

/**
 * <lcm-sec-cell href="/servers/1" muted mono block> – Inhalt einer Tabellenzelle. Die Tabelle zeichnet ihre Zellen
 * in ihrem eigenen Schatten und kennt weder Links noch gedämpften Text; dieser Baustein bringt beides mit.
 * Mit `href` wird ein Link daraus, `external` öffnet ihn in einem neuen Tab. `block` kürzt zu lange Zeilen mit
 * Auslassungspunkten, wie die Tabelle es mit ihren eigenen Zellen tut.
 */
export class SecCell extends LitElement {
  static override properties = {
    href: { type: String },
    external: { type: Boolean },
    muted: { type: Boolean, reflect: true },
    mono: { type: Boolean, reflect: true },
    block: { type: Boolean, reflect: true },
    strong: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline;
      }
      :host([block]) {
        display: block;
        overflow: hidden;
        text-overflow: ellipsis;
      }
      :host([muted]) {
        color: var(--text-2);
      }
      :host([mono]) {
        font-family: var(--font-mono);
        font-size: 0.92em;
      }
      :host([strong]) {
        font-weight: var(--fw-semibold);
      }
      a {
        color: var(--accent-text);
        text-decoration: none;
      }
      a:hover {
        text-decoration: underline;
      }
    `,
  ];

  declare href: string;
  declare external: boolean;
  declare muted: boolean;
  declare mono: boolean;
  declare block: boolean;
  declare strong: boolean;

  constructor() {
    super();
    this.href = '';
    this.external = false;
    this.muted = false;
    this.mono = false;
    this.block = false;
    this.strong = false;
  }

  override render() {
    if (!this.href) return html`<slot></slot>`;
    return this.external
      ? html`<a href=${this.href} target="_blank" rel="noopener noreferrer"><slot></slot></a>`
      : html`<a href=${this.href}><slot></slot></a>`;
  }
}
customElements.define('lcm-sec-cell', SecCell);
