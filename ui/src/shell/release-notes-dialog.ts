import { LitElement, html, nothing } from 'lit';
import '../components/lcm-button';
import '../components/lcm-modal';
import { RELEASE_NOTES_URL, readReleaseNotes, systemApi, type ReleaseNotes } from '../api/system';
import { baseStyles } from '../components/shared';
import { I18nController } from '../lib/controllers';
import { t } from '../lib/i18n';
import { LiveData } from '../lib/live-data';
import { store } from '../lib/store';
import { docsStyles } from '../pages/docs/docs-styles';
import { parseDocHtml, renderDocNode } from '../pages/docs/docs-render';

const CHANGELOG_URL = 'https://doc.techeve.de/lcm/changelog/?mtm_campaign=linking&mtm_kwd=lcm';

/**
 * <lcm-release-notes-dialog> – was eine neue Version mitbringt. Der Server meldet die Hinweise einmal je Administrator
 * nach einem Update; ohne Hinweise (kein Update, Doku-Server nicht erreichbar) bleibt das Fenster zu.
 */
export class LcmReleaseNotesDialog extends LitElement {
  static override properties = { _closed: { state: true } };
  static override styles = [baseStyles, docsStyles];

  declare _closed: boolean;

  readonly #notes = new LiveData<ReleaseNotes>(this, { url: () => RELEASE_NOTES_URL, transform: readReleaseNotes });

  constructor() {
    super();
    this._closed = false;
    new I18nController(this);
  }

  #close(): void {
    this._closed = true;
    // Geht das schief, erscheint das Fenster bei der nächsten Anmeldung noch einmal. Mehr passiert nicht.
    void systemApi
      .markReleaseNotesSeen()
      .then(() => store.invalidate(RELEASE_NOTES_URL))
      .catch(() => undefined);
  }

  override render() {
    const notes = this.#notes.data;
    const open = !this._closed && Boolean(notes?.html);
    return html`<lcm-modal ?open=${open} size="ml" heading=${t('releaseNotes.title', { version: notes?.version ?? '' })} close-label=${t('releaseNotes.close')} @lcm-close=${() => this.#close()}>
      ${open && notes
        ? html`<article class="doc">
              ${parseDocHtml(notes.html).map((node) => renderDocNode(node, {}))}
              <p><a href=${CHANGELOG_URL} target="_blank" rel="noopener noreferrer">${t('releaseNotes.all')}</a></p>
            </article>
            <lcm-button slot="footer" variant="primary" @click=${() => this.#close()}>${t('releaseNotes.close')}</lcm-button>`
        : nothing}
    </lcm-modal>`;
  }
}
customElements.define('lcm-release-notes-dialog', LcmReleaseNotesDialog);
