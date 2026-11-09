import { html } from 'lit';
import '../components/lcm-main-nav.js';
import { accountItems, languages, navItems, navLabels, themeOptions } from './text.js';

/** Hauptmenü für die Seitenkompositionen im Katalog. */
export function appNav(current: string) {
  return html`<lcm-main-nav
    .items=${navItems}
    current=${current}
    brand-label="LCM, zum Dashboard"
    docs-href="#doku"
    .labels=${navLabels}
    .themeOptions=${themeOptions}
    theme=${document.documentElement.dataset.theme ?? 'auto'}
    .languages=${languages}
    language="de"
    account-initials="TG"
    .accountItems=${accountItems}
    @lcm-navigate=${(e: Event) => e.preventDefault()}
  ></lcm-main-nav>`;
}
