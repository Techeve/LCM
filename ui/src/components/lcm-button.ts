import { LitElement, css, html, nothing } from 'lit';
import { buttonStyles, type ButtonVariant } from './button-styles.js';
import { icon, type IconName } from './icons.js';
import { baseStyles } from './shared.js';

/**
 * <lcm-button variant="primary" icon="plus">Text</lcm-button>
 * Mit `href` wird ein Link daraus, mit `target="_blank"` öffnet er in einem neuen Tab (ohne Zugriff auf dieses Fenster), mit `block` füllt der Knopf die Breite. Ohne sichtbaren Text (`icon-only`) ist `label` Pflicht (aria-label).
 */
export class LcmButton extends LitElement {
  static override properties = {
    variant: { type: String, reflect: true },
    size: { type: String, reflect: true },
    icon: { type: String },
    label: { type: String },
    href: { type: String },
    target: { type: String },
    disabled: { type: Boolean, reflect: true },
    busy: { type: Boolean, reflect: true },
    iconOnly: { type: Boolean, attribute: 'icon-only', reflect: true },
    block: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    buttonStyles,
    css`
      :host {
        display: inline-flex;
      }
      :host([disabled]) {
        pointer-events: none;
      }
      :host([block]) {
        display: flex;
      }
      :host([block]) .btn {
        flex: 1;
      }
    `,
  ];

  declare variant: ButtonVariant;
  declare size: 'm' | 'small';
  declare icon: IconName | '';
  declare label: string;
  declare href: string;
  declare target: string;
  declare disabled: boolean;
  declare busy: boolean;
  declare iconOnly: boolean;
  declare block: boolean;

  constructor() {
    super();
    this.variant = 'secondary';
    this.size = 'm';
    this.icon = '';
    this.label = '';
    this.href = '';
    this.target = '';
    this.disabled = false;
    this.busy = false;
    this.iconOnly = false;
    this.block = false;
  }

  private classes(): string {
    return ['btn', this.variant, this.size === 'small' ? 'small' : '', this.iconOnly ? 'icon-only' : '', this.busy ? 'busy' : '']
      .filter(Boolean)
      .join(' ');
  }

  override render() {
    const content = html`${this.busy ? html`<span class="spinner" aria-hidden="true"></span>` : this.icon ? icon(this.icon) : nothing}<slot></slot>`;
    const label = this.label || nothing;
    if (this.href) {
      const rel = this.target === '_blank' ? 'noopener noreferrer' : nothing;
      return html`<a class=${this.classes()} href=${this.href} target=${this.target || nothing} rel=${rel} aria-label=${label} aria-disabled=${this.disabled ? 'true' : nothing}>${content}</a>`;
    }
    return html`<button class=${this.classes()} type="button" ?disabled=${this.disabled} aria-label=${label} aria-busy=${this.busy ? 'true' : nothing}>${content}</button>`;
  }
}
customElements.define('lcm-button', LcmButton);
