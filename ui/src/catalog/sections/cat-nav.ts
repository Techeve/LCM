import { LitElement, css, html } from 'lit';
import '../../components/lcm-main-nav.js';
import { baseStyles } from '../../components/shared.js';
import { catalogStyles } from '../catalog-styles.js';
import { accountItems, languages, navItems, navLabels, themeOptions } from '../text.js';

export class CatNav extends LitElement {
  static override properties = { current: { state: true }, language: { state: true } };
  static override styles = [
    baseStyles,
    catalogStyles,
    css`
      .page-stub {
        padding: var(--space-5) var(--page-pad);
        color: var(--text-2);
      }
    `,
  ];
  declare current: string;
  declare language: string;

  constructor() {
    super();
    this.current = 'servers';
    this.language = 'de';
  }

  private nav(narrow: boolean) {
    return html`<div class=${narrow ? 'frame narrow' : 'frame'}>
      <lcm-main-nav
        .items=${navItems}
        current=${this.current}
        brand-label="LCM, zum Dashboard"
        docs-href="#doku"
        .labels=${navLabels}
        .themeOptions=${themeOptions}
        theme=${document.documentElement.dataset.theme ?? 'auto'}
        .languages=${languages}
        language=${this.language}
        account-initials="TG"
        .accountItems=${accountItems}
        @lcm-navigate=${(e: CustomEvent<{ id: string }>) => {
          e.preventDefault();
          if (e.detail.id !== 'home') this.current = e.detail.id;
        }}
        @lcm-language-change=${(e: CustomEvent<{ id: string }>) => (this.language = e.detail.id)}
        @lcm-theme-change=${(e: CustomEvent<{ id: string }>) => {
          const root = document.documentElement;
          if (e.detail.id === 'auto') delete root.dataset.theme;
          else root.dataset.theme = e.detail.id;
          this.requestUpdate();
        }}
      ></lcm-main-nav>
      <div class="page-stub">Seiteninhalt</div>
    </div>`;
  }

  override render() {
    return html`<section>
      <h2>Hauptmenü oben</h2>
      <p class="lead">Volle Fensterbreite. Links das Logo (führt zum Dashboard) und die Menüpunkte, rechts Suche (Strg/Cmd+K), Doku, Darstellung, Sprachliste und Kontomenü. Auf schmalen Breiten klappt das Menü in ein Symbol.</p>
      ${this.nav(false)}
      <h3 class="sub">Schmale Breite</h3>
      ${this.nav(true)}
    </section>`;
  }
}
customElements.define('cat-nav', CatNav);
