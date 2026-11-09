import { LitElement, css, html } from 'lit';
import '../../components/lcm-empty-state';
import '../../components/lcm-notice';
import '../../components/lcm-side-nav';
import type { SideNavSection } from '../../components/lcm-side-nav';
import { baseStyles } from '../../components/shared';
import { auth } from '../../lib/auth';
import { I18nController, SubscribeController } from '../../lib/controllers';
import { t } from '../../lib/i18n';
import { FadeOnChange } from '../../lib/motion';
import { palette } from '../../lib/palette';
import { router } from '../../lib/router';
import { ACCOUNT_SECTIONS, continueTarget, sectionOf, type AccountSection } from './account-model';
import './account-password';
import './account-preferences';
import './account-profile';
import './account-two-factor';
import { formStyles } from './form-styles';

const SECTION_LABELS: Record<AccountSection, string> = {
  profile: 'account.section.profile',
  security: 'account.section.security',
  preferences: 'account.section.preferences',
};

/**
 * Mein Konto (`/account/:section?`): Profil, Sicherheit (Passwort, 2FA), Darstellung. Muss das Konto erst ein neues
 * Passwort setzen oder 2FA einrichten, zeigt die Seite nur diesen Schritt und führt danach zu `next` weiter.
 */
export class AccountPage extends LitElement {
  static override styles = [
    baseStyles,
    formStyles,
    css`
      :host {
        display: grid;
        grid-template-columns: minmax(12rem, 15rem) minmax(0, 1fr);
        gap: var(--space-6);
        align-items: start;
      }
      :host([required]) {
        grid-template-columns: minmax(0, 46rem);
        justify-content: center;
      }
      lcm-side-nav {
        position: sticky;
        inset-block-start: var(--space-4);
      }
      section {
        display: grid;
        gap: var(--gap);
        min-inline-size: 0;
        max-inline-size: var(--content-w, 48rem);
      }
      h1 {
        margin: 0;
        font-size: var(--fs-2xl);
        font-weight: var(--head-weight);
        letter-spacing: var(--head-tracking);
      }
      @media (max-width: 56rem) {
        :host {
          grid-template-columns: minmax(0, 1fr);
          gap: var(--space-4);
        }
        lcm-side-nav {
          position: static;
        }
      }
    `,
  ];

  constructor() {
    super();
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new FadeOnChange(this, () => router.state.match?.params.section ?? '', () => this.renderRoot.querySelector('section'));
  }

  /** Nach jedem Rendern neu anmelden: die Shell räumt die Palette beim Seitenwechsel ab. */
  override updated(): void {
    if (auth.remediation) return;
    palette.register(this, [
      { id: 'account:password', label: t('account.palette.password'), icon: 'key', run: () => void router.navigate('/account/security') },
      { id: 'account:two-factor', label: t('account.palette.twoFactor'), icon: 'shield', run: () => void router.navigate('/account/security') },
    ]);
  }

  override willUpdate(): void {
    this.toggleAttribute('required', auth.remediation !== null);
  }

  #sideNav(): SideNavSection[] {
    return [
      {
        label: t('account.title'),
        items: ACCOUNT_SECTIONS.map((id) => ({ id, label: t(SECTION_LABELS[id]), href: id === 'profile' ? '/account' : `/account/${id}` })),
      },
    ];
  }

  /** Pflichtschritt: nur das, was das Backend gerade verlangt. */
  #required() {
    const next = continueTarget(router.state.query.get('next'));
    const password = auth.remediation === 'password';
    return html`<section>
      <h1>${password ? t('account.required.passwordTitle') : t('account.required.twoFactorTitle')}</h1>
      <lcm-notice tone="warn">${password ? t('account.required.passwordText') : t('account.required.twoFactorText')}</lcm-notice>
      ${password
        ? html`<lcm-account-password return-to=${next}></lcm-account-password>`
        : html`<lcm-account-two-factor required return-to=${next}></lcm-account-two-factor>`}
    </section>`;
  }

  #content(section: AccountSection) {
    switch (section) {
      case 'security':
        return html`<lcm-account-password></lcm-account-password><lcm-account-two-factor></lcm-account-two-factor>`;
      case 'preferences':
        return html`<lcm-account-preferences></lcm-account-preferences>`;
      default:
        return html`<lcm-account-profile></lcm-account-profile>`;
    }
  }

  override render() {
    if (auth.remediation) return this.#required();
    const section = sectionOf(router.state.match?.params.section);
    return html`
      <lcm-side-nav label=${t('account.title')} .sections=${this.#sideNav()} current=${section ?? ''}></lcm-side-nav>
      <section>
        ${section
          ? html`<h1>${t(SECTION_LABELS[section])}</h1>
              ${this.#content(section)}`
          : html`<lcm-empty-state level="1" icon="search" heading=${t('notFound.title')} text=${t('notFound.text')}></lcm-empty-state>`}
      </section>
    `;
  }
}
customElements.define('lcm-page-account', AccountPage);
