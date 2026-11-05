import { LitElement, css, html, nothing, type TemplateResult } from 'lit';
import '../components/lcm-logo.js';
import '../components/lcm-toaster.js';
import './sections/cat-foundations.js';
import './sections/cat-nav.js';
import './sections/cat-status.js';
import './sections/cat-table.js';
import './sections/cat-surfaces.js';
import './sections/cat-forms.js';
import './sections/cat-help.js';
import './sections/cat-overlays.js';
import './sections/cat-dashboard.js';
import './sections/cat-server.js';
import './sections/cat-settings.js';
import './sections/cat-messages.js';

type Theme = 'auto' | 'light' | 'dark';
type Density = 'auto' | 'comfortable' | 'compact';

const SECTIONS = [
  ['foundations', 'Grundlagen'],
  ['nav', 'Hauptmenü'],
  ['status', 'Ampel'],
  ['table', 'Datentabelle'],
  ['surfaces', 'Karten und Bausteine'],
  ['forms', 'Formulare'],
  ['help', 'Erklärungen'],
  ['overlays', 'Fenster, Dialoge, Toasts'],
  ['dashboard', 'Dashboard'],
  ['server', 'Serverseite'],
  ['settings', 'Einstellungen'],
  ['messages', 'Meldungen'],
] as const;

const SECTION_VIEW: Record<(typeof SECTIONS)[number][0], TemplateResult> = {
  foundations: html`<cat-foundations id="foundations"></cat-foundations>`,
  nav: html`<cat-nav id="nav"></cat-nav>`,
  status: html`<cat-status id="status"></cat-status>`,
  table: html`<cat-table id="table"></cat-table>`,
  surfaces: html`<cat-surfaces id="surfaces"></cat-surfaces>`,
  forms: html`<cat-forms id="forms"></cat-forms>`,
  help: html`<cat-help id="help"></cat-help>`,
  overlays: html`<cat-overlays id="overlays"></cat-overlays>`,
  dashboard: html`<cat-dashboard id="dashboard"></cat-dashboard>`,
  server: html`<cat-server id="server"></cat-server>`,
  settings: html`<cat-settings id="settings"></cat-settings>`,
  messages: html`<cat-messages id="messages"></cat-messages>`,
};

function readParam<T extends string>(name: string, allowed: readonly T[], fallback: T): T {
  const value = new URLSearchParams(location.search).get(name) as T | null;
  return value && allowed.includes(value) ? value : fallback;
}

/** Komponentenkatalog (nur Entwicklung): hell/dunkel, Dichte, alle Bausteine mit Testdaten. */
class LcmCatalog extends LitElement {
  static override properties = {
    theme: { state: true },
    density: { state: true },
  };
  static override styles = css`
    :host {
      display: block;
      min-block-size: 100dvh;
    }
    .bar {
      position: sticky;
      inset-block-start: 0;
      z-index: 30;
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: var(--space-4) var(--space-5);
      padding: var(--space-3) var(--page-pad);
      background: color-mix(in srgb, var(--surface-1) 92%, transparent);
      backdrop-filter: blur(10px);
      box-shadow: 0 1px 0 var(--line);
    }
    .brand {
      display: flex;
      align-items: center;
      gap: var(--space-3);
      font-weight: var(--fw-bold);
    }
    .brand small {
      color: var(--text-3);
      font-weight: var(--fw-regular);
    }
    .group {
      display: inline-flex;
      align-items: center;
      gap: var(--space-2);
    }
    .group > span {
      color: var(--text-2);
      font-family: var(--label-font);
      font-size: var(--fs-xs);
      font-weight: var(--fw-semibold);
      letter-spacing: var(--label-spacing);
      text-transform: var(--label-transform);
    }
    .seg {
      display: inline-flex;
      padding: 3px;
      border-radius: var(--radius-m);
      background: var(--surface-2);
    }
    .seg button {
      min-block-size: 2rem;
      padding-inline: var(--space-3);
      border: 0;
      border-radius: var(--radius-s);
      background: transparent;
      color: var(--text-2);
      font: inherit;
      font-size: var(--fs-s);
      font-weight: var(--fw-medium);
      cursor: pointer;
    }
    .seg button:hover {
      color: var(--text-1);
    }
    .seg button[aria-pressed='true'] {
      background: var(--accent-solid);
      color: var(--text-on-accent);
    }
    @media (max-width: 90rem) {
      .group > span {
        display: none;
      }
    }
    .toc {
      display: flex;
      flex-wrap: wrap;
      gap: 2px;
      flex-basis: 100%;
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .toc button {
      padding: var(--space-1) var(--space-3);
      border: 0;
      border-radius: var(--radius-pill);
      background: transparent;
      color: var(--text-2);
      font: inherit;
      font-size: var(--fs-s);
      font-weight: var(--fw-medium);
      cursor: pointer;
    }
    .toc button:hover {
      background: var(--surface-2);
      color: var(--text-1);
    }
    main {
      padding-inline: var(--page-pad);
    }
    main > * + * {
      border-block-start: 1px solid var(--line);
    }
  `;

  declare theme: Theme;
  declare density: Density;
  /** `?s=messages` zeigt nur diesen Abschnitt. */
  readonly only = new URLSearchParams(location.search).get('s') ?? '';

  constructor() {
    super();
    this.theme = readParam('t', ['auto', 'light', 'dark'], 'auto');
    this.density = readParam('density', ['auto', 'comfortable', 'compact'], 'auto');
  }

  override willUpdate(): void {
    const root = document.documentElement;
    if (this.theme === 'auto') delete root.dataset.theme;
    else root.dataset.theme = this.theme;
    if (this.density === 'auto') delete root.dataset.density;
    else root.dataset.density = this.density;
  }

  private setTheme(theme: Theme): void {
    this.theme = theme;
  }

  private jump(id: string): void {
    if (this.only && id !== this.only) {
      const params = new URLSearchParams(location.search);
      params.set('s', id);
      location.search = params.toString();
      return;
    }
    this.renderRoot.querySelector(`#${id}`)?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  private segment<T extends string>(items: readonly (readonly [T, string])[], current: T, set: (value: T) => void) {
    return html`<span class="seg">${items.map(([id, label]) => html`<button type="button" aria-pressed=${String(id === current)} @click=${() => set(id)}>${label}</button>`)}</span>`;
  }

  override render() {
    return html`
      <div class="bar">
        <div class="brand"><lcm-logo></lcm-logo><span>LCM <small>Komponentenkatalog</small></span></div>
        <div class="group"><span>Darstellung</span>${this.segment([['light', 'Hell'], ['dark', 'Dunkel'], ['auto', 'Auto']] as const, this.theme, (v) => this.setTheme(v))}</div>
        <div class="group"><span>Dichte</span>${this.segment([['auto', 'Standard'], ['comfortable', 'Komfortabel'], ['compact', 'Kompakt']] as const, this.density, (v) => (this.density = v))}</div>
        <ul class="toc" aria-label="Abschnitte">${SECTIONS.map(([id, label]) => html`<li><button type="button" @click=${() => this.jump(id)}>${label}</button></li>`)}</ul>
      </div>
      <div>
        <main>${SECTIONS.filter(([id]) => !this.only || id === this.only).map(([id]) => SECTION_VIEW[id])}</main>
      </div>
      <lcm-toaster
        close-label="Schließen"
        .moreText=${(count: number) => `+${count} weitere`}
        .waitingText=${(count: number) => (count === 1 ? '1 Meldung wartet, bis das Fenster zu ist' : `${count} Meldungen warten, bis das Fenster zu ist`)}
      ></lcm-toaster>
      ${nothing}
    `;
  }
}
customElements.define('lcm-catalog', LcmCatalog);
