import { LitElement, css } from 'lit';
import { icon, type IconName } from './icons.js';
import { baseStyles } from './shared.js';

/** <lcm-icon name="search"> – Symbol in Textgröße; `label` macht es zum Bild mit Beschriftung. */
export class LcmIcon extends LitElement {
  static override properties = {
    name: { type: String },
    label: { type: String },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: inline-flex;
        color: inherit;
      }
    `,
  ];

  declare name: IconName;
  declare label: string;

  constructor() {
    super();
    this.name = 'info';
    this.label = '';
  }

  override updated(): void {
    if (this.label) {
      this.setAttribute('role', 'img');
      this.setAttribute('aria-label', this.label);
    } else {
      this.removeAttribute('role');
      this.removeAttribute('aria-label');
    }
  }

  override render() {
    return icon(this.name);
  }
}
customElements.define('lcm-icon', LcmIcon);
