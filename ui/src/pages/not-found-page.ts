import { css, html, LitElement } from 'lit';
import '../components/lcm-button';
import '../components/lcm-empty-state';
import { I18nController } from '../lib/controllers';
import { t } from '../lib/i18n';

/** Unbekannte Adresse. */
class NotFoundPage extends LitElement {
  static override styles = css`
    :host {
      display: block;
    }
  `;

  constructor() {
    super();
    new I18nController(this);
  }

  override render() {
    return html`<lcm-empty-state icon="search" level="1" heading=${t('notFound.title')} text=${t('notFound.text')}>
      <lcm-button slot="action" variant="primary" href="/">${t('notFound.back')}</lcm-button>
    </lcm-empty-state>`;
  }
}
customElements.define('lcm-page-not-found', NotFoundPage);
