import { LitElement, css, html, nothing } from 'lit';
import '../components/lcm-empty-state';
import '../components/lcm-side-nav';
import type { SideNavSection } from '../components/lcm-side-nav';
import { baseStyles } from '../components/shared';
import { auth } from '../lib/auth';
import { I18nController, SubscribeController } from '../lib/controllers';
import { t } from '../lib/i18n';
import { FadeOnChange } from '../lib/motion';
import { findSettingsSection, visibleSettings } from '../lib/navigation';
import { router } from '../lib/router';

// Jede Einstellungsseite ist eine Datei `settings/<abschnitt>.ts`, die das Element
// `lcm-settings-<abschnitt>` definiert. Fehlt die Datei, zeigt der Abschnitt den Platzhalter.
const sectionModules = import.meta.glob<unknown>(['./settings/*.ts', '!./settings/*.test.ts']);

function sectionTag(id: string): string | undefined {
  return sectionModules[`./settings/${id}.ts`] ? `lcm-settings-${id}` : undefined;
}

/** Einstellungen: Seitenmenü links, rechts der gewählte Abschnitt. */
export class SettingsPage extends LitElement {
  static override properties = { _tag: { state: true }, _loadFailed: { state: true } };
  static override styles = [
    baseStyles,
    css`
      :host {
        display: grid;
        grid-template-columns: minmax(12rem, 16rem) minmax(0, 1fr);
        gap: var(--space-6);
        align-items: start;
      }
      lcm-side-nav {
        --control-h: calc(2.125rem - 0.375rem * var(--compact));
        position: sticky;
        inset-block-start: calc(var(--control-h) + 2.5rem);
        max-block-size: calc(100dvh - var(--control-h) - 3rem);
        overflow-y: auto;
      }
      h1 {
        margin: 0 0 var(--space-4);
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
          max-block-size: none;
          order: 1;
        }
      }
    `,
  ];

  declare _tag: string;
  declare _loadFailed: boolean;
  #section: string | undefined;
  #element: HTMLElement | undefined;

  constructor() {
    super();
    this._tag = '';
    this._loadFailed = false;
    new I18nController(this);
    new SubscribeController(this, (listener) => router.onChange(listener));
    new SubscribeController(this, (listener) => auth.onChange(listener));
    new FadeOnChange(this, () => `${this.#currentId}:${this._tag}`, () => this.renderRoot.querySelector('section'));
  }

  get #currentId(): string | undefined {
    return router.state.match?.params.section;
  }

  override willUpdate(): void {
    const id = this.#currentId;
    if (id === this.#section) return;
    this.#section = id;
    void this.#load(id);
  }

  /** Lädt das Modul des Abschnitts; bis dahin bleibt der bisherige Inhalt stehen. */
  async #load(id: string | undefined): Promise<void> {
    const loader = id ? sectionModules[`./settings/${id}.ts`] : undefined;
    try {
      await loader?.();
      if (id === this.#section) {
        this._loadFailed = false;
        this._tag = (id && sectionTag(id)) || '';
      }
    } catch {
      if (id === this.#section) this._loadFailed = true;
    }
  }

  #sideNav(): SideNavSection[] {
    return visibleSettings((permission) => auth.can(permission)).map((group) => ({
      label: t(group.labelKey),
      items: group.sections.map((section) => ({
        id: section.id,
        label: t(section.labelKey),
        href: `/settings/${section.id}`,
      })),
    }));
  }

  #content() {
    const section = findSettingsSection(this.#currentId);
    if (!section) return html`<lcm-empty-state level="2" icon="search" heading=${t('notFound.title')} text=${t('notFound.text')}></lcm-empty-state>`;
    if (!auth.can(section.permission)) {
      return html`<lcm-empty-state level="2" icon="lock" heading=${t('app.forbidden.title')} text=${t('app.forbidden.text')}></lcm-empty-state>`;
    }
    if (this._loadFailed) {
      return html`<lcm-empty-state level="2" icon="warning" heading=${t('app.loadFailed.title')} text=${t('app.loadFailed.text')}></lcm-empty-state>`;
    }
    if (!this._tag) return nothing;
    if (this.#element?.localName !== this._tag) this.#element = document.createElement(this._tag);
    return this.#element;
  }

  override render() {
    const section = findSettingsSection(this.#currentId);
    return html`
      <lcm-side-nav label=${t('settings.menu')} .sections=${this.#sideNav()} current=${this.#currentId ?? ''}></lcm-side-nav>
      <section>
        ${section ? html`<h1>${t(section.labelKey)}</h1>` : nothing} ${this.#content()}
      </section>
    `;
  }
}
customElements.define('lcm-page-settings', SettingsPage);
