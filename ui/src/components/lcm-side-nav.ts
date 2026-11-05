import { LitElement, css, html } from 'lit';
import { baseStyles, emit } from './shared.js';

export interface SideNavSection {
  label: string;
  items: { id: string; label: string; href: string }[];
}

/** <lcm-side-nav label="Einstellungen" .sections=${…} current="alerts"> – Seitenmenü mit Abschnitten. */
export class LcmSideNav extends LitElement {
  static override properties = {
    sections: { attribute: false },
    current: { type: String },
    label: { type: String },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: block;
      }
      nav {
        display: flex;
        flex-direction: column;
        gap: var(--space-5);
      }
      h3 {
        margin: 0 0 var(--space-2);
        padding-inline: var(--space-3);
        color: var(--text-3);
        font-family: var(--label-font);
        font-size: var(--fs-xs);
        font-weight: var(--fw-semibold);
        letter-spacing: var(--label-spacing);
        text-transform: var(--label-transform);
      }
      ul {
        display: flex;
        flex-direction: column;
        gap: 2px;
        margin: 0;
        padding: 0;
        list-style: none;
      }
      a {
        display: flex;
        align-items: center;
        min-block-size: var(--control-h);
        padding-inline: var(--space-3);
        border-radius: var(--radius-m);
        color: var(--text-2);
        font-size: var(--fs-m);
        font-weight: var(--fw-medium);
        text-decoration: none;
        transition: background var(--dur-1) var(--ease);
      }
      a:hover {
        background: var(--surface-2);
        color: var(--text-1);
      }
      a[aria-current='page'] {
        background: var(--accent-soft);
        color: var(--accent-text);
        font-weight: var(--fw-semibold);
      }
    `,
  ];

  declare sections: SideNavSection[];
  declare current: string;
  declare label: string;

  constructor() {
    super();
    this.sections = [];
    this.current = '';
    this.label = '';
  }

  /** Bei einer langen Liste soll der aktuelle Eintrag sichtbar sein. Rollt die Liste nicht selbst, bliebe sonst die ganze Seite mit. */
  override updated(changed: Map<string, unknown>): void {
    if (changed.has('current') && this.scrollHeight > this.clientHeight) this.renderRoot.querySelector('[aria-current="page"]')?.scrollIntoView({ block: 'nearest' });
  }

  private navigate(event: Event, id: string, href: string): void {
    const proceed = this.dispatchEvent(new CustomEvent('lcm-navigate', { detail: { id, href }, bubbles: true, composed: true, cancelable: true }));
    if (!proceed) event.preventDefault();
    else emit(this, 'lcm-side-navigate', { id });
  }

  override render() {
    return html`<nav aria-label=${this.label}>
      ${this.sections.map(
        (section) => html`<section>
          <h3>${section.label}</h3>
          <ul>
            ${section.items.map(
              (item) => html`<li>
                <a href=${item.href} aria-current=${item.id === this.current ? 'page' : 'false'} @click=${(e: Event) => this.navigate(e, item.id, item.href)}>${item.label}</a>
              </li>`,
            )}
          </ul>
        </section>`,
      )}
    </nav>`;
  }
}
customElements.define('lcm-side-nav', LcmSideNav);
