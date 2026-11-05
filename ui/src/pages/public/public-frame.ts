import { LitElement, css, html } from 'lit';
import '../../components/lcm-button';
import '../../components/lcm-card';
import '../../components/lcm-logo';
import '../../components/lcm-menu';
import { baseStyles } from '../../components/shared';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { i18n, t } from '../../lib/i18n';
import { getTheme, onThemeChange, setTheme, type Theme } from '../../lib/theme';
import { languageItems, themeItems } from '../../shell/nav-model';
import { about } from '../../shell/update-model';

/** Sprach- und Theme-Auswahl für Seiten ohne Hauptmenü, dazu das Info-Fenster (Version und Impressum). */
export function publicMenus() {
  const theme = getTheme();
  return html`<lcm-menu
      label=${t('nav.language')}
      icon="globe"
      .items=${languageItems(i18n.languages)}
      value=${i18n.language}
      @lcm-select=${(event: CustomEvent<{ id: string }>) => void auth.changeLanguage(event.detail.id)}
    ></lcm-menu>
    <lcm-menu
      label=${t('nav.theme')}
      .icon=${theme === 'dark' ? 'moon' : theme === 'light' ? 'sun' : 'auto'}
      .items=${themeItems()}
      value=${theme}
      @lcm-select=${(event: CustomEvent<{ id: Theme }>) => setTheme(event.detail.id)}
    ></lcm-menu>
    <lcm-button variant="ghost" icon="info" icon-only label=${t('about.menu')} @click=${() => about.open()}></lcm-button>`;
}

/**
 * <lcm-public-frame heading="Passwort festlegen" subtitle="…"> – ruhiger, zentrierter Rahmen für die öffentlichen
 * Abläufe (Aktivierung, Passwort zurücksetzen), wie die Anmeldung: Logo, Überschrift, Karte, unten Sprache und Theme.
 * Mit `wide` wird die Karte breiter (Linux-Aktivierung mit Schlüsselanleitung).
 */
export class LcmPublicFrame extends LitElement {
  static override properties = {
    heading: { type: String },
    subtitle: { type: String },
    wide: { type: Boolean, reflect: true },
  };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        grid-template-rows: 1fr auto;
        justify-items: center;
        min-block-size: 100dvh;
        padding: var(--space-6) var(--page-pad) var(--space-4);
      }
      .stage {
        display: grid;
        align-content: center;
        gap: var(--space-5);
        inline-size: min(28rem, 100%);
      }
      :host([wide]) .stage {
        inline-size: min(40rem, 100%);
      }
      header {
        display: grid;
        justify-items: center;
        gap: var(--space-2);
        text-align: center;
      }
      lcm-logo {
        inline-size: 3.5rem;
        block-size: 3.5rem;
        filter: drop-shadow(0 6px 16px var(--shadow-far));
      }
      h1 {
        margin: var(--space-2) 0 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      p {
        margin: 0;
        color: var(--text-2);
        line-height: 1.5;
      }
      :host([embedded]) {
        min-block-size: auto;
        padding: var(--space-4) 0;
      }
      :host([embedded]) footer {
        display: none;
      }
      footer {
        display: flex;
        gap: var(--space-1);
        justify-content: center;
        padding-block-start: var(--space-5);
      }
    `,
  ];

  declare heading: string;
  declare subtitle: string;
  declare wide: boolean;

  constructor() {
    super();
    this.heading = '';
    this.subtitle = '';
    this.wide = false;
    new I18nController(this);
    new SubscribeController(this, onThemeChange);
    new SubscribeController(this, (listener) => auth.onChange(listener));
  }

  /** Mit Sitzung steht der Rahmen in der Shell (Hauptmenü mit Sprache und Theme ist da). */
  override willUpdate(): void {
    this.toggleAttribute('embedded', auth.isAuthenticated);
  }

  override render() {
    return html`
      <div class="stage">
        <header>
          <lcm-logo label=${t('nav.brand')}></lcm-logo>
          <h1>${this.heading}</h1>
          ${this.subtitle ? html`<p>${this.subtitle}</p>` : ''}
        </header>
        <lcm-card><slot></slot></lcm-card>
      </div>
      <footer>${publicMenus()}</footer>
    `;
  }
}
customElements.define('lcm-public-frame', LcmPublicFrame);
